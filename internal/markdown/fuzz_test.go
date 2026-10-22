package markdown

import (
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// FuzzToXHTML feeds arbitrary text through the conversion that every chapter
// goes through on its way into an EPUB and checks the properties that EPUB
// readers depend on. Run it with
//
//	go test ./internal/markdown -fuzz FuzzToXHTML -fuzztime 30s
//
// (without -fuzz the seeds below run as an ordinary test).
func FuzzToXHTML(f *testing.F) {
	for _, seed := range []string{
		"",
		"# 标题\n\n正文<sup>1</sup>\n",
		"if x<y and z>w then",
		"<div>\n<font color=\"red\">x</font>\n</div>",
		"<!-- c -->\n<script>alert(1)</script>",
		"<pre>\nraw\n</pre>",
		"<p onclick=\"x()\">a</p><img src=x onerror=y>",
		"[l](javascript:alert(1)) ![i](data:text/html,x) <https://example.com>",
		"```\n<b>\n```\n\n    <i>\n",
		"a\x00b\x0bc\ufffe<sup>",
		"</sup><sup></sup><sup>",
		"&amp; &lt; &#0; &#xD800; &bogus;",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, md string) {
		html, err := ToHTML(md)
		if err != nil {
			t.Fatalf("ToHTML: %v", err)
		}
		nodes, err := ParseFragment(html)
		if err != nil {
			t.Fatalf("ParseFragment: %v", err)
		}
		nodes, _ = LinkRemoteImages(nodes, "[图片]")
		out := SerializeXHTML(nodes)

		dec := xml.NewDecoder(strings.NewReader("<root>" + out + "</root>"))
		for {
			if _, err := dec.Token(); errors.Is(err, io.EOF) {
				break
			} else if err != nil {
				t.Fatalf("not well-formed XML: %v\ninput:  %q\noutput: %q", err, md, out)
			}
		}

		// No active content: look at the elements and attributes, not at the
		// text (a book may well talk about <script> and onerror).
		parsed, err := ParseFragment(out)
		if err != nil {
			t.Fatalf("ParseFragment(output): %v", err)
		}
		var walk func(n *xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode {
				switch n.Data {
				case "script", "iframe", "object", "embed", "form", "base", "link", "meta", "style":
					t.Fatalf("active element <%s>\ninput:  %q\noutput: %q", n.Data, md, out)
				}
				for _, a := range n.Attr {
					v := strings.ToLower(strings.TrimSpace(a.Val))
					if strings.HasPrefix(strings.ToLower(a.Key), "on") || strings.HasPrefix(v, "javascript:") {
						t.Fatalf("active attribute %s=%q\ninput:  %q\noutput: %q", a.Key, a.Val, md, out)
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		for _, n := range parsed {
			walk(n)
		}
	})
}
