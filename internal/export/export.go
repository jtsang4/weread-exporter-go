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
	// Logger receives warnings (missing images, ...); defaults to slog.Default().
	Logger *slog.Logger
}

func (s Source) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

// Validate checks that the metadata is usable and that every chapter has been captured.
func (s Source) Validate() error {
	if err := s.Meta.Validate(); err != nil {
		return err
	}
	for i, ch := range s.Meta.Chapters {
		if !s.Store.HasChapter(i, ch.ID) {
			return fmt.Errorf("export: chapter %d %q (%d) has not been captured", i+1, ch.Title, ch.ID)
		}
	}
	return nil
}

// chapterHTML renders chapter i to an HTML fragment.
func (s Source) chapterMarkdown(i int) (string, error) {
	ch := s.Meta.Chapters[i]
	md, err := s.Store.ReadChapter(i, ch.ID)
	if err != nil {
		return "", fmt.Errorf("export: read chapter %d %q: %w", i+1, ch.Title, err)
	}
	return md, nil
}
