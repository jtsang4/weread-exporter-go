package markdown

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/renderer/html"
	xhtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// converter renders CommonMark. Raw HTML is allowed because the capture contains
// inline <sup> tags for footnote markers; XHTML-style void tags (<br />, <hr />)
// keep the output usable inside EPUB documents.
var converter = goldmark.New(
	goldmark.WithRendererOptions(html.WithUnsafe(), html.WithXHTML()),
)

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
