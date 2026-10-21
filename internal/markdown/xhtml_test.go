package markdown

import (
	"encoding/xml"
	"io"
	"strings"
	"testing"
)

func serialize(t *testing.T, fragment string) string {
	t.Helper()
	nodes, err := ParseFragment(fragment)
	if err != nil {
		t.Fatal(err)
	}
	return SerializeXHTML(nodes)
}

// wellFormed parses s as XML (wrapped in a root element).
func wellFormed(t *testing.T, s string) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader("<root>" + s + "</root>"))
	for {
		if _, err := dec.Token(); err == io.EOF {
			return
		} else if err != nil {
			t.Fatalf("not well-formed XML: %v\n%s", err, s)
		}
	}
}

func TestSerializeXHTMLBasics(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"void elements are self-closed", `<p>a<br>b</p><hr><img src="x.jpg" alt="">`, `<p>a<br />b</p><hr /><img src="x.jpg" alt="" />`},
		{"text is escaped", `<p>1 &lt; 2 &amp; 3 &gt; 2</p>`, `<p>1 &lt; 2 &amp; 3 &gt; 2</p>`},
		{"attribute values are escaped", `<a href="?a=1&amp;b=&quot;2&quot;">x</a>`, `<a href="?a=1&amp;b=&quot;2&quot;">x</a>`},
		{"empty non-void elements are not self-closed", `<p></p><div></div>`, `<p></p><div></div>`},
		{"comments are dropped", `a<!-- secret -->b`, `ab`},
		{"named entities become characters", `<p>a&nbsp;b &copy; &mdash;</p>`, "<p>a\u00a0b © —</p>"},
		{"names are lower-cased", `<P CLASS="X">y</P>`, `<p class="X">y</p>`},
		{"nested structure", `<pre><code>x &lt; y</code></pre>`, `<pre><code>x &lt; y</code></pre>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := serialize(t, tc.in)
			if got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
			wellFormed(t, got)
		})
	}
}

func TestSerializeXHTMLDropsDangerousContent(t *testing.T) {
	in := `<p onclick="evil()">a</p><script>alert(1)</script><iframe src="x"></iframe>` +
		`<a href="javascript:alert(1)">l</a><style>p{}</style><object data="x"></object>` +
		`<form action="x"><input name="q"><button>go</button></form><img src="x.jpg" onerror="evil()">`
	got := serialize(t, in)
	for _, bad := range []string{"onclick", "onerror", "<script", "alert", "<iframe", "javascript:", "<style", "<object", "<form", "<input", "<button"} {
		if strings.Contains(got, bad) {
			t.Errorf("output still contains %q: %s", bad, got)
		}
	}
	if !strings.Contains(got, "<p>a</p>") || !strings.Contains(got, `<img src="x.jpg" />`) {
		t.Errorf("harmless content was lost: %s", got)
	}
	wellFormed(t, got)
}

func TestSerializeXHTMLRemovesCharactersXMLForbids(t *testing.T) {
	got := serialize(t, "<p>a\x00b\x01c\x0bd\x0ce\ufffe\uffff f\ttab</p>")
	if got != "<p>abcde f\ttab</p>" {
		t.Fatalf("got %q", got)
	}
	wellFormed(t, got)
}

func TestSerializeXHTMLIsAlwaysWellFormed(t *testing.T) {
	for _, in := range []string{
		"<p>unclosed <b>bold <i>italic",
		"<table><tr><td>cell",
		"</p></div>stray closers",
		"<a$b>odd tag</a$b>",
		`<p a$b="1" ="2" x:y="3" xmlns="u">attrs</p>`,
		"<svg><circle r=1></svg>",
		"a & b < c > d",
	} {
		wellFormed(t, serialize(t, in))
	}
}

func TestHasRemoteImages(t *testing.T) {
	parse := func(s string) bool {
		nodes, err := ParseFragment(s)
		if err != nil {
			t.Fatal(err)
		}
		return HasRemoteImages(nodes)
	}
	if !parse(`<p><img src="https://x/a.jpg"></p>`) || !parse(`<img src="HTTP://x/a.jpg">`) {
		t.Error("remote image not detected")
	}
	if parse(`<p><img src="images/a.jpg"></p>`) || parse(`<a href="https://x">link</a>`) || parse(`<p>text</p>`) {
		t.Error("false positive")
	}
}

func TestAssignAnchorIDs(t *testing.T) {
	nodes, err := ParseFragment(`<h1>第1章　起步</h1><p>intro</p><h2>1.1 搭建 环境</h2><p>text</p><h2>1.2 运行</h2><p>1.3 段落标题</p><p>end</p>`)
	if err != nil {
		t.Fatal(err)
	}
	ids := AssignAnchorIDs(nodes, []string{"1.1 搭建环境", "1.2 运行", "1.3 段落标题", "not in the chapter", ""}, "t")
	if want := []string{"t1", "t2", "t3", "", ""}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	out := SerializeXHTML(nodes)
	for _, want := range []string{`<h2 id="t1">1.1 搭建 环境</h2>`, `<h2 id="t2">1.2 运行</h2>`, `<p id="t3">1.3 段落标题</p>`} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
}

func TestAssignAnchorIDsKeepsDocumentOrderAndExistingIDs(t *testing.T) {
	nodes, err := ParseFragment(`<h2 id="mine">B</h2><h2>A</h2><h2>B</h2>`)
	if err != nil {
		t.Fatal(err)
	}
	// "A" comes first in the TOC but later in the document than the first "B":
	// matching proceeds forward only, so "B" (second TOC entry) binds to the last <h2>.
	ids := AssignAnchorIDs(nodes, []string{"A", "B"}, "t")
	if ids[0] != "t1" || ids[1] != "t2" {
		t.Fatalf("ids = %v", ids)
	}

	nodes, _ = ParseFragment(`<h2 id="mine">B</h2>`)
	if ids := AssignAnchorIDs(nodes, []string{"B"}, "t"); ids[0] != "mine" {
		t.Fatalf("an existing id must be reused, got %v", ids)
	}
}
