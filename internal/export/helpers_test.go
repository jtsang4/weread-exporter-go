package export

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/book"
)

var testNow = time.Date(2026, 10, 22, 8, 30, 15, 0, time.UTC)

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func testPNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		for y := 0; y < 8; y++ {
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const (
	imgLegacyPNG = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.jpg" // PNG data under a .jpg name, as written by the Python tool
	imgRealJPG   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.jpg"
	imgUnused    = "cccccccccccccccccccccccccccccccc.jpg"
)

// testSource builds a small captured book in a temporary cache:
//
//	1 "版权信息"        level 1
//	2 "第1章 起步"      level 1, anchors "1.1 搭建环境" and "1.2 运行"
//	3 "1.1 小节"        level 2
//	4 "第2章 变量"      level 1
//	5 "2.1 字符串"      level 2
//
// chapter 2 contains images (one stored under a misleading name, one missing
// from the cache, one remote) and a code block.
func testSource(t *testing.T) Source {
	t.Helper()
	store := book.NewStore(t.TempDir(), "08232ac0720befa90825d88")
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	meta := &book.Meta{
		Title:  "Python编程：从入门到实践 & <第2版>",
		Author: "埃里克·马瑟斯",
		Cover:  "https://cdn.example.invalid/t6_cover.jpg",
		Intro:  "一本\"书\"的简介 & more",
		Chapters: []book.Chapter{
			{ID: 41, Title: "版权信息", Level: 1, Anchors: []book.Anchor{}},
			{ID: 42, Title: "第1章 起步", Level: 1, Anchors: []book.Anchor{{Title: "1.1 搭建环境", Level: 2}, {Title: "1.2 运行", Level: 2}}},
			{ID: 43, Title: "1.1 小节", Level: 2, Anchors: []book.Anchor{}},
			{ID: 44, Title: "第2章 变量", Level: 1, Anchors: []book.Anchor{}},
			{ID: 45, Title: "2.1 字符串", Level: 2, Anchors: []book.Anchor{}},
		},
	}
	chapters := map[int]string{
		0: "# 版权信息\n\n版权所有 <sup>1</sup>\n",
		1: "# 第1章 起步\n\n## 1.1 搭建环境\n\n先安装 `python`。\n\n![](images/" + imgLegacyPNG + ")\n\n## 1.2 运行\n\n```\nprint('hi < there')\n```\n\n![](images/" + imgRealJPG + ")\n\n![](images/missing.jpg)\n\n![](https://cdn.example.invalid/remote.png)\n",
		2: "# 1.1 小节\n\n小节内容。\n",
		3: "# 第2章 变量\n\n变量内容 & 更多。\n",
		4: "# 2.1 字符串\n\n字符串。\n",
	}
	for i, ch := range meta.Chapters {
		if err := store.SaveChapter(i, ch.ID, chapters[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SaveCover(testJPEG(t)); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		imgLegacyPNG: testPNG(t, color.RGBA{R: 255, A: 255}),
		imgRealJPG:   testJPEG(t),
		imgUnused:    testPNG(t, color.RGBA{B: 255, A: 255}),
	} {
		if err := store.SaveImage(name, data); err != nil {
			t.Fatal(err)
		}
	}
	return Source{BookID: "08232ac0720befa90825d88", Meta: meta, Store: store, Logger: quietLogger}
}

func contains(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			t.Errorf("missing %q in:\n%s", n, haystack)
		}
	}
}

func notContains(t *testing.T, haystack string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			t.Errorf("unexpected %q in:\n%s", n, haystack)
		}
	}
}
