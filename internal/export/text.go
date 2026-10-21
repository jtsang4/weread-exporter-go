package export

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jtsang4/weread-exporter-go/internal/fsutil"
	"github.com/jtsang4/weread-exporter-go/internal/markdown"
)

// WriteTXT writes the book as plain text: the visible text of every chapter,
// each followed by a blank line.
func WriteTXT(w io.Writer, src Source) error {
	if err := src.Validate(); err != nil {
		return err
	}
	for _, sel := range src.selected() {
		md, err := src.chapterMarkdown(sel.Pos)
		if err != nil {
			return err
		}
		html, err := markdown.ToHTML(md)
		if err != nil {
			return fmt.Errorf("export: convert chapter %d %q: %w", sel.Pos+1, sel.Chapter.Title, err)
		}
		if _, err := io.WriteString(w, strings.TrimRight(markdown.Text(html), "\n")+"\n\n"); err != nil {
			return err
		}
	}
	return nil
}

// markdownImage finds the cached images a chapter refers to.
var markdownImage = regexp.MustCompile(`\]\(images/([^)\s]+)\)`)

// WriteMarkdown writes the chapters, concatenated, as one Markdown file. The
// images they refer to are copied to "<file name without extension>.assets/"
// next to it and the links are rewritten to point there, so the result can be
// moved around as a pair.
func WriteMarkdown(path string, src Source) error {
	if err := src.Validate(); err != nil {
		return err
	}
	assetsName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".assets"
	linkPrefix := "](" + markdownPathEscape(assetsName) + "/"

	var text strings.Builder
	used := map[string]struct{}{}
	for _, sel := range src.selected() {
		md, err := src.chapterMarkdown(sel.Pos)
		if err != nil {
			return err
		}
		for _, m := range markdownImage.FindAllStringSubmatch(md, -1) {
			used[m[1]] = struct{}{}
		}
		md = strings.ReplaceAll(md, "](images/", linkPrefix)
		text.WriteString(md)
		text.WriteString("\n")
	}

	if len(used) > 0 {
		assetsDir := filepath.Join(filepath.Dir(path), assetsName)
		if err := os.MkdirAll(assetsDir, 0o755); err != nil {
			return fmt.Errorf("export: create %s: %w", assetsDir, err)
		}
		for name := range used {
			if err := copyFile(src.Store.ImagePath(name), filepath.Join(assetsDir, name)); err != nil {
				src.logger().Warn("image referenced by the Markdown could not be copied", "image", name, "error", err)
			}
		}
	}
	return fsutil.WriteFileAtomic(path, []byte(text.String()), 0o644)
}

// markdownPathEscape makes a path segment safe inside a Markdown link
// destination: spaces and parentheses would end the destination early.
func markdownPathEscape(segment string) string {
	r := strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E")
	return r.Replace(segment)
}

func copyFile(from, to string) error {
	if filepath.Base(from) != filepath.Base(to) {
		return fmt.Errorf("refusing to rename while copying")
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	return fsutil.WriteAtomic(to, 0o644, func(w io.Writer) error {
		_, err := io.Copy(w, in)
		return err
	})
}
