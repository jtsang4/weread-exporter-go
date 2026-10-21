package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// BookInfo describes a book the way the info command shows it.
type BookInfo struct {
	ID      string
	Meta    *book.Meta
	SoldOut bool
	// Cached[i] tells whether chapter i (0-based) is captured in the cache.
	Cached []bool
	// FromCache is true when the site could not be reached and the description
	// comes from the cache.
	FromCache bool
}

// CachedCount is the number of chapters in the cache.
func (b *BookInfo) CachedCount() int {
	n := 0
	for _, ok := range b.Cached {
		if ok {
			n++
		}
	}
	return n
}

// Info is what the info command found out: a book, or the books of a list.
type Info struct {
	Book   *BookInfo
	ListID string
	List   []weread.BookListEntry
}

// Info looks up the configured book (or book list) without exporting anything.
func (e *Exporter) Info(ctx context.Context) (*Info, error) {
	ref, err := weread.ParseRef(e.cfg.BookRef)
	if err != nil {
		return nil, usagef("%v", err)
	}
	if ref.Kind == weread.RefBookList {
		entries, err := e.api.BookList(ctx, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("load book list %s: %w", ref.ID, err)
		}
		return &Info{ListID: ref.ID, List: entries}, nil
	}

	store := book.NewStore(e.cfg.CacheDir, ref.ID)
	info := &BookInfo{ID: ref.ID}
	detail, err := e.api.BookDetail(ctx, ref.ID)
	switch {
	case err == nil:
		info.Meta, info.SoldOut = detail.Meta, detail.SoldOut
	case ctx.Err() != nil:
		return nil, ctx.Err()
	default:
		cached, cerr := store.LoadMeta()
		if cerr != nil {
			if errors.Is(cerr, book.ErrNoMeta) {
				return nil, fmt.Errorf("load the book info of %s: %w", ref.ID, err)
			}
			return nil, errors.Join(fmt.Errorf("load the book info of %s: %w", ref.ID, err), cerr)
		}
		e.log.Warn("could not reach the site; showing the cached book info", "error", err)
		info.Meta, info.FromCache = cached, true
	}
	if len(info.Meta.Chapters) == 0 {
		return nil, fmt.Errorf("the book info of %s lists no chapters", ref.ID)
	}
	info.Cached = make([]bool, len(info.Meta.Chapters))
	for i, ch := range info.Meta.Chapters {
		info.Cached[i] = store.HasChapter(i, ch.ID)
	}
	return &Info{Book: info}, nil
}
