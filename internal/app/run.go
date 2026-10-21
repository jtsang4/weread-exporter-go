package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/markdown"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// Result describes what the export of one book produced.
type Result struct {
	BookID string
	Title  string
	// Chapters is the number of chapters in the export.
	Chapters int
	// Captured is the number of chapters that had to be captured in a browser
	// during this run (the others came from the cache).
	Captured int
	// Files are the output files that were written.
	Files []string
	// Skipped are output files that already existed and were left alone.
	Skipped []string
}

// Run exports the configured book, or every book of the configured book list.
// The results of the books that were exported (or that failed after writing
// some files) are returned even when an error is returned as well. A book list
// in which some books failed yields a *BatchError.
func (e *Exporter) Run(ctx context.Context) (results []*Result, err error) {
	ref, err := weread.ParseRef(e.cfg.BookRef)
	if err != nil {
		return nil, usagef("%v", err)
	}
	if ref.Kind == weread.RefBookList && e.cfg.Chapters != "" {
		return nil, usagef("--chapters selects chapters of one book; it cannot be combined with a book list")
	}
	if err := os.MkdirAll(e.cfg.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create the cache directory: %w", err)
	}
	if err := e.checkTools(); err != nil {
		return nil, err
	}

	// Whatever happens, the account's reader mode must not stay changed.
	defer func() {
		if rerr := e.restoreReaderMode(ctx); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	e.recoverReaderMode(ctx)

	if ref.Kind == weread.RefBookList {
		return e.exportBookList(ctx, ref.ID)
	}
	res, err := e.exportBook(ctx, ref.ID)
	if reportable(res, err) {
		results = append(results, res)
	}
	return results, err
}

// reportable says whether the outcome of a book belongs into the results: it
// does when the book was exported, or failed after writing some files.
func reportable(res *Result, err error) bool {
	return res != nil && (err == nil || len(res.Files) > 0)
}

// checkTools fails early when an output format needs a program that is missing:
// finding out after hours of capturing would be too late.
func (e *Exporter) checkTools() error {
	for _, f := range e.cfg.Formats {
		if f != FormatMOBI {
			continue
		}
		converter, err := e.findMOBI(e.cfg.MOBIConverter)
		if err != nil {
			return fmt.Errorf("cannot produce MOBI: %w", err)
		}
		e.mobiConverter = converter
		e.log.Debug("MOBI converter found", "path", converter)
	}
	return nil
}

// exportBookList exports the books of a book list one after the other. A book
// that fails does not stop the others, unless the failure would repeat.
func (e *Exporter) exportBookList(ctx context.Context, listID string) ([]*Result, error) {
	entries, err := e.api.BookList(ctx, listID)
	if err != nil {
		return nil, fmt.Errorf("load book list %s: %w", listID, err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("book list %s contains no books", listID)
	}
	e.log.Info("exporting a book list", "list", listID, "books", len(entries))

	var results []*Result
	batch := &BatchError{Total: len(entries), Failed: map[string]error{}}
	seen := map[string]bool{}
	for i, entry := range entries {
		if seen[entry.ID] {
			batch.Total--
			continue
		}
		seen[entry.ID] = true
		e.log.Info("next book of the list", "n", i+1, "of", len(entries), "id", entry.ID, "title", entry.Title)

		res, err := e.exportBook(ctx, entry.ID)
		if reportable(res, err) {
			results = append(results, res)
		}
		switch {
		case err == nil:
		case ctx.Err() != nil, errors.Is(err, ErrLoginNeeded), errors.Is(err, ErrHorizontalReader):
			return results, err // the other books would fail the same way
		default:
			e.log.Error("book failed; carrying on with the next one", "id", entry.ID, "title", entry.Title, "error", err)
			batch.Failed[entry.ID] = err
		}
		if res != nil && res.Captured > 0 && i < len(entries)-1 {
			if err := e.sleep(ctx, e.cfg.LoadInterval); err != nil {
				return results, err
			}
		}
	}
	if len(batch.Failed) > 0 {
		return results, batch
	}
	return results, nil
}

// exportBook exports one book: refresh its description, capture the chapters
// that are not cached, post-process them and write the output files.
func (e *Exporter) exportBook(ctx context.Context, bookID string) (*Result, error) {
	store := book.NewStore(e.cfg.CacheDir, bookID)
	if err := store.EnsureDirs(); err != nil {
		return nil, fmt.Errorf("create the cache directory of the book: %w", err)
	}
	meta, soldOut, err := e.loadMeta(ctx, store, bookID)
	if err != nil {
		return nil, err
	}
	positions, partial, err := e.chapterSelection(meta)
	if err != nil {
		return nil, err
	}
	res := &Result{BookID: bookID, Title: meta.Title, Chapters: len(positions)}
	e.log.Info("exporting", "title", meta.Title, "author", meta.Author, "id", bookID, "chapters", len(positions), "of", len(meta.Chapters))

	e.fetchCover(ctx, store, meta)

	var todo []int
	for _, pos := range positions {
		if !store.HasChapter(pos, meta.Chapters[pos].ID) {
			todo = append(todo, pos)
		}
	}
	if len(todo) > 0 {
		if soldOut {
			return res, fmt.Errorf("%w: %q has %d chapters that are not cached (%d of %d are)",
				ErrBookUnavailable, meta.Title, len(todo), len(positions)-len(todo), len(positions))
		}
		e.log.Info("chapters to capture", "missing", len(todo), "cached", len(positions)-len(todo),
			"pause", e.cfg.LoadInterval, "at-least", time.Duration(len(todo)-1)*e.cfg.LoadInterval)
		captureErr := e.captureChapters(ctx, bookID, meta, store, todo)
		// The browser is done: give the account its reader mode back before the
		// (possibly long) post-processing.
		restoreErr := e.restoreReaderMode(ctx)
		if captureErr != nil {
			return res, errors.Join(captureErr, restoreErr)
		}
		if restoreErr != nil {
			e.log.Error("the export goes on, but the reader mode is still switched", "error", restoreErr)
		}
		res.Captured = len(todo)
	} else {
		e.log.Info("every chapter is cached; no browser is needed")
	}

	if err := e.postProcess(ctx, store, meta, positions); err != nil {
		return res, err
	}
	src := e.source(bookID, meta, store, positions, partial)
	if err := e.writeOutputs(ctx, src, e.outputBase(meta, partial), res); err != nil {
		return res, err
	}
	return res, nil
}

// loadMeta returns the description of the book: the fresh one from the site
// (stored in the cache), or the cached one when the site cannot be reached.
// soldOut reports that WeRead no longer offers the book.
func (e *Exporter) loadMeta(ctx context.Context, store *book.Store, bookID string) (meta *book.Meta, soldOut bool, err error) {
	cached, err := store.LoadMeta()
	switch {
	case err == nil:
		if verr := cached.Validate(); verr != nil {
			e.log.Warn("ignoring an unusable cached meta.json", "error", verr)
			cached = nil
		}
	case errors.Is(err, book.ErrNoMeta):
		cached = nil
	default:
		e.log.Warn("ignoring an unreadable cached meta.json", "error", err)
		cached = nil
	}

	detail, err := e.api.BookDetail(ctx, bookID)
	switch {
	case err != nil && cached != nil && ctx.Err() == nil:
		e.log.Warn("could not refresh the book info; using the cached copy", "error", err)
		return cached, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("load the book info of %s: %w", bookID, err)
	case detail.SoldOut:
		e.log.Warn("WeRead no longer offers this book (sold out)", "id", bookID)
		if cached != nil {
			return cached, true, nil
		}
		if verr := detail.Meta.Validate(); verr != nil {
			return nil, false, fmt.Errorf("%w (book %s, and nothing of it is cached)", ErrBookUnavailable, bookID)
		}
		return detail.Meta, true, nil
	}

	if err := detail.Meta.Validate(); err != nil {
		return nil, false, fmt.Errorf("the book info of %s is unusable: %w", bookID, err)
	}
	if cached != nil && !sameChapters(cached, detail.Meta) {
		e.log.Warn("the chapter list changed since the last export; cached chapters are matched by position and id")
	}
	if err := saveMetaIfChanged(store, cached, detail.Meta); err != nil {
		return nil, false, fmt.Errorf("save the book info: %w", err)
	}
	return detail.Meta, false, nil
}

func sameChapters(a, b *book.Meta) bool {
	if len(a.Chapters) != len(b.Chapters) {
		return false
	}
	for i := range a.Chapters {
		if a.Chapters[i].ID != b.Chapters[i].ID {
			return false
		}
	}
	return true
}

func saveMetaIfChanged(store *book.Store, old, fresh *book.Meta) error {
	if old != nil {
		oldData, err1 := book.EncodeMeta(old)
		newData, err2 := book.EncodeMeta(fresh)
		if err1 == nil && err2 == nil && bytes.Equal(oldData, newData) {
			return nil
		}
	}
	return store.SaveMeta(fresh)
}

// chapterSelection resolves --chapters into the positions to export. partial
// says whether that is fewer than all chapters.
func (e *Exporter) chapterSelection(meta *book.Meta) (positions []int, partial bool, err error) {
	total := len(meta.Chapters)
	if e.cfg.Chapters != "" {
		spec, err := ParseChapterSpec(e.cfg.Chapters)
		if err != nil {
			return nil, false, err
		}
		positions, err = spec.Resolve(total)
		if err != nil {
			return nil, false, err
		}
		return positions, len(positions) != total, nil
	}
	positions = make([]int, total)
	for i := range positions {
		positions[i] = i
	}
	return positions, false, nil
}

// fetchCover downloads the cover unless the cache has it. A missing cover is
// not worth failing the export for.
func (e *Exporter) fetchCover(ctx context.Context, store *book.Store, meta *book.Meta) {
	if store.HasCover() {
		return
	}
	fetch := e.imageFetcher()
	for _, url := range meta.CoverURLs() {
		data, err := fetch(ctx, url)
		if err != nil {
			e.log.Debug("cover not available", "url", url, "error", err)
			continue
		}
		if _, ok := book.SniffImage(data); !ok {
			e.log.Debug("the cover URL did not return an image", "url", url)
			continue
		}
		if err := store.SaveCover(data); err != nil {
			e.log.Warn("could not save the cover", "error", err)
		}
		return
	}
	e.log.Warn("the book has no usable cover; exporting without one")
}

// imageFetcher downloads images through the HTTP client of the site (proxy, retries).
func (e *Exporter) imageFetcher() markdown.ImageFetcher {
	return func(ctx context.Context, url string) ([]byte, error) {
		resp, err := e.api.Get(ctx, url, map[string]string{"Referer": e.api.BaseURL() + "/"})
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
}

// postProcess tidies up the captured Markdown of the chapters and downloads
// their images. A chapter is processed once: afterwards the raw capture is kept
// as "<chapter>.md.bak" and the processed text replaces it.
func (e *Exporter) postProcess(ctx context.Context, store *book.Store, meta *book.Meta, positions []int) error {
	fetch := e.imageFetcher()
	var downloaded, reused, failed int
	for _, pos := range positions {
		ch := meta.Chapters[pos]
		if store.IsProcessed(pos, ch.ID) {
			continue
		}
		raw, err := store.ReadChapter(pos, ch.ID)
		if err != nil {
			return fmt.Errorf("read chapter %d: %w", pos+1, err)
		}
		text, stats := markdown.LocalizeImages(ctx, markdown.Normalize(raw), fetch, store, e.log)
		if err := ctx.Err(); err != nil {
			return err // the images are incomplete: leave the chapter unprocessed
		}
		if err := store.RewriteChapter(pos, ch.ID, text); err != nil {
			return fmt.Errorf("save chapter %d: %w", pos+1, err)
		}
		downloaded += stats.Downloaded
		reused += stats.Reused
		failed += stats.Failed
	}
	if downloaded+reused+failed > 0 {
		e.log.Info("images", "downloaded", downloaded, "cached", reused, "failed", failed)
	}
	if failed > 0 {
		e.log.Warn("some images could not be downloaded and stay linked to the web; the e-book shows them only online")
	}
	return nil
}
