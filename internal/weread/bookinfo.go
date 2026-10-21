package weread

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jtsang4/weread-exporter-go/internal/book"
)

// BookDetail is what the book detail page tells us about a book.
type BookDetail struct {
	Meta    *book.Meta
	SoldOut bool
}

// BookDetailURL is the public detail page of a book.
func (c *Client) BookDetailURL(bookID string) string {
	return c.baseURL + "/web/bookDetail/" + bookID
}

// ReaderURL is the reader page of a book, or of one chapter if chapterUID > 0.
func (c *Client) ReaderURL(bookID string, chapterUID int64) string {
	u := c.baseURL + "/web/reader/" + bookID
	if chapterUID > 0 {
		u += "k" + ChapterHash(chapterUID)
	}
	return u
}

// BookDetail downloads and parses the book detail page.
func (c *Client) BookDetail(ctx context.Context, bookID string) (*BookDetail, error) {
	resp, err := c.Get(ctx, c.BookDetailURL(bookID), nil)
	if err != nil {
		return nil, err
	}
	return ParseBookDetail(resp.Body)
}

// ParseBookDetail extracts the book metadata from the page's
// window.__INITIAL_STATE__ script.
func ParseBookDetail(html []byte) (*BookDetail, error) {
	raw, err := extractInitialState(html)
	if err != nil {
		return nil, err
	}

	var state struct {
		Reader struct {
			BookInfo struct {
				Title   string  `json:"title"`
				Author  string  `json:"author"`
				Cover   string  `json:"cover"`
				Intro   string  `json:"intro"`
				SoldOut flexInt `json:"soldout"`
			} `json:"bookInfo"`
			ChapterInfos []struct {
				ChapterUID flexInt `json:"chapterUid"`
				Title      string  `json:"title"`
				Level      flexInt `json:"level"`
				WordCount  flexInt `json:"wordCount"`
				Anchors    []struct {
					Title string  `json:"title"`
					Level flexInt `json:"level"`
				} `json:"anchors"`
			} `json:"chapterInfos"`
		} `json:"reader"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, fmt.Errorf("weread: parse book detail state: %w", err)
	}

	info := state.Reader.BookInfo
	meta := &book.Meta{
		Title:  info.Title,
		Author: info.Author,
		Cover:  info.Cover,
		Intro:  info.Intro,
	}
	for _, ch := range state.Reader.ChapterInfos {
		chapter := book.Chapter{
			ID:      int64(ch.ChapterUID),
			Title:   ch.Title,
			Level:   ch.Level.Int(),
			Words:   ch.WordCount.Int(),
			Anchors: []book.Anchor{},
		}
		for _, a := range ch.Anchors {
			chapter.Anchors = append(chapter.Anchors, book.Anchor{Title: a.Title, Level: a.Level.Int()})
		}
		meta.Chapters = append(meta.Chapters, chapter)
	}

	// The original tool searched the whole page for `"soldout":1`, which also
	// matches unrelated books mentioned on the page; the book's own flag is what counts.
	return &BookDetail{Meta: meta, SoldOut: info.SoldOut != 0}, nil
}
