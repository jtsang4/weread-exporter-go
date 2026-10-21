package weread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/jtsang4/weread-exporter-go/internal/book"
)

// detailPage mimics the server-rendered state of /web/bookDetail/<id>: braces
// and quotes inside strings must not confuse the extraction, ids and levels may
// be strings, and the page mentions another sold-out book that must be ignored.
const detailPage = `<!DOCTYPE html><html><head><title>x</title></head><body>
<script>window.__INITIAL_STATE__={"reader":{"bookInfo":{"bookId":"34336681","title":"Python编程：从入门到实践（第2版）","author":"埃里克·马瑟斯","cover":"https://cdn.weread.qq.com/weread/cover/90/YueWen_34336681/t6_YueWen_34336681.jpg","intro":"简介 with } brace and \"quotes\"","soldout":0},
"chapterInfos":[
 {"chapterUid":41,"title":"版权信息","level":1,"wordCount":606,"anchors":[]},
 {"chapterUid":"42","title":"第1章 起步","level":"1","wordCount":"1312","anchors":[{"title":"1.1 搭建环境","level":2},{"title":"1.2 运行","level":"2"}]},
 {"chapterUid":43,"title":"1.1 小节","level":2,"wordCount":10}
]},"other":{"books":[{"soldout":1}]}};</script>
<script>window.__NUXT__ = {};</script></body></html>`

func TestParseBookDetail(t *testing.T) {
	detail, err := ParseBookDetail([]byte(detailPage))
	if err != nil {
		t.Fatal(err)
	}
	if detail.SoldOut {
		t.Fatal("soldout of another book leaked into this one")
	}
	m := detail.Meta
	if m.Title != "Python编程：从入门到实践（第2版）" || m.Author != "埃里克·马瑟斯" {
		t.Fatalf("title/author = %q / %q", m.Title, m.Author)
	}
	if !strings.HasSuffix(m.Cover, "/t6_YueWen_34336681.jpg") {
		t.Fatalf("cover = %q", m.Cover)
	}
	if m.Intro != `简介 with } brace and "quotes"` {
		t.Fatalf("intro = %q", m.Intro)
	}
	want := []book.Chapter{
		{ID: 41, Title: "版权信息", Level: 1, Words: 606, Anchors: []book.Anchor{}},
		{ID: 42, Title: "第1章 起步", Level: 1, Words: 1312, Anchors: []book.Anchor{{Title: "1.1 搭建环境", Level: 2}, {Title: "1.2 运行", Level: 2}}},
		{ID: 43, Title: "1.1 小节", Level: 2, Words: 10, Anchors: []book.Anchor{}},
	}
	if !reflect.DeepEqual(m.Chapters, want) {
		t.Fatalf("chapters = %+v\nwant      %+v", m.Chapters, want)
	}
}

func TestParseBookDetailSoldOut(t *testing.T) {
	page := strings.Replace(detailPage, `"soldout":0`, `"soldout":1`, 1)
	detail, err := ParseBookDetail([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if !detail.SoldOut {
		t.Fatal("soldout not detected")
	}
}

func TestParseBookDetailErrors(t *testing.T) {
	for name, page := range map[string]string{
		"no state":        `<html>nothing here</html>`,
		"marker no value": `<script>window.__INITIAL_STATE__ = undefined</script>`,
		"truncated json":  `<script>window.__INITIAL_STATE__={"reader":{</script>`,
		"wrong shape":     `<script>window.__INITIAL_STATE__={"reader":{"bookInfo":{"title":5}}}</script>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBookDetail([]byte(page)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestClientBookDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/web/bookDetail/abc12345" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(detailPage))
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, nil)
	detail, err := c.BookDetail(context.Background(), "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Meta.Chapters) != 3 {
		t.Fatalf("chapters = %d", len(detail.Meta.Chapters))
	}
	if _, err := c.BookDetail(context.Background(), "missing1"); err == nil {
		t.Fatal("expected an error for a missing book")
	}
}
