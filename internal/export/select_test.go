package export

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestSelectionValidate(t *testing.T) {
	src := testSource(t)
	tests := []struct {
		name    string
		sel     []int
		wantErr string
	}{
		{"all", nil, ""},
		{"subset", []int{1, 3}, ""},
		{"single", []int{4}, ""},
		{"empty", []int{}, "no chapters selected"},
		{"out of range", []int{1, 9}, "out of range"},
		{"negative", []int{-1}, "out of range"},
		{"descending", []int{3, 1}, "strictly ascending"},
		{"duplicate", []int{1, 1}, "strictly ascending"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := src
			s.Select = tc.sel
			err := s.Validate()
			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestSelectionOnlyNeedsTheSelectedChaptersCaptured(t *testing.T) {
	src := testSource(t)
	if err := removeFile(src.Store.ChapterPath(0, src.Meta.Chapters[0].ID)); err != nil {
		t.Fatal(err)
	}
	if err := src.Validate(); err == nil {
		t.Fatal("a missing chapter must fail the full export")
	}
	src.Select = []int{1, 2}
	if err := src.Validate(); err != nil {
		t.Fatalf("the selected chapters are all captured: %v", err)
	}
	src.Select = []int{0}
	if err := src.Validate(); err == nil || !strings.Contains(err.Error(), "has not been captured") {
		t.Fatalf("selecting the missing chapter must fail, got %v", err)
	}
}

func TestSelectedTXTAndMarkdown(t *testing.T) {
	src := testSource(t)
	src.Select = []int{1, 3}

	var txt bytes.Buffer
	if err := WriteTXT(&txt, src); err != nil {
		t.Fatal(err)
	}
	contains(t, txt.String(), "第1章 起步", "变量内容")
	notContains(t, txt.String(), "版权信息", "小节内容", "字符串。")

	dir := t.TempDir()
	out := dir + "/part.md"
	if err := WriteMarkdown(out, src); err != nil {
		t.Fatal(err)
	}
	data := readFile(t, out)
	contains(t, data, "# 第1章 起步", "# 第2章 变量")
	notContains(t, data, "# 版权信息", "# 1.1 小节", "# 2.1 字符串")
}

func TestSelectedPDFHTML(t *testing.T) {
	src := testSource(t)
	src.Select = []int{2}
	html, err := PDFHTML(src, PDFOptions{})
	if err != nil {
		t.Fatal(err)
	}
	contains(t, html, "小节内容")
	notContains(t, html, "版权所有", "变量内容")
	if n := strings.Count(html, `<section class="chapter">`); n != 1 {
		t.Errorf("%d chapter sections, want 1", n)
	}
}

func TestSelectedEPUB(t *testing.T) {
	src := testSource(t)
	src.Select = []int{1, 2, 4} // chapter 3 of the book, level 2, follows chapter 2; 5 hangs under an unselected parent

	var buf bytes.Buffer
	if err := WriteEPUB(&buf, src, EPUBOptions{Now: testNow}); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(rc)
		_ = rc.Close()
		files[f.Name] = string(data)
	}

	// The chapter files are numbered in the order of the selection.
	for _, name := range []string{"EPUB/chap_0001.xhtml", "EPUB/chap_0002.xhtml", "EPUB/chap_0003.xhtml"} {
		if _, ok := files[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if _, ok := files["EPUB/chap_0004.xhtml"]; ok {
		t.Error("the selection has three chapters, found a fourth file")
	}
	contains(t, files["EPUB/chap_0001.xhtml"], "第1章 起步")
	contains(t, files["EPUB/chap_0002.xhtml"], "小节内容")
	contains(t, files["EPUB/chap_0003.xhtml"], "字符串")
	nav := files["EPUB/nav.xhtml"]
	contains(t, nav, "第1章 起步", "1.1 小节", "2.1 字符串")
	notContains(t, nav, "版权信息", "第2章 变量")
	opf := files["EPUB/content.opf"]
	if n := strings.Count(opf, `<itemref idref="chap`); n != 3 {
		t.Errorf("%d chapters in the spine, want 3:\n%s", n, opf)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
