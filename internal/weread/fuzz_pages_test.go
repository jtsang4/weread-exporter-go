package weread

import (
	"testing"
)

// FuzzParsePages feeds arbitrary bytes to the parsers of what the site sends
// (book pages, book list pages, reader pages, cookie text). Whatever a page
// contains, they must neither panic nor hang, and a parse that reports success
// must hand out consistent data. Run it with
//
//	go test ./internal/weread -fuzz FuzzParsePages -fuzztime 30s
func FuzzParsePages(f *testing.F) {
	for _, seed := range []string{
		"",
		`<script>window.__INITIAL_STATE__={"reader":{"bookInfo":{"title":"T","author":"A","cover":"https://x/s_1.jpg","intro":"i","soldout":0},` +
			`"chapterInfos":[{"chapterUid":41,"title":"c","level":1,"wordCount":"123","anchors":[{"title":"a","level":2}]}]},"config":{"useHorizonReader":1}};</script>`,
		`window.__INITIAL_STATE__ = {"config":{"useHorizonReader":"0"}}`,
		`window.__INITIAL_STATE__={"reader":{"chapterInfos":[{"chapterUid":null,"level":true,"anchors":null}]}}`,
		`<script>window.__NUXT__=(function(a,b){return {data:[{bookEntities:{"912071":{bookId:a,title:b},"x":{title:"y"},"34336681":{title:'q\'s'}}}]}}("912071","Book Title"))</script>`,
		`window.__NUXT__={bookEntities:{"1":{title:"\u0041\x42\n"}}}`,
		`window.__NUXT__=(function(a){return {bookEntities:{`,
		`window.__NUXT__ bookEntities: { "1": { title: `,
		`{"wr_vid":"1","wr_skey":"abc"}`,
		"wr_vid=1; wr_skey=abc; ; =x; y=",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, page []byte) {
		if detail, err := ParseBookDetail(page); err == nil && (detail == nil || detail.Meta == nil) {
			t.Fatalf("no book detail, no error, for %q", page)
		}
		if entries, err := ParseBookList(page); err == nil {
			if len(entries) == 0 {
				t.Fatalf("an empty book list without an error for %q", page)
			}
			for _, e := range entries {
				if !isDigits(e.BookID) || !hashedIDPattern.MatchString(e.ID) {
					t.Fatalf("inconsistent entry %+v for %q", e, page)
				}
			}
		}
		if mode, err := ParseReaderMode(page); err == nil && mode != ReaderModeHorizontal && mode != ReaderModeVertical {
			t.Fatalf("reader mode %d for %q", mode, page)
		}
		_ = ParseCookieText(string(page))
	})
}
