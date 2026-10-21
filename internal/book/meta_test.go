package book

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestChapterUnmarshalAcceptsNumericAndStringIDs(t *testing.T) {
	var chapters []Chapter
	in := `[{"id":41,"title":"a","level":1,"words":10,"anchors":[]},
	        {"id":"42","title":"b","level":2,"words":0,"anchors":[{"title":"x","level":3}]},
	        {"title":"no id"},
	        {"id":null,"title":"null id"}]`
	if err := json.Unmarshal([]byte(in), &chapters); err != nil {
		t.Fatal(err)
	}
	if chapters[0].ID != 41 || chapters[1].ID != 42 || chapters[2].ID != 0 || chapters[3].ID != 0 {
		t.Fatalf("ids = %d %d %d %d", chapters[0].ID, chapters[1].ID, chapters[2].ID, chapters[3].ID)
	}
	if chapters[1].Title != "b" || chapters[1].Level != 2 || len(chapters[1].Anchors) != 1 || chapters[1].Anchors[0].Level != 3 {
		t.Fatalf("chapter 1 = %+v", chapters[1])
	}
}

func TestChapterUnmarshalRejectsGarbageID(t *testing.T) {
	var c Chapter
	if err := json.Unmarshal([]byte(`{"id":"abc"}`), &c); err == nil {
		t.Fatal("expected an error")
	}
}

func TestMetaRoundTrip(t *testing.T) {
	want := &Meta{
		Title: "书名 & <title>", Author: "作者", Cover: "https://x/y.jpg", Intro: "intro",
		Chapters: []Chapter{
			{ID: 41, Title: "c1", Level: 1, Words: 5, Anchors: []Anchor{{Title: "a", Level: 2}}},
			{ID: 42, Title: "c2", Level: 2, Words: 6, Anchors: []Anchor{}},
		},
	}
	data, err := EncodeMeta(want)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "书名 & <title>") {
		t.Fatalf("HTML characters must not be escaped in meta.json: %s", data)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Fatal("meta.json should end with a newline")
	}
	var got Meta
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&got, want) {
		t.Fatalf("got %+v\nwant %+v", got, *want)
	}
}

func TestMetaValidate(t *testing.T) {
	ok := &Meta{Title: "t", Chapters: []Chapter{{ID: 1}}}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, m := range map[string]*Meta{
		"nil":         nil,
		"no title":    {Title: "  ", Chapters: []Chapter{{ID: 1}}},
		"no chapters": {Title: "t"},
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCoverURLs(t *testing.T) {
	const dir = "https://cdn.weread.qq.com/weread/cover/90/YueWen_1/"
	cases := []struct {
		name  string
		cover string
		want  []string
	}{
		{"t6 upgraded to t9", dir + "t6_YueWen_1.jpg", []string{dir + "t9_YueWen_1.jpg", dir + "t6_YueWen_1.jpg"}},
		{"t5 upgraded to t9", dir + "t5_YueWen_1.jpg", []string{dir + "t9_YueWen_1.jpg", dir + "t5_YueWen_1.jpg"}},
		{"thumbnail upgraded to t9", dir + "s_YueWen_1.jpg", []string{dir + "t9_YueWen_1.jpg", dir + "s_YueWen_1.jpg"}},
		{"already t9", dir + "t9_YueWen_1.jpg", []string{dir + "t9_YueWen_1.jpg"}},
		{"no size prefix", dir + "cover.jpg", []string{dir + "cover.jpg"}},
		{"only the file name is rewritten", "https://x/t6_dir/s_a.jpg", []string{"https://x/t6_dir/t9_a.jpg", "https://x/t6_dir/s_a.jpg"}},
		{"empty", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := (&Meta{Cover: tc.cover}).CoverURLs()
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
