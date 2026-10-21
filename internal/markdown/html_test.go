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
