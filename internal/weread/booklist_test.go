package weread

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// nuxtPage mimics the minified Nuxt payload of /misc/booklist/<id>: titles are
// either literals (with escapes, commas and braces inside) or variables hoisted
// into the argument list of the wrapping function.
const nuxtPage = `<html><body><div id="__nuxt"></div><script>window.__NUXT__=(function(a,b,c,d,e,f){return {layout:"default",data:[{bookEntities:{` +
	`"912071":{bookId:c,isLecture:a,author:"[美]博恩·崔西",title:"吃掉那只青蛙：博恩·崔西的高效时间管理法则",cover:"https:\u002F\u002Fcdn.weread.qq.com\u002Fx.jpg",ratingTitle:e,likedCount:b,lectureInfo:{},isSoldOut:a},` +
	`"22523047":{bookId:d,title:e,author:"何苦",tags:[1,2,{x:"}"}]},` +
	`"333":{title:"a, \"b\" {c} \u4e2d\ud83d\ude00",author:f},` +
	`"abc":{title:"not a book entry"}}}],error:null,state:{}}}(0,1,"id-1","id-2","最后的棒棒","x,y"));</script></body></html>`

func TestParseBookList(t *testing.T) {
	entries, err := ParseBookList([]byte(nuxtPage))
	if err != nil {
		t.Fatal(err)
	}
	want := []BookListEntry{
		{BookID: "912071", ID: MustHash("912071"), Title: "吃掉那只青蛙：博恩·崔西的高效时间管理法则"},
		{BookID: "22523047", ID: MustHash("22523047"), Title: "最后的棒棒"},
		{BookID: "333", ID: MustHash("333"), Title: `a, "b" {c} 中😀`},
	}
	if len(entries) != len(want) {
		t.Fatalf("got %d entries: %+v", len(entries), entries)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, entries[i], want[i])
		}
	}
}

func TestParseBookListWithoutHoistedVariables(t *testing.T) {
	page := `<script>window.__NUXT__={data:[{bookEntities:{"1":{title:"One"},"2":{title:"Two"}}}]}</script>`
	entries, err := ParseBookList([]byte(page))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].Title != "One" || entries[1].Title != "Two" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestParseBookListErrors(t *testing.T) {
	for name, page := range map[string]string{
		"no nuxt payload": `<html>hello</html>`,
		"no entities":     `<script>window.__NUXT__={data:[{}]}</script>`,
		"empty entities":  `<script>window.__NUXT__={data:[{bookEntities:{}}]}</script>`,
		"no digit keys":   `<script>window.__NUXT__={data:[{bookEntities:{"abc":{title:"x"}}}]}</script>`,
		"truncated":       `<script>window.__NUXT__={data:[{bookEntities:{"1":{title:"x"`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseBookList([]byte(page)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestClientBookList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/misc/booklist/1_abc" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(nuxtPage))
	}))
	defer srv.Close()

	entries, err := newTestClient(t, srv.URL, nil).BookList(context.Background(), "1_abc")
	if err != nil || len(entries) != 3 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
	if _, err := newTestClient(t, srv.URL, nil).BookList(context.Background(), "nope"); err == nil {
		t.Fatal("expected an error for an unknown list")
	}
}

func TestExcerptTruncates(t *testing.T) {
	got := excerpt(strings.Repeat("word ", 100))
	if !strings.HasSuffix(got, "…") || len(got) > 170 {
		t.Fatalf("excerpt = %q (%d bytes)", got, len(got))
	}
	if excerpt("a \n b") != "a b" {
		t.Fatal("whitespace not collapsed")
	}
}
