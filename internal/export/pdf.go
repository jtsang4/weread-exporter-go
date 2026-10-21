package export

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jtsang4/weread-exporter-go/internal/assets"
	"github.com/jtsang4/weread-exporter-go/internal/markdown"
)

// PDFOptions tunes PDFHTML.
type PDFOptions struct {
	// ExtraCSS is appended to the built-in style sheet.
	ExtraCSS string
}

// PDFHTML renders the whole book as one self-contained HTML document that a
// browser can print to PDF: style sheet inlined, one <section> per chapter
// (each starting on a new page) and the cover in front.
//
// Image URLs stay relative ("cover.jpg", "images/<name>"), so the document has to
// be saved in the book's cache directory to resolve them.
func PDFHTML(src Source, opts PDFOptions) (string, error) {
	if err := src.Validate(); err != nil {
		return "", err
	}

	var body strings.Builder
	if src.Store.HasCover() {
		body.WriteString(`<div class="cover"><img src="` + escapeAttr(filepath.Base(src.Store.CoverPath())) + `" alt="" /></div>` + "\n")
	}
	for i, ch := range src.Meta.Chapters {
		md, err := src.chapterMarkdown(i)
		if err != nil {
			return "", err
		}
		html, err := markdown.ToHTML(md)
		if err != nil {
			return "", fmt.Errorf("export: convert chapter %d %q: %w", i+1, ch.Title, err)
		}
		nodes, err := markdown.ParseFragment(html)
		if err != nil {
			return "", fmt.Errorf("export: parse chapter %d %q: %w", i+1, ch.Title, err)
		}
		// Re-serialising removes anything active (scripts, handlers) that raw HTML
		// in the book text could smuggle into the page the browser renders.
		body.WriteString(`<section class="chapter">` + "\n")
		body.WriteString(markdown.SerializeXHTML(nodes))
		body.WriteString("\n</section>\n")
	}

	css := assets.PDFCSS
	if strings.TrimSpace(opts.ExtraCSS) != "" {
		css += "\n" + opts.ExtraCSS
	}
	// A "</style>" in user supplied CSS must not be able to break out of the element.
	css = strings.ReplaceAll(css, "</style", `<\/style`)

	var doc strings.Builder
	doc.WriteString("<!DOCTYPE html>\n<html lang=\"" + epubLang + "\">\n<head>\n<meta charset=\"utf-8\" />\n<title>")
	doc.WriteString(escapeText(src.Meta.Title))
	doc.WriteString("</title>\n<style>\n")
	doc.WriteString(css)
	doc.WriteString("\n</style>\n</head>\n<body>\n")
	doc.WriteString(body.String())
	doc.WriteString("</body>\n</html>\n")
	return doc.String(), nil
}
