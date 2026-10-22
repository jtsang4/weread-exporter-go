package export

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// complaint matches the lines in which EPUBCheck reports a problem of any
// severity (messages look like "ERROR(RSC-006): path(line,column): text").
var complaint = regexp.MustCompile(`(?m)^(FATAL|ERROR|WARNING|USAGE)\(`)

// richMarkdown uses every construct that CommonMark (and so a captured chapter)
// can turn into HTML, plus the odd bits the capture hook produces and some
// hostile ones: raw HTML and obsolete tags that a book's text might contain.
const richMarkdown = "# 标题 & <符号>\n\n" +
	"段落，**加粗**、*斜体*、`code`、[链接](https://example.com/a?b=1&c=2)、上标<sup>1</sup>、行尾两个空格换行  \n下一行。\n\n" +
	"## 二级标题\n\n### 三级标题\n\n#### 四级标题\n\n##### 五级标题\n\n###### 六级标题\n\n" +
	"> 引用\n>\n> 多段 & 更多\n\n" +
	"- 列表一\n- 列表二\n  - 嵌套一\n  - 嵌套二\n\n" +
	"1. 有序一\n2. 有序二\n\n" +
	"```go\nfmt.Println(\"a < b && c > d\")\n```\n\n" +
	"    缩进代码块\n\n" +
	"---\n\n" +
	"![带说明的图](images/" + imgRealJPG + ")\n\n" +
	"第一段\n\n\n\n\n第二段（前面有多个空行）\n\n" +
	"if x<y and z>w then\n\n" +
	"<div>原始 HTML</div>\n\n<font color=\"red\">过时标签</font>\n"

// TestEPUBPassesEPUBCheck validates the generated packages with the W3C's
// EPUBCheck (https://www.w3.org/publishing/epubcheck/) when it is installed.
// Warnings count as failures. The test is skipped without the checker and with
// -short, because the checker is a Java program that takes seconds to start;
// CI sets WEREAD_TEST_REQUIRE_EPUBCHECK, which turns the missing checker into a
// failure so that the test cannot be skipped by accident.
func TestEPUBPassesEPUBCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("EPUBCheck is skipped with -short")
	}
	checker, err := exec.LookPath("epubcheck")
	if err != nil {
		if os.Getenv("WEREAD_TEST_REQUIRE_EPUBCHECK") != "" {
			t.Fatalf("EPUBCheck is required (WEREAD_TEST_REQUIRE_EPUBCHECK is set): %v", err)
		}
		t.Skip("epubcheck is not installed")
	}

	cases := []struct {
		name  string
		tweak func(src *Source) error // changes the test book before it is exported
		opts  EPUBOptions
	}{
		{name: "whole book"},
		{name: "without a cover", tweak: func(src *Source) error { return removeFile(src.Store.CoverPath()) }},
		{name: "some chapters", tweak: func(src *Source) error { src.Select = []int{1, 2, 4}; return nil }},
		{name: "one chapter", tweak: func(src *Source) error { src.Select = []int{0}; return nil }},
		{name: "untitled chapter", tweak: func(src *Source) error {
			src.Meta.Chapters[2].Title = " "
			src.Meta.Chapters[3].Title = ""
			return nil
		}},
		{name: "blank chapter", tweak: func(src *Source) error {
			return src.Store.SaveChapter(2, src.Meta.Chapters[2].ID, "\n\n\n\n")
		}},
		{name: "irregular table of contents levels", tweak: func(src *Source) error {
			for i, level := range []int{3, 1, 4, 2, 3} { // starts deep, skips levels, jumps up and down
				src.Meta.Chapters[i].Level = level
			}
			return nil
		}},
		{name: "every kind of markup", tweak: func(src *Source) error {
			return src.Store.SaveChapter(0, src.Meta.Chapters[0].ID, richMarkdown)
		}},
		{name: "extra CSS", opts: EPUBOptions{ExtraCSS: "p { text-indent: 2em; }"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := testSource(t)
			if tc.tweak != nil {
				if err := tc.tweak(&src); err != nil {
					t.Fatal(err)
				}
			}
			opts := tc.opts
			opts.Now = testNow
			var buf bytes.Buffer
			if err := WriteEPUB(&buf, src, opts); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(t.TempDir(), "book.epub")
			if err := os.WriteFile(file, buf.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
			defer cancel()
			out, err := exec.CommandContext(ctx, checker, file).CombinedOutput()
			if err != nil || complaint.Match(out) {
				t.Fatalf("EPUBCheck is not happy (%v):\n%s", err, out)
			}
		})
	}
}
