// Package book defines the data model of an exported book and the on-disk cache
// layout. The layout is compatible with the original Python tool, so an existing
// cache directory (for example the drunkdream/weread-books repository) keeps working:
//
//	cache/<book id>/meta.json
//	cache/<book id>/cover.jpg
//	cache/<book id>/chapters/<n>-<chapter uid>.md   (+ .md.bak = raw capture)
//	cache/<book id>/images/<md5(url)>.<ext>
package book

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Anchor is a sub-heading inside a chapter that appears in the table of contents.
type Anchor struct {
	Title string `json:"title"`
	Level int    `json:"level"`
}

// Chapter describes one chapter of a book as listed on the book detail page.
type Chapter struct {
	// ID is WeRead's chapterUid.
	ID    int64  `json:"id"`
	Title string `json:"title"`
	// Level is the nesting level in the table of contents (1 = top level).
	Level   int      `json:"level"`
	Words   int      `json:"words"`
	Anchors []Anchor `json:"anchors"`
}

// UnmarshalJSON accepts the chapter id as a number or a numeric string, because
// caches written by other tools are not consistent about it.
func (c *Chapter) UnmarshalJSON(data []byte) error {
	type plain Chapter
	aux := struct {
		*plain
		ID json.RawMessage `json:"id"`
	}{plain: (*plain)(c)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	raw := strings.Trim(strings.TrimSpace(string(aux.ID)), `"`)
	if raw == "" || raw == "null" {
		c.ID = 0
		return nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return fmt.Errorf("book: invalid chapter id %s", aux.ID)
	}
	c.ID = id
	return nil
}

// Meta is the book description cached in meta.json.
type Meta struct {
	Title    string    `json:"title"`
	Author   string    `json:"author"`
	Cover    string    `json:"cover"`
	Intro    string    `json:"intro"`
	Chapters []Chapter `json:"chapters"`
}

// Validate checks the invariants the exporter relies on.
func (m *Meta) Validate() error {
	if m == nil {
		return fmt.Errorf("book: nil meta")
	}
	if strings.TrimSpace(m.Title) == "" {
		return fmt.Errorf("book: meta has no title")
	}
	if len(m.Chapters) == 0 {
		return fmt.Errorf("book: meta has no chapters")
	}
	return nil
}

// coverSizePrefix matches the size prefix of a WeRead cover file name: "s_"
// (thumbnail) or "t<N>_" (N = 1..9, larger is bigger).
var coverSizePrefix = regexp.MustCompile(`^(s|t\d)_`)

// CoverURLs returns the URLs to try for the cover image, best first: the
// largest rendition ("t9_") and then the URL from the book page as a fallback.
func (m *Meta) CoverURLs() []string {
	if m.Cover == "" {
		return nil
	}
	urls := make([]string, 0, 2)
	if slash := strings.LastIndex(m.Cover, "/"); slash >= 0 {
		dir, file := m.Cover[:slash+1], m.Cover[slash+1:]
		if loc := coverSizePrefix.FindString(file); loc != "" {
			if hi := dir + "t9_" + file[len(loc):]; hi != m.Cover {
				urls = append(urls, hi)
			}
		}
	}
	return append(urls, m.Cover)
}

// EncodeMeta serialises meta in the cache format.
func EncodeMeta(m *Meta) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
