package markdown

import (
	"strings"
	"testing"
)

func TestToHTML(t *testing.T) {
	got, err := ToHTML("# Title\n\nSome **bold** text with <sup>1</sup> and `code`.\n\n```\nx < y && z\n```\n\n![](images/a.jpg)\n\n- a\n- b\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"<h1>Title</h1>",
		"<strong>bold</strong>",
		"<sup>1</sup>", // raw inline HTML passes through
		"<code>code</code>",
		"<pre><code>x &lt; y &amp;&amp; z\n</code></pre>",
		`<img src="images/a.jpg" alt="" />`, // XHTML style
		"<li>a</li>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML lacks %q:\n%s", want, got)
		}
	}
}

// The capture hook writes <sup> around footnote markers; every other tag-like
// piece of text in a chapter is book text and must not be taken for markup
// (it would vanish from the text and could make an EPUB invalid).
func TestToHTMLShowsStrayTagsAsText(t *testing.T) {
	cases := []struct{ name, md, want string }{
		{"footnote markers are markup", "a<sup>1</sup>b", "<p>a<sup>1</sup>b</p>"},
		{"footnote markers in capitals", "a<SUP>1</SUP>b", "<p>a<SUP>1</SUP>b</p>"},
		{"a pseudo tag in prose", "if x<y and z>w then", "<p>if x&lt;y and z&gt;w then</p>"},
		{"obsolete tags", `<font color="red">red</font> text`, "<p>&lt;font color=&quot;red&quot;&gt;red&lt;/font&gt; text</p>"},
		{"a block of HTML", "<div>\nhello\n</div>", "<p>&lt;div&gt;\nhello\n&lt;/div&gt;</p>"},
		{"a comment", "<!-- note -->", "<p>&lt;!-- note --&gt;</p>"},
		{"a script block", "<script>alert(1)</script>", "<p>&lt;script&gt;alert(1)&lt;/script&gt;</p>"},
		{"code is not touched", "`<div>`", "<p><code>&lt;div&gt;</code></p>"},
		{"autolinks still work", "<https://example.com/a>", `<p><a href="https://example.com/a">https://example.com/a</a></p>`},
		{"a tag next to a footnote marker", "x<sup>1</sup><br>y", "<p>x<sup>1</sup>&lt;br&gt;y</p>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ToHTML(tc.md)
			if err != nil {
				t.Fatal(err)
			}
			if got = strings.TrimSpace(got); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}

	// The text view (TXT output) shows the tags the book contains.
	html, err := ToHTML("if x<y and z>w then <font>x</font>\n")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Text(html), "if x<y and z>w then <font>x</font>\n"; got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
}

func TestText(t *testing.T) {
	html, err := ToHTML("# Title\n\nFirst **para** &amp; more\n\nSecond <sup>2</sup> para\n")
	if err != nil {
		t.Fatal(err)
	}
	got := Text(html)
	want := "Title\nFirst para & more\nSecond 2 para\n"
	if got != want {
		t.Fatalf("Text = %q, want %q", got, want)
	}
}

func TestParseFragmentRepairsUnbalancedMarkup(t *testing.T) {
	nodes, err := ParseFragment("<p>a <sup>b</p><p>c</p>")
	if err != nil {
		t.Fatal(err)
	}
	out := SerializeXHTML(nodes)
	if strings.Count(out, "<p>") != strings.Count(out, "</p>") || strings.Count(out, "<sup>") != strings.Count(out, "</sup>") {
		t.Fatalf("not well formed: %s", out)
	}
}
