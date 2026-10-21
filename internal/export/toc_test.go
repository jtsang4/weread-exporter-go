package export

import (
	"reflect"
	"strings"
	"testing"
)

// render flattens a table of contents for comparison:
// "A[a1,a2]" is a section with children, "B" a plain entry.
func render(toc []*tocEntry) string {
	var parts []string
	for _, e := range toc {
		s := e.Title + "@" + e.Href
		if len(e.Children) > 0 {
			var kids []string
			for _, c := range e.Children {
				kids = append(kids, c.Title+"@"+c.Href)
			}
			s += "[" + strings.Join(kids, ",") + "]"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

func TestBuildTOC(t *testing.T) {
	ch := func(n int, title string, level int, anchors ...anchorTOC) chapterTOC {
		return chapterTOC{File: "c" + string(rune('0'+n)) + ".xhtml", Title: title, Level: level, Anchors: anchors}
	}
	cases := []struct {
		name     string
		chapters []chapterTOC
		want     string
	}{
		{
			"flat",
			[]chapterTOC{ch(1, "A", 1), ch(2, "B", 1)},
			"A@c1.xhtml B@c2.xhtml",
		},
		{
			"anchors make a section",
			[]chapterTOC{ch(1, "A", 1), ch(2, "B", 1, anchorTOC{"b1", "t1"}, anchorTOC{"b2", "t2"}), ch(3, "C", 1)},
			"A@c1.xhtml B@c2.xhtml[b1@c2.xhtml#t1,b2@c2.xhtml#t2] C@c3.xhtml",
		},
		{
			"anchor that was not found links to the chapter",
			[]chapterTOC{ch(1, "A", 1, anchorTOC{"a1", ""})},
			"A@c1.xhtml[a1@c1.xhtml]",
		},
		{
			"deeper chapters join the open section",
			[]chapterTOC{ch(1, "A", 1, anchorTOC{"a1", "t1"}), ch(2, "A.1", 2), ch(3, "A.2", 2), ch(4, "B", 1)},
			"A@c1.xhtml[a1@c1.xhtml#t1,A.1@c2.xhtml,A.2@c3.xhtml] B@c4.xhtml",
		},
		{
			"a deeper chapter turns the previous plain chapter into a section",
			[]chapterTOC{ch(1, "A", 1), ch(2, "B", 1), ch(3, "B.1", 2), ch(4, "B.2", 2), ch(5, "C", 1)},
			"A@c1.xhtml B@c2.xhtml[B.1@c3.xhtml,B.2@c4.xhtml] C@c5.xhtml",
		},
		{
			"the section still open at the end is kept (the original dropped it)",
			[]chapterTOC{ch(1, "A", 1), ch(2, "A.1", 2)},
			"A@c1.xhtml[A.1@c2.xhtml]",
		},
		{
			"last chapter with anchors is kept",
			[]chapterTOC{ch(1, "A", 1), ch(2, "B", 1, anchorTOC{"b1", "t1"})},
			"A@c1.xhtml B@c2.xhtml[b1@c2.xhtml#t1]",
		},
		{
			"a deeper chapter first does not crash (the original raised IndexError)",
			[]chapterTOC{ch(1, "Z", 2), ch(2, "A", 1)},
			"Z@c1.xhtml A@c2.xhtml",
		},
		{
			"empty",
			nil,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := render(buildTOC(tc.chapters)); got != tc.want {
				t.Fatalf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

func TestTOCDepth(t *testing.T) {
	flat := []*tocEntry{{Title: "a"}, {Title: "b"}}
	nested := []*tocEntry{{Title: "a", Children: []*tocEntry{{Title: "x", Children: []*tocEntry{{Title: "y"}}}}}}
	if tocDepth(nil) != 0 || tocDepth(flat) != 1 || tocDepth(nested) != 3 {
		t.Fatalf("depths: %d %d %d", tocDepth(nil), tocDepth(flat), tocDepth(nested))
	}
}

func TestUUIDv5MatchesTheRFCReference(t *testing.T) {
	dns := [16]byte{0x6b, 0xa7, 0xb8, 0x10, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}
	// python3 -c "import uuid; print(uuid.uuid5(uuid.NAMESPACE_DNS, 'python.org'))"
	if got := uuidV5(dns, "python.org"); got != "886313e1-3b8a-5372-9b90-0c9aee199e5d" {
		t.Fatalf("uuidV5 = %s", got)
	}
	a := uuidV5(namespaceURL, "https://weread.qq.com/web/bookDetail/x")
	if a != uuidV5(namespaceURL, "https://weread.qq.com/web/bookDetail/x") || a == uuidV5(namespaceURL, "https://weread.qq.com/web/bookDetail/y") {
		t.Fatal("uuid must be stable and depend on the name")
	}
	if !reflect.DeepEqual(len(a), 36) {
		t.Fatalf("bad format %s", a)
	}
}
