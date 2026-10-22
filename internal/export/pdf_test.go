package export

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

func TestPDFHTML(t *testing.T) {
	src := testSource(t)
	doc, err := PDFHTML(src, PDFOptions{ExtraCSS: "p { color: red; }"})
	if err != nil {
		t.Fatal(err)
	}
	contains(t, doc,
		"<!DOCTYPE html>",
		`<meta charset="utf-8" />`,
		"<title>Python编程：从入门到实践 &amp; &lt;第2版&gt;</title>",
		`<div class="cover"><img src="cover.jpg" alt="" /></div>`,
		"@page {",
		"p { color: red; }",
		"<h1>第1章 起步</h1>",
		`<img src="images/`+imgLegacyPNG+`" alt="" />`,
		"<pre><code>print('hi &lt; there')\n</code></pre>",
	)
	if n := strings.Count(doc, `<section class="chapter">`); n != 5 {
		t.Fatalf("%d chapter sections, want 5", n)
	}
	if strings.Index(doc, "版权信息") > strings.Index(doc, "第2章 变量") {
		t.Fatal("chapters out of order")
	}
	if strings.Contains(doc, `<div class="page-break">`) {
		t.Fatal("page breaks must come from the chapter sections (no blank page after the last chapter)")
	}
}

func TestPDFHTMLWithoutCover(t *testing.T) {
	src := testSource(t)
	if err := removeFile(src.Store.CoverPath()); err != nil {
		t.Fatal(err)
	}
	doc, err := PDFHTML(src, PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	notContains(t, doc, `class="cover"`)
}

func TestPDFHTMLNeutralisesActiveContent(t *testing.T) {
	src := testSource(t)
	hostile := "# T\n\n<script>fetch('file:///etc/passwd')</script>\n\n<img src=x onerror=\"alert(1)\">\n\n<iframe src=\"https://evil.invalid\"></iframe>\n"
	if err := src.Store.SaveChapter(0, src.Meta.Chapters[0].ID, hostile); err != nil {
		t.Fatal(err)
	}
	doc, err := PDFHTML(src, PDFOptions{ExtraCSS: "</style><script>alert(2)</script>"})
	if err != nil {
		t.Fatal(err)
	}
	// Whatever the user's CSS contains stays inside the (raw text) <style> element.
	if n := strings.Count(doc, "</style>"); n != 1 {
		t.Fatalf("%d closing style tags: user CSS broke out of the element", n)
	}
	page := doc[strings.Index(doc, "</style>")+len("</style>"):]

	// The hostile markup of the chapter is book text: it is shown as escaped
	// text and must not become an element or an attribute of the page.
	contains(t, page, "&lt;script&gt;", "&lt;iframe", `onerror="alert(1)"&gt;`)
	notContains(t, page, "<script", "<iframe", "<img src=\"x\"")
	root, err := xhtml.Parse(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(n *xhtml.Node)
	walk = func(n *xhtml.Node) {
		if n.Type == xhtml.ElementNode {
			switch n.Data {
			case "script", "iframe", "object", "embed", "form", "base":
				t.Errorf("active element <%s> in the page", n.Data)
			}
			for _, a := range n.Attr {
				if strings.HasPrefix(strings.ToLower(a.Key), "on") {
					t.Errorf("event handler %s=%q on <%s>", a.Key, a.Val, n.Data)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
}

func TestPDFHTMLRequiresEveryChapter(t *testing.T) {
	src := testSource(t)
	_ = removeFile(src.Store.ChapterPath(1, src.Meta.Chapters[1].ID))
	if _, err := PDFHTML(src, PDFOptions{}); err == nil {
		t.Fatal("expected an error")
	}
}
