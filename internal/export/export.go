// Package export turns a captured book (Markdown chapters in the cache) into
// the final output formats: EPUB, PDF (HTML for the browser to print), TXT, a
// merged Markdown file and, through an external converter, MOBI.
package export

import (
	"fmt"
	"log/slog"

	"github.com/jtsang4/weread-exporter-go/internal/book"
)

// Source is a captured book: its description and the cache directory that
// holds the chapters, images and cover.
type Source struct {
	// BookID is the hashed book id (the cache directory name).
	BookID string
	Meta   *book.Meta
	Store  *book.Store
	// Select limits the export to some chapters: their positions (0-based) in
	// Meta.Chapters, strictly ascending. Nil exports every chapter.
	Select []int
	// Logger receives warnings (missing images, ...); defaults to slog.Default().
	Logger *slog.Logger
}

// selection is one exported chapter.
type selection struct {
	Pos     int // position in Meta.Chapters; the cache file name derives from it
	Chapter book.Chapter
}

// selected lists the chapters to export in book order.
func (s Source) selected() []selection {
	if s.Select == nil {
		out := make([]selection, len(s.Meta.Chapters))
		for i, ch := range s.Meta.Chapters {
			out[i] = selection{Pos: i, Chapter: ch}
		}
		return out
	}
	out := make([]selection, 0, len(s.Select))
	for _, i := range s.Select {
		if i >= 0 && i < len(s.Meta.Chapters) {
			out = append(out, selection{Pos: i, Chapter: s.Meta.Chapters[i]})
		}
	}
	return out
}

func (s Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Validate checks that the metadata is usable, that the selection makes sense
// and that every selected chapter has been captured.
func (s Source) Validate() error {
	if err := s.Meta.Validate(); err != nil {
		return err
	}
	if s.Select != nil {
		if len(s.Select) == 0 {
			return fmt.Errorf("export: no chapters selected")
		}
		prev := -1
		for _, i := range s.Select {
			if i < 0 || i >= len(s.Meta.Chapters) {
				return fmt.Errorf("export: chapter %d is out of range (the book has %d chapters)", i+1, len(s.Meta.Chapters))
			}
			if i <= prev {
				return fmt.Errorf("export: the chapter selection must be strictly ascending (chapter %d)", i+1)
			}
			prev = i
		}
	}
	for _, sel := range s.selected() {
		if !s.Store.HasChapter(sel.Pos, sel.Chapter.ID) {
			return fmt.Errorf("export: chapter %d %q (%d) has not been captured", sel.Pos+1, sel.Chapter.Title, sel.Chapter.ID)
		}
	}
	return nil
}

// chapterMarkdown returns the captured Markdown of the chapter at position i.
func (s Source) chapterMarkdown(i int) (string, error) {
	ch := s.Meta.Chapters[i]
	md, err := s.Store.ReadChapter(i, ch.ID)
	if err != nil {
		return "", fmt.Errorf("export: read chapter %d %q: %w", i+1, ch.Title, err)
	}
	return md, nil
}
