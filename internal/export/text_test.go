package export

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func removeFile(path string) error { return os.Remove(path) }

func TestWriteTXT(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTXT(&buf, testSource(t)); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	contains(t, got,
		"版权信息\n版权所有 1\n\n",
		"第1章 起步\n1.1 搭建环境\n先安装 python。\n",
		"print('hi < there')\n",
		"变量内容 & 更多。\n\n",
	)
	notContains(t, got, "<h1>", "<sup>", "&amp;", "&lt;", "![](")
	if !strings.HasSuffix(got, "字符串。\n\n") {
		t.Fatalf("each chapter must end with a blank line, got ...%q", got[max(0, len(got)-30):])
	}
	// chapters keep their order
	if strings.Index(got, "版权信息") > strings.Index(got, "第1章 起步") || strings.Index(got, "第1章 起步") > strings.Index(got, "第2章 变量") {
		t.Fatal("chapters out of order")
	}
}

func TestWriteTXTRequiresEveryChapter(t *testing.T) {
	src := testSource(t)
	_ = removeFile(src.Store.ChapterPath(0, src.Meta.Chapters[0].ID))
	if err := WriteTXT(&bytes.Buffer{}, src); err == nil {
		t.Fatal("expected an error")
	}
}

func TestWriteMarkdownMergesChaptersAndCopiesImages(t *testing.T) {
	src := testSource(t)
	out := filepath.Join(t.TempDir(), "My Book (v2).md")
	if err := WriteMarkdown(out, src); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	contains(t, got,
		"# 版权信息\n",
		"# 第1章 起步\n",
		"# 2.1 字符串\n",
		"![](My%20Book%20%28v2%29.assets/"+imgLegacyPNG+")",
		"![](My%20Book%20%28v2%29.assets/"+imgRealJPG+")",
		"![](My%20Book%20%28v2%29.assets/missing.jpg)", // dangling link stays visible, the copy only warns
		"![](https://cdn.example.invalid/remote.png)",
	)
	notContains(t, got, "](images/")

	assets := filepath.Join(filepath.Dir(out), "My Book (v2).assets")
	for _, name := range []string{imgLegacyPNG, imgRealJPG} {
		if _, err := os.Stat(filepath.Join(assets, name)); err != nil {
			t.Errorf("image %s was not copied: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(assets, imgUnused)); err == nil {
		t.Error("an image the text does not refer to was copied")
	}
	// chapters are separated by a blank line
	if !strings.Contains(got, "<sup>1</sup>\n\n# 第1章") {
		t.Errorf("chapters are not separated by a newline:\n%s", got)
	}
}

func TestWriteMarkdownWithoutImagesCreatesNoAssetsDirectory(t *testing.T) {
	src := testSource(t)
	for i, ch := range src.Meta.Chapters {
		if err := src.Store.SaveChapter(i, ch.ID, "# "+ch.Title+"\n\ntext\n"); err != nil {
			t.Fatal(err)
		}
	}
	dir := t.TempDir()
	if err := WriteMarkdown(filepath.Join(dir, "book.md"), src); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "book.md" {
		t.Fatalf("directory holds %v", entries)
	}
}
