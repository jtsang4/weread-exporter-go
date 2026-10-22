package app

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/fakesite"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

func epubEntries(t *testing.T, path string) map[string]bool {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatalf("%s is not a valid zip/EPUB: %v", path, err)
	}
	defer func() { _ = zr.Close() }()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	return names
}

func TestRunExportsFromTheCacheWithoutABrowser(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.cfg.Formats = []string{"epub", "txt", "md"}
	h.br.launchErr = []error{errors.New("no browser must be started")}
	e := h.exporter()

	results := h.mustRun(e)

	if h.br.launchCount() != 0 {
		t.Fatalf("the browser was launched although every chapter is cached\n%s", h.logs())
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	res := results[0]
	if res.Title != "A Test Book" || res.Chapters != 3 || res.Captured != 0 || res.BookID != testBookID {
		t.Fatalf("result = %+v", res)
	}
	want := []string{
		filepath.Join(h.cfg.OutputDir, "A Test Book.epub"),
		filepath.Join(h.cfg.OutputDir, "A Test Book.txt"),
		filepath.Join(h.cfg.OutputDir, "A Test Book.md"),
	}
	if strings.Join(res.Files, "\n") != strings.Join(want, "\n") {
		t.Fatalf("files = %v, want %v", res.Files, want)
	}

	entries := epubEntries(t, want[0])
	for _, name := range []string{"mimetype", "META-INF/container.xml"} {
		if !entries[name] {
			t.Errorf("EPUB lacks %s", name)
		}
	}
	txt := readFile(t, want[1])
	for _, s := range []string{"Chapter 1", "Text of chapter 2", "Text of chapter 3"} {
		mustContain(t, "txt", txt, s)
	}
	md := readFile(t, want[2])
	mustContain(t, "md", md, "## Chapter 3")

	// The cache keeps the raw captures next to the processed chapters.
	for pos, ch := range testBook().Chapters {
		if !h.store().IsProcessed(pos, ch.UID) {
			t.Errorf("chapter %d was not post-processed", pos+1)
		}
	}
	if !h.store().HasCover() {
		t.Error("the cover was not downloaded")
	}
}

func TestRunKeepsExistingOutputsUnlessOverwriting(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	e := h.exporter()
	first := h.mustRun(e)[0]
	if len(first.Files) != 1 || len(first.Skipped) != 0 {
		t.Fatalf("first run: %+v", first)
	}
	path := first.Files[0]
	if err := os.WriteFile(path, []byte("hand-edited"), 0o644); err != nil {
		t.Fatal(err)
	}

	second := h.mustRun(h.exporter())[0]
	if len(second.Files) != 0 || len(second.Skipped) != 1 || second.Skipped[0] != path {
		t.Fatalf("second run: %+v", second)
	}
	if got := readFile(t, path); got != "hand-edited" {
		t.Fatalf("the existing output was replaced: %q", got)
	}

	h.cfg.Overwrite = true
	third := h.mustRun(h.exporter())[0]
	if len(third.Files) != 1 {
		t.Fatalf("third run: %+v", third)
	}
	if got := readFile(t, path); got == "hand-edited" {
		t.Fatal("--overwrite did not replace the output")
	}
}

func TestRunLocalisesImagesOnce(t *testing.T) {
	h := newHarness(t)
	h.prefill()
	st := h.store()
	meta, err := st.LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	withImage := "## Chapter\n\nA figure:\n\n![](https://res.weread.qq.com/img/figure.png)\n\nand a broken one ![](https://res.weread.qq.com/img/gone.missing)\n"
	for pos, ch := range meta.Chapters {
		if err := st.SaveChapter(pos, ch.ID, withImage); err != nil {
			t.Fatal(err)
		}
	}
	h.cfg.Formats = []string{"md"}
	e := h.exporter()

	h.mustRun(e)

	md := readFile(t, filepath.Join(h.cfg.OutputDir, "A Test Book.md"))
	mustContain(t, "md", md, "A%20Test%20Book.assets/")
	mustContain(t, "md", md, "https://res.weread.qq.com/img/gone.missing") // stays remote, with a warning
	mustContain(t, "log", h.logs(), "image not localised")
	mustContain(t, "log", h.logs(), "images of this chapter could not be downloaded") // names the chapter
	mustContain(t, "log", h.logs(), "move the chapter's .md.bak file back over its .md file")
	assets, err := os.ReadDir(filepath.Join(h.cfg.OutputDir, "A Test Book.assets"))
	if err != nil || len(assets) != 1 {
		t.Fatalf("assets = %v, %v; the same image used by three chapters is stored once", assets, err)
	}

	fetched := len(h.site.Fetched())
	h.cfg.Overwrite = true
	h.mustRun(h.exporter())
	if again := len(h.site.Fetched()); again != fetched {
		t.Fatalf("the second run fetched %d more images; processed chapters must not hit the network", again-fetched)
	}
}

// The troubleshooting guide tells users to move a chapter's ".md.bak" back over
// its ".md" to try its images again. That has to work without the browser, and
// only for that chapter.
func TestRunTriesTheImagesOfAChapterAgainWhenItsRawCaptureIsPutBack(t *testing.T) {
	h := newHarness(t)
	h.prefill()
	st := h.store()
	meta, err := st.LoadMeta()
	if err != nil {
		t.Fatal(err)
	}
	for pos, ch := range meta.Chapters {
		if err := st.SaveChapter(pos, ch.ID, "## Chapter\n\nA broken picture ![](https://res.weread.qq.com/img/gone.missing) in the text.\n"); err != nil {
			t.Fatal(err)
		}
	}
	h.cfg.Formats = []string{"md"}
	h.mustRun(h.exporter())
	for pos, ch := range meta.Chapters {
		if !st.IsProcessed(pos, ch.ID) {
			t.Fatalf("chapter %d was not processed by the first run", pos+1)
		}
	}
	fetched := len(h.site.Fetched())

	path := st.ChapterPath(0, meta.Chapters[0].ID)
	if err := os.Rename(path+".bak", path); err != nil {
		t.Fatal(err)
	}
	h.cfg.Overwrite = true
	h.mustRun(h.exporter())

	if again := len(h.site.Fetched()) - fetched; again != 1 {
		t.Fatalf("the second run fetched %d images, want the one of the chapter that was put back", again)
	}
	if !st.IsProcessed(0, meta.Chapters[0].ID) {
		t.Fatal("the chapter that was put back is not processed again")
	}
	if h.br.launchCount() != 0 {
		t.Fatal("the browser started to process chapters that are captured already")
	}
}

func TestRunCapturesMissingChaptersAndRestoresTheReaderMode(t *testing.T) {
	h := newHarness(t)
	e := h.exporter()

	res := h.mustRun(e)[0]

	if res.Captured != 3 || res.Chapters != 3 {
		t.Fatalf("result = %+v", res)
	}
	urls := h.br.capturedURLs()
	if len(urls) != 3 || urls[0] != h.readerURL(0) || urls[2] != h.readerURL(2) {
		t.Fatalf("captured = %v", urls)
	}
	for pos := range testBook().Chapters {
		if !h.store().HasChapter(pos, testBook().Chapters[pos].UID) {
			t.Errorf("chapter %d is not cached", pos+1)
		}
	}
	if got := h.site.ModeChanges(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("reader mode changes = %v, want [0 1] (vertical for the export, then back)", got)
	}
	if h.site.Mode() != 1 {
		t.Fatal("the account was left in the vertical reader")
	}
	if exists(filepath.Join(h.cfg.CacheDir, "reader-mode-restore.json")) {
		t.Fatal("the restore marker was not removed")
	}
	if len(h.br.opened) != 1 || !h.br.opened[0].closed {
		t.Fatal("the browser session must be closed")
	}
	if len(h.br.injected) == 0 || h.br.injected[0]["wr_vid"] != testVID {
		t.Fatalf("the saved login was not handed to the browser: %v", h.br.injected)
	}
	if got := readFile(t, h.store().ChapterPath(0, 41)); !strings.Contains(got, "Text of chapter 1") {
		t.Fatalf("chapter 1 = %q", got)
	}
	if !exists(filepath.Join(h.cfg.OutputDir, "A Test Book.epub")) {
		t.Fatal("no EPUB")
	}
	// A pause between the chapters, none after the last.
	if len(h.sleeps) != 2 {
		t.Fatalf("sleeps = %v", h.sleeps)
	}
}

func TestRunLeavesAVerticalAccountAlone(t *testing.T) {
	h := newHarness(t)
	h.site.SetMode(0)
	h.mustRun(h.exporter())
	if got := h.site.ModeChanges(); len(got) != 0 {
		t.Fatalf("mode changes = %v, want none", got)
	}
	if h.site.Mode() != 0 {
		t.Fatal("the mode changed")
	}
}

func TestRunResumesFromTheCache(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 3)
	res := h.mustRun(h.exporter())[0]
	if res.Captured != 1 {
		t.Fatalf("captured = %d, want 1", res.Captured)
	}
	if urls := h.br.capturedURLs(); len(urls) != 1 || urls[0] != h.readerURL(1) {
		t.Fatalf("captured %v, want only chapter 2", urls)
	}
}

func TestRunSelectsChapters(t *testing.T) {
	h := newHarness(t)
	h.cfg.Chapters = "2-3"
	h.cfg.Formats = []string{"txt"}
	res := h.mustRun(h.exporter())[0]

	if res.Chapters != 2 || res.Captured != 2 {
		t.Fatalf("result = %+v", res)
	}
	if urls := h.br.capturedURLs(); len(urls) != 2 || urls[0] != h.readerURL(1) {
		t.Fatalf("captured %v", urls)
	}
	path := filepath.Join(h.cfg.OutputDir, "A Test Book (chapters 2-3).txt")
	txt := readFile(t, path)
	mustContain(t, "txt", txt, "Text of chapter 2")
	mustNotContain(t, "txt", txt, "Text of chapter 1")
	if exists(filepath.Join(h.cfg.OutputDir, "A Test Book.txt")) {
		t.Fatal("a partial export must not take the name of the whole book")
	}
}

func TestRunSelectingEveryChapterKeepsTheNormalName(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.cfg.Chapters = "1-"
	res := h.mustRun(h.exporter())[0]
	if filepath.Base(res.Files[0]) != "A Test Book.epub" {
		t.Fatalf("file = %s", res.Files[0])
	}
}

func TestRunRejectsChapterSelectionBeyondTheBook(t *testing.T) {
	h := newHarness(t)
	h.cfg.Chapters = "9"
	_, err := h.exporter().Run(context.Background())
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
	if h.br.launchCount() != 0 {
		t.Fatal("the browser started for an invalid selection")
	}
}

func TestRunRejectsChaptersWithABookList(t *testing.T) {
	h := newHarness(t)
	h.cfg.BookRef = "352003193_7T4LYH9ib"
	h.cfg.Chapters = "1-2"
	_, err := h.exporter().Run(context.Background())
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
}

func TestRunRejectsAnInvalidBookReference(t *testing.T) {
	h := newHarness(t)
	h.cfg.BookRef = "not a book!"
	_, err := h.exporter().Run(context.Background())
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
}

func TestRunAcceptsBookURLs(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	for _, ref := range []string{
		"1234567",
		testBookID,
		"https://weread.qq.com/web/bookDetail/" + testBookID,
		"https://weread.qq.com/web/reader/" + testBookID + "k" + weread.ChapterHash(41),
	} {
		h.cfg.BookRef = ref
		h.cfg.Overwrite = true
		if res := h.mustRun(h.exporter()); len(res) != 1 || res[0].BookID != testBookID {
			t.Errorf("%s: results %+v", ref, res)
		}
	}
}

func TestRunRefreshesTheBookInfoAndKeepsCachedChaptersOfAnUnchangedList(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)

	// The book was updated: new title, same chapters.
	b := testBook()
	b.Title = "A Better Title"
	h.site.AddBook(b)
	res := h.mustRun(h.exporter())[0]

	if res.Title != "A Better Title" || res.Captured != 0 {
		t.Fatalf("result = %+v", res)
	}
	meta, err := h.store().LoadMeta()
	if err != nil || meta.Title != "A Better Title" {
		t.Fatalf("the refreshed info was not cached: %+v, %v", meta, err)
	}
}

func TestRunCapturesNewChaptersOfAnUpdatedBook(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)

	b := testBook()
	b.Chapters = append(b.Chapters, fakesite.Chapter{UID: 44, Title: "Chapter 4", Words: 40})
	h.site.AddBook(b)
	h.br.pages[h.site.ChapterURL(testBookID, 44)] = chapterMarkdown(4)

	res := h.mustRun(h.exporter())[0]
	if res.Chapters != 4 || res.Captured != 1 {
		t.Fatalf("result = %+v", res)
	}
	mustContain(t, "log", h.logs(), "chapter list changed")
}

func TestRunFallsBackToTheCachedInfoWhenTheSiteIsDown(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	if err := os.WriteFile(h.store().CoverPath(), fakesite.JPEG(), 0o644); err != nil {
		t.Fatal(err)
	}
	h.onRequest = func(*http.Request) error { return errors.New("connection refused") }
	res := h.mustRun(h.exporter())[0]
	if res.Title != "A Test Book" || res.Captured != 0 {
		t.Fatalf("result = %+v", res)
	}
	mustContain(t, "log", h.logs(), "using the cached copy")
}

func TestRunFailsWithoutCachedInfoWhenTheSiteIsDown(t *testing.T) {
	h := newHarness(t)
	h.onRequest = func(*http.Request) error { return errors.New("connection refused") }
	_, err := h.exporter().Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "load the book info") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunSoldOutBook(t *testing.T) {
	h := newHarness(t)
	b := testBook()
	b.SoldOut = true
	h.site.AddBook(b)

	t.Run("not cached", func(t *testing.T) {
		_, err := h.exporter().Run(context.Background())
		if !errors.Is(err, ErrBookUnavailable) {
			t.Fatalf("err = %v, want ErrBookUnavailable", err)
		}
		if h.br.launchCount() != 0 {
			t.Fatal("no browser for a book that is not offered any more")
		}
	})

	t.Run("fully cached", func(t *testing.T) {
		h.prefill(1, 2, 3)
		res := h.mustRun(h.exporter())[0]
		if len(res.Files) != 1 {
			t.Fatalf("result = %+v", res)
		}
	})
}

func TestRunPartlyCachedSoldOutBookFails(t *testing.T) {
	h := newHarness(t)
	b := testBook()
	b.SoldOut = true
	h.site.AddBook(b)
	h.prefill(1)
	_, err := h.exporter().Run(context.Background())
	if !errors.Is(err, ErrBookUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunWarnsAboutTruncatedChapters(t *testing.T) {
	h := newHarness(t)
	b := testBook()
	for i := range b.Chapters {
		b.Chapters[i].Words = 5000 // the table of contents promises far more than "Text of chapter n"
	}
	h.site.AddBook(b)
	h.mustRun(h.exporter())
	mustContain(t, "log", h.logs(), "looks too short")
}

func TestRunOutputFilesAreAtomic(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.cfg.Formats = []string{"epub", "pdf"}
	e := h.exporter(func(e *Exporter) {
		e.printPDF = func(context.Context, string, browser.PDFOptions) ([]byte, error) {
			return nil, errors.New("chrome crashed")
		}
	})
	res, err := e.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "chrome crashed") {
		t.Fatalf("err = %v", err)
	}
	if len(res) != 1 || len(res[0].Files) != 1 {
		t.Fatalf("the EPUB is complete and must be reported: %+v", res)
	}
	if exists(filepath.Join(h.cfg.OutputDir, "A Test Book.pdf")) {
		t.Fatal("a failed PDF must leave no file behind")
	}
	if exists(h.store().Dir + "/" + printHTMLName) {
		t.Fatal("the temporary print page was not removed")
	}
	entries, _ := os.ReadDir(h.cfg.OutputDir)
	for _, ent := range entries {
		if strings.Contains(ent.Name(), ".tmp") || strings.Contains(ent.Name(), ".part") {
			t.Errorf("leftover %s", ent.Name())
		}
	}
}

func TestRunPrintsPDFFromACacheDirectoryPage(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.cfg.Formats = []string{"pdf"}
	var htmlSeen string
	e := h.exporter(func(e *Exporter) {
		e.printPDF = func(_ context.Context, htmlPath string, opts browser.PDFOptions) ([]byte, error) {
			if filepath.Dir(htmlPath) != h.store().Dir {
				t.Errorf("the page %s must live in the book's cache directory so that relative images resolve", htmlPath)
			}
			htmlSeen = readFile(t, htmlPath)
			return []byte("%PDF-1.7 fake"), nil
		}
	})
	res := h.mustRun(e)[0]
	mustContain(t, "print page", htmlSeen, "Text of chapter 2")
	if got := readFile(t, res.Files[0]); got != "%PDF-1.7 fake" {
		t.Fatalf("pdf = %q", got)
	}
}

func TestRunConvertsMOBIFromTheEPUB(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake converter is a shell script")
	}
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.cfg.Formats = []string{"mobi"}

	converter := filepath.Join(t.TempDir(), "ebook-convert")
	script := "#!/bin/sh\n[ -s \"$1\" ] || exit 3\nprintf 'MOBI from %s' \"$(basename \"$1\")\" > \"$2\"\n"
	if err := os.WriteFile(converter, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := h.exporter(func(e *Exporter) {
		e.findMOBI = func(string) (string, error) { return converter, nil }
	})
	res := h.mustRun(e)[0]

	// MOBI is converted from the EPUB, which therefore is part of the output.
	mobi := filepath.Join(h.cfg.OutputDir, "A Test Book.mobi")
	if len(res.Files) != 2 || res.Files[0] != filepath.Join(h.cfg.OutputDir, "A Test Book.epub") || res.Files[1] != mobi {
		t.Fatalf("files = %v", res.Files)
	}
	if got := readFile(t, mobi); got != "MOBI from A Test Book.epub" {
		t.Fatalf("mobi = %q", got)
	}
	entries, _ := os.ReadDir(h.cfg.OutputDir)
	for _, ent := range entries {
		if strings.Contains(ent.Name(), ".part") {
			t.Errorf("leftover %s", ent.Name())
		}
	}
}

func TestRunFailsEarlyWithoutAMOBIConverter(t *testing.T) {
	h := newHarness(t)
	h.cfg.Formats = []string{"mobi"}
	e := h.exporter(func(e *Exporter) {
		e.findMOBI = func(string) (string, error) { return "", errors.New("no converter here") }
	})
	_, err := e.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no converter here") {
		t.Fatalf("err = %v", err)
	}
	if h.br.launchCount() != 0 || len(h.site.Fetched()) != 0 {
		t.Fatal("nothing may be captured when the export cannot finish anyway")
	}
}

func TestRunWithCustomCSS(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	css := filepath.Join(t.TempDir(), "extra.css")
	if err := os.WriteFile(css, []byte("p { color: #123456; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.cfg.CSSFile = css
	res := h.mustRun(h.exporter())[0]

	zr, err := zip.OpenReader(res.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }()
	found := false
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".css") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, rc)
		_ = rc.Close()
		if strings.Contains(buf.String(), "#123456") {
			found = true
		}
	}
	if !found {
		t.Fatal("the custom CSS is not part of the EPUB")
	}
}

func TestNewRejectsAMissingCSSFile(t *testing.T) {
	h := newHarness(t)
	h.cfg.CSSFile = filepath.Join(t.TempDir(), "missing.css")
	if _, err := New(h.cfg, nil); err == nil {
		t.Fatal("a missing CSS file must be reported")
	}
}

func TestRunBookList(t *testing.T) {
	h := newHarness(t)
	other := testBook()
	other.ID = weread.MustHash("7654321")
	other.Title = "Second Book"
	h.site.AddBook(other)
	h.site.AddBookList("352003193_abc", []fakesite.ListEntry{
		{BookID: "1234567", Title: "A Test Book"},
		{BookID: "7654321", Title: "Second Book"},
		{BookID: "1234567", Title: "A Test Book (duplicate)"},
	})
	for i, ch := range other.Chapters {
		h.br.pages[h.site.ChapterURL(other.ID, ch.UID)] = chapterMarkdown(i + 1)
	}
	h.cfg.BookRef = "352003193_abc"
	h.cfg.LoadInterval = 7 * time.Second

	res := h.mustRun(h.exporter())

	if len(res) != 2 || res[0].Title != "A Test Book" || res[1].Title != "Second Book" {
		t.Fatalf("results = %+v", res)
	}
	for _, name := range []string{"A Test Book.epub", "Second Book.epub"} {
		if !exists(filepath.Join(h.cfg.OutputDir, name)) {
			t.Errorf("%s is missing", name)
		}
	}
	// The reader mode is restored after every book, not only at the very end.
	if got := h.site.ModeChanges(); len(got) != 4 || got[1] != 1 || got[3] != 1 {
		t.Fatalf("reader mode changes = %v, want vertical/horizontal per book", got)
	}
	// A longer pause between the books than the ordinary chapter pause is not
	// needed, but there must be one after the first book.
	sawBookPause := false
	for _, d := range h.sleeps {
		if d == h.cfg.LoadInterval {
			sawBookPause = true
		}
	}
	if !sawBookPause {
		t.Fatalf("sleeps = %v", h.sleeps)
	}
}

func TestRunBookListContinuesAfterAFailedBook(t *testing.T) {
	h := newHarness(t)
	broken := testBook()
	broken.ID = weread.MustHash("7654321")
	broken.Title = "Sold Out Book"
	broken.SoldOut = true
	h.site.AddBook(broken)
	h.site.AddBookList("352003193_abc", []fakesite.ListEntry{
		{BookID: "7654321", Title: "Sold Out Book"},
		{BookID: "1234567", Title: "A Test Book"},
	})
	h.cfg.BookRef = "352003193_abc"

	res, err := h.exporter().Run(context.Background())

	var batch *BatchError
	if !errors.As(err, &batch) {
		t.Fatalf("err = %v, want a BatchError", err)
	}
	if batch.Total != 2 || len(batch.Failed) != 1 || batch.Failed[broken.ID] == nil {
		t.Fatalf("batch = %+v", batch)
	}
	if !errors.Is(err, ErrBookUnavailable) {
		t.Fatal("errors.Is must see through the batch error")
	}
	if len(res) != 1 || res[0].Title != "A Test Book" {
		t.Fatalf("results = %+v: the healthy book must still be exported", res)
	}
}

func TestRunBookListStopsWhenEveryBookWouldFailTheSameWay(t *testing.T) {
	h := newHarness(t)
	h.cfg.KeepReaderMode = true // the account is horizontal: every book fails the same way
	other := testBook()
	other.ID = weread.MustHash("7654321")
	h.site.AddBook(other)
	h.site.AddBookList("352003193_abc", []fakesite.ListEntry{{BookID: "1234567"}, {BookID: "7654321"}})
	h.cfg.BookRef = "352003193_abc"

	_, err := h.exporter().Run(context.Background())
	if !errors.Is(err, ErrHorizontalReader) {
		t.Fatalf("err = %v", err)
	}
	if h.br.launchCount() != 0 {
		t.Fatalf("launches = %d", h.br.launchCount())
	}
}

func TestRunContextCancelledBetweenChapters(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.br.onCapture = func(url string) {
		if url == h.readerURL(1) {
			cancel() // the user pressed Ctrl-C while chapter 2 loads
		}
	}
	res, err := h.exporter().Run(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a cancellation", err)
	}
	if len(res) != 0 {
		t.Fatalf("results = %+v: nothing was written", res)
	}
	if !h.store().HasChapter(0, 41) || h.store().HasChapter(1, 42) {
		t.Fatal("the chapter captured before the interruption must stay cached")
	}
	if h.site.Mode() != 1 {
		t.Fatal("the reader mode must be restored although the run was interrupted")
	}
	if exists(filepath.Join(h.cfg.CacheDir, "reader-mode-restore.json")) {
		t.Fatal("the marker must be gone after the restore")
	}
	if len(h.br.opened) != 1 || !h.br.opened[0].closed {
		t.Fatal("the browser must be closed")
	}
}

func TestRunImageDownloadInterruptedLeavesTheChapterUnprocessed(t *testing.T) {
	h := newHarness(t)
	h.prefill(2, 3)
	st := h.store()
	if err := st.SaveChapter(0, 41, "![](https://res.weread.qq.com/img/a.png)\n"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onRequest = func(r *http.Request) error {
		if strings.HasSuffix(r.URL.Path, "/img/a.png") {
			cancel() // Ctrl-C while the image downloads
		}
		return nil
	}
	if _, err := h.exporter().Run(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a cancellation", err)
	}
	if st.IsProcessed(0, 41) {
		t.Fatal("a chapter whose images were not fetched must stay unprocessed so that the next run retries")
	}

	// The next run finishes the job.
	h.onRequest = nil
	h.mustRun(h.exporter())
	if !st.IsProcessed(0, 41) {
		t.Fatal("the chapter was not processed by the second run")
	}
	if got := readFile(t, st.ChapterPath(0, 41)); !strings.Contains(got, "](images/") {
		t.Fatalf("the image was not localised: %q", got)
	}
}

func TestRunReportsChaptersThatCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{&browser.ChapterUnavailableError{Footer: fakesite.PaywallFooter}}
	res, err := h.exporter().Run(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(res) != 0 {
		t.Fatalf("results = %+v: nothing was written", res)
	}
	mustContain(t, "error", err.Error(), "--chapters")
	mustContain(t, "error", err.Error(), "Chapter 2")
	if !h.store().HasChapter(0, 41) {
		t.Fatal("the chapter before the unreadable one must stay cached")
	}
	if h.site.Mode() != 1 {
		t.Fatal("the reader mode must be restored after the failure")
	}
}
