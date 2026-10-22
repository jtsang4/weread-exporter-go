package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/util"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// converter renders CommonMark. XHTML-style void tags (<br />, <hr />) keep the
// output usable inside EPUB documents. The only raw HTML in a captured chapter
// is the <sup> pair that the capture hook writes around footnote markers; any
// other tag-like text is book text ("if x<y and z>w") and is shown as text.
var converter = goldmark.New(
	goldmark.WithRendererOptions(
		html.WithUnsafe(), html.WithXHTML(),
		renderer.WithNodeRenderers(util.Prioritized(rawHTMLAsText{}, 100)), // before goldmark's own renderer (1000)
	),
)

// rawHTMLAsText renders raw HTML as text, except for the footnote tags.
type rawHTMLAsText struct{}

// RegisterFuncs implements renderer.NodeRenderer.
func (rawHTMLAsText) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindRawHTML, renderRawHTML)
	reg.Register(ast.KindHTMLBlock, renderHTMLBlock)
}

func renderRawHTML(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkSkipChildren, nil
	}
	raw, ok := node.(*ast.RawHTML)
	if !ok {
		return ast.WalkSkipChildren, nil
	}
	var tag []byte
	for i := range raw.Segments.Len() {
		segment := raw.Segments.At(i)
		tag = append(tag, segment.Value(source)...)
	}
	if isFootnoteTag(string(tag)) {
		_, _ = w.Write(tag)
	} else {
		_, _ = w.Write(util.EscapeHTML(tag))
	}
	return ast.WalkSkipChildren, nil
}

// renderHTMLBlock shows a block of raw HTML as a paragraph of text.
func renderHTMLBlock(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	block, ok := node.(*ast.HTMLBlock)
	if !ok {
		return ast.WalkContinue, nil
	}
	var text []byte
	for i := range block.Lines().Len() {
		line := block.Lines().At(i)
		text = append(text, line.Value(source)...)
	}
	if block.HasClosure() {
		text = append(text, block.ClosureLine.Value(source)...)
	}
	_, _ = w.WriteString("<p>")
	_, _ = w.Write(util.EscapeHTML(bytes.TrimRight(text, "\r\n")))
	_, _ = w.WriteString("</p>\n")
	return ast.WalkContinue, nil
}

// isFootnoteTag reports whether tag is <sup> or </sup>, the markup the capture
// hook produces for footnote markers.
func isFootnoteTag(tag string) bool {
	return strings.EqualFold(tag, "<sup>") || strings.EqualFold(tag, "</sup>")
}

// ToHTML renders Markdown to an HTML fragment.
func ToHTML(md string) (string, error) {
	var buf bytes.Buffer
	if err := converter.Convert([]byte(md), &buf); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ParseFragment parses an HTML fragment as the content of a <body>. The HTML5
// parser repairs unbalanced tags (the capture can contain a stray <sup>), so
// the result is always a well-formed tree.
func ParseFragment(fragment string) ([]*xhtml.Node, error) {
	ctx := &xhtml.Node{Type: xhtml.ElementNode, Data: "body", DataAtom: atom.Body}
	return xhtml.ParseFragment(strings.NewReader(fragment), ctx)
}

// Text returns the visible text of an HTML fragment: all text nodes concatenated
// with entities decoded. Block elements are already separated by newlines in the
// HTML produced by ToHTML, so paragraphs end up on separate lines.
func Text(fragment string) string {
	nodes, err := ParseFragment(fragment)
	if err != nil {
		return fragment
	}
	var b strings.Builder
	for _, n := range nodes {
		collectText(&b, n)
	}
	return b.String()
}

func collectText(b *strings.Builder, n *xhtml.Node) {
	if n.Type == xhtml.TextNode {
		b.WriteString(n.Data)
		return
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		collectText(b, c)
	}
}
