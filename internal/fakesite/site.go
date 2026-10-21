// Package fakesite is an in-process imitation of weread.qq.com for tests.
//
// It serves the pages and endpoints the exporter talks to: book detail pages
// with the server-rendered state, a reader page that draws its text on a canvas
// (the way the real reader does, so the capture hook has something to hook),
// the account endpoints (profile, session renewal, reader mode) and telemetry
// endpoints that must never be reached. It is only meant to be imported by test
// code.
package fakesite

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"

	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// TB is the part of testing.TB the site needs.
type TB interface {
	Helper()
	Cleanup(func())
}

// Line is one fillText call of the reader canvas.
type Line struct {
	Text  string  `json:"text"`
	Font  string  `json:"font"`
	Color string  `json:"color"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
}

// Colours and fonts the capture hook was calibrated for.
const (
	BodyColor      = "rgb(208, 211, 216)"
	HighlightColor = "rgb(255, 120, 0)"
	BodyFont       = "20px PingFang SC"
	HeadingFont    = "30px PingFang SC"
	SubheadFont    = "24px PingFang SC"

	// PaywallFooter is what the reader's footer button says for a chapter the
	// signed-in reader has not bought.
	PaywallFooter = "会员卡 19元/月 · 开通后即可阅读"
)

// Body is a body text line at height y.
func Body(text string, y float64) Line {
	return Line{Text: text, Font: BodyFont, Color: BodyColor, X: 40, Y: y}
}

// At is a line of the given font size in the body colour.
func At(text string, size int, x, y float64) Line {
	return Line{Text: text, Font: strconv.Itoa(size) + "px PingFang SC", Color: BodyColor, X: x, Y: y}
}

// Anchor is a sub-heading listed in the table of contents.
type Anchor struct {
	Title string
	Level int
}

// Chapter is one reader page and, inside a Book, one entry of its table of contents.
type Chapter struct {
	UID   int64
	Title string
	Level int // table-of-contents level, default 1
	Words int // default: derived from the text
	// Anchors are the sub-headings listed in the table of contents.
	Anchors []Anchor

	// Screens holds the canvas content of each page of the chapter.
	Screens [][]Line
	// LastFooter is the footer button of the last page. In a Book the default is
	// "下一章" ("" for the last chapter, which has no button); for pages added
	// with AddPage it is exactly what is set.
	LastFooter string
	// NeedsLogin makes the reader show a "登录" footer to anonymous visitors.
	NeedsLogin bool
	// Paywalled shows "登录" to anonymous visitors and PaywallFooter to signed-in ones.
	Paywalled bool
	// Horizontal serves the paged two-column reader instead.
	Horizontal bool
	// RedirectTo sends the browser elsewhere.
	RedirectTo string
	// Extra is raw HTML placed inside div.passage-content (pre/img/hr elements).
	Extra string
}

// Book is a book with a detail page and readable chapters.
type Book struct {
	// ID is the hashed book id used in URLs.
	ID      string
	Title   string
	Author  string
	Intro   string
	Cover   string // default: a WeRead CDN style URL
	SoldOut bool

	Chapters []Chapter
}

// ListEntry is one book of a book list.
type ListEntry struct {
	BookID string // numeric id
	Title  string
}

// Account is the signed-in user of the site.
type Account struct {
	VID          string
	Name         string
	RefreshToken string
}

// Site is the fake server. Create it with New.
type Site struct {
	// NoScan keeps the QR code of the login unscanned: the login never completes.
	NoScan bool

	srv   *httptest.Server
	other *httptest.Server // plays oss.weread.qq.com for cross-origin telemetry

	mu         sync.Mutex
	pages      map[string]page
	books      map[string]Book
	lists      map[string][]ListEntry
	account    *Account
	skeys      map[string]bool
	renewals   int
	mode       int // useHorizonReader of the account
	modeSets   []int
	skeyNumber int

	documentCooks []string
	telemetryHits []string
	fetched       []string
	readerHits    int
}

type page struct {
	ch        Chapter
	modeAware bool // follows the account's reader mode (pages of a Book)
}

// New starts a site that is shut down when the test ends.
func New(t TB) *Site {
	t.Helper()
	s := &Site{
		pages: map[string]page{},
		books: map[string]Book{},
		lists: map[string][]ListEntry{},
		skeys: map[string]bool{},
		mode:  1, // new accounts default to the paged reader
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/web/bookDetail/", s.bookDetail)
	mux.HandleFunc("/web/reader/", s.reader)
	mux.HandleFunc("/misc/booklist/", s.bookList)
	mux.HandleFunc("/web/user", s.user)
	mux.HandleFunc("/web/login/renewal", s.renewal)
	mux.HandleFunc("/web/user_config/modify", s.configModify)
	mux.HandleFunc("/fake/login", s.login)
	mux.HandleFunc("/avatar.png", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/img/", s.image)
	mux.HandleFunc("/wrepub/", s.image)
	mux.HandleFunc("/hera/", s.telemetry)
	mux.HandleFunc("/i/hera/", s.telemetry)
	mux.HandleFunc("/sentry/", s.telemetry)
	mux.HandleFunc("/river/", s.telemetry)
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)

	s.other = httptest.NewServer(http.HandlerFunc(s.telemetry))
	t.Cleanup(s.other.Close)
	return s
}

// URL is the site root, e.g. http://127.0.0.1:41234.
func (s *Site) URL() string { return s.srv.URL }

// AddPage serves a reader page at /web/reader/<path>.
func (s *Site) AddPage(path string, c Chapter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages[path] = page{ch: c}
}

// ReaderURL is the URL of a page added with AddPage.
func (s *Site) ReaderURL(path string) string { return s.srv.URL + "/web/reader/" + path }

// ChapterURL is the reader URL of a chapter of a book added with AddBook.
func (s *Site) ChapterURL(bookID string, uid int64) string {
	return s.srv.URL + "/web/reader/" + bookID + "k" + weread.ChapterHash(uid)
}

// AddBook serves the book's detail page, its reader state page and its chapters.
func (s *Site) AddBook(b Book) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if b.Cover == "" {
		b.Cover = "https://cdn.weread.qq.com/wrepub/CB_" + b.ID + "/s_cover.jpg"
	}
	for i := range b.Chapters {
		ch := &b.Chapters[i]
		if ch.Level == 0 {
			ch.Level = 1
		}
		if ch.UID == 0 {
			ch.UID = int64(i + 1)
		}
		if ch.LastFooter == "" && i < len(b.Chapters)-1 {
			ch.LastFooter = "下一章"
		}
		if ch.Words == 0 {
			for _, screen := range ch.Screens {
				for _, l := range screen {
					ch.Words += len([]rune(l.Text))
				}
			}
		}
		s.pages[b.ID+"k"+weread.ChapterHash(ch.UID)] = page{ch: *ch, modeAware: true}
	}
	s.books[b.ID] = b
}

// AddBookList serves a book list page.
func (s *Site) AddBookList(id string, entries []ListEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lists[id] = entries
}

// SetAccount enables account semantics: sessions are validated, the profile,
// renewal and reader-mode endpoints work.
func (s *Site) SetAccount(a Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a.Name == "" {
		a.Name = "Tester"
	}
	if a.RefreshToken == "" {
		a.RefreshToken = "refresh-" + a.VID
	}
	s.account = &a
	s.skeys = map[string]bool{}
}

// LoginCookies returns the cookies of a freshly signed-in session.
func (s *Site) LoginCookies() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account == nil {
		panic("fakesite: LoginCookies without SetAccount")
	}
	return map[string]string{
		"wr_vid":  s.account.VID,
		"wr_skey": s.newSkeyLocked(),
		"wr_rt":   s.account.RefreshToken,
	}
}

func (s *Site) newSkeyLocked() string {
	s.skeyNumber++
	skey := "skey-" + strconv.Itoa(s.skeyNumber)
	s.skeys[skey] = true
	return skey
}

// ExpireSession invalidates every session key handed out so far; the refresh
// token keeps working.
func (s *Site) ExpireSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.skeys = map[string]bool{}
}

// Mode is the account's reader mode (0 vertical, 1 horizontal).
func (s *Site) Mode() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode
}

// SetMode changes the account's reader mode directly.
func (s *Site) SetMode(m int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
}

// ModeChanges lists the modes the exporter set through the API, in order.
func (s *Site) ModeChanges() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.modeSets...)
}

// Renewals counts the session renewals.
func (s *Site) Renewals() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.renewals
}

// ReaderHits counts the requests for reader pages.
func (s *Site) ReaderHits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readerHits
}

// Fetched lists the image requests (/img/ and /wrepub/) in order.
func (s *Site) Fetched() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.fetched...)
}

// Leaked lists the telemetry requests that reached a server; the browser layer
// must answer them itself, so the list has to stay empty.
func (s *Site) Leaked() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.telemetryHits...)
}

// CookieHeaders lists the Cookie headers of the document requests.
func (s *Site) CookieHeaders() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.documentCooks...)
}

func (s *Site) telemetry(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.telemetryHits = append(s.telemetryHits, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	http.Error(w, "telemetry must be answered by the browser layer", http.StatusTeapot)
}

func (s *Site) image(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.fetched = append(s.fetched, r.URL.Path)
	s.mu.Unlock()
	switch {
	case strings.HasSuffix(r.URL.Path, ".png"):
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(PNG())
	case strings.HasSuffix(r.URL.Path, ".jpg"), strings.HasSuffix(r.URL.Path, ".jpeg"):
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(JPEG())
	case strings.HasSuffix(r.URL.Path, ".missing"):
		http.NotFound(w, r)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// loggedIn reports whether the request carries a valid session. Without an
// account any wr_vid cookie counts.
func (s *Site) loggedIn(r *http.Request) bool {
	vid, err := r.Cookie("wr_vid")
	if err != nil || vid.Value == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.account == nil {
		return true
	}
	skey, err := r.Cookie("wr_skey")
	return vid.Value == s.account.VID && err == nil && s.skeys[skey.Value]
}

func (s *Site) login(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	vid, skey, rt := "4242", "fresh-skey", "refresh-token"
	if s.account != nil {
		vid, rt = s.account.VID, s.account.RefreshToken
		skey = s.newSkeyLocked()
	}
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "wr_vid", Value: vid, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "wr_skey", Value: skey, Path: "/"})
	http.SetCookie(w, &http.Cookie{Name: "wr_rt", Value: rt, Path: "/"})
	_, _ = w.Write([]byte("ok"))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

// user imitates GET /web/user?userVid=…
func (s *Site) user(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	acct := s.account
	s.mu.Unlock()
	if acct == nil {
		http.NotFound(w, r)
		return
	}
	vid := r.URL.Query().Get("userVid")
	switch {
	case vid != acct.VID:
		writeJSON(w, map[string]any{"errCode": -2010, "errMsg": "用户不存在"})
	case !s.loggedIn(r):
		writeJSON(w, map[string]any{"errCode": -2012, "errMsg": "登录超时"})
	default:
		n, _ := strconv.ParseInt(acct.VID, 10, 64)
		writeJSON(w, map[string]any{"userVid": n, "name": acct.Name, "avatar": "/avatar.png"})
	}
}

// renewal imitates POST /web/login/renewal: the refresh token buys a new session key.
func (s *Site) renewal(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	acct := s.account
	s.mu.Unlock()
	if acct == nil || r.Method != http.MethodPost {
		http.NotFound(w, r)
		return
	}
	rt, err := r.Cookie("wr_rt")
	if err != nil || rt.Value != acct.RefreshToken {
		writeJSON(w, map[string]any{"succ": 0, "errCode": -2013, "errMsg": "refresh token rejected"})
		return
	}
	s.mu.Lock()
	skey := s.newSkeyLocked()
	s.renewals++
	s.mu.Unlock()
	for name, value := range map[string]string{"wr_vid": acct.VID, "wr_skey": skey, "wr_pf": "0", "wr_rt": acct.RefreshToken} {
		http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/"})
	}
	writeJSON(w, map[string]any{"succ": 1})
}

// configModify imitates POST /web/user_config/modify (the reader mode switch).
func (s *Site) configModify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !s.loggedIn(r) {
		writeJSON(w, map[string]any{"errCode": -2012, "errMsg": "登录超时"})
		return
	}
	var body struct {
		UseHorizonReader *int `json:"useHorizonReader"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UseHorizonReader == nil {
		writeJSON(w, map[string]any{"errCode": -1, "errMsg": "bad request"})
		return
	}
	s.mu.Lock()
	s.mode = *body.UseHorizonReader
	s.modeSets = append(s.modeSets, s.mode)
	s.mu.Unlock()
	writeJSON(w, map[string]any{"useHorizonReader": *body.UseHorizonReader})
}

// navBar renders the login button or the avatar menu.
func navBar(in bool, avatarSrc string) string {
	if in {
		return fmt.Sprintf(`<div class="menu_container"><img class="wr_avatar_img" src="%s"></div>`, html.EscapeString(avatarSrc))
	}
	return `<button class="navBar_link_Login">登录</button>`
}

const loginScript = `
document.addEventListener('click', (e) => {
  const b = e.target.closest('.navBar_link_Login');
  if (!b) return;
  document.body.insertAdjacentHTML('beforeend', '<div class="login-qr">scan me</div>');
  if (!window.__scan) return;
  // The "user" scans the QR code a moment later.
  setTimeout(async () => {
    await fetch('/fake/login', {method: 'POST'});
    document.querySelector('.navBar_link_Login').outerHTML = '<div class="menu_container"><img class="wr_avatar_img" src="/avatar.png"></div>';
  }, 300);
});
`

type stateAnchor struct {
	Title string `json:"title"`
	Level int    `json:"level"`
}

type stateChapter struct {
	ChapterUID int64         `json:"chapterUid"`
	Title      string        `json:"title"`
	Level      int           `json:"level"`
	WordCount  int           `json:"wordCount"`
	Anchors    []stateAnchor `json:"anchors"`
}

// bookDetail serves /web/bookDetail/<id>. Unknown ids are served as a bare page
// (browser tests only need the nav bar); known books carry the server state.
func (s *Site) bookDetail(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/web/bookDetail/")
	s.mu.Lock()
	s.documentCooks = append(s.documentCooks, r.Header.Get("Cookie"))
	b, known := s.books[id]
	noScan := s.NoScan
	s.mu.Unlock()

	avatar := "/avatar.png"
	if r.URL.Query().Get("avatar") == "default" {
		avatar = "/img/Default.svg"
	}
	state := ""
	if known {
		soldout := 0
		if b.SoldOut {
			soldout = 1
		}
		infos := make([]stateChapter, len(b.Chapters))
		for i, ch := range b.Chapters {
			anchors := make([]stateAnchor, len(ch.Anchors))
			for j, a := range ch.Anchors {
				anchors[j] = stateAnchor(a)
			}
			infos[i] = stateChapter{ChapterUID: ch.UID, Title: ch.Title, Level: ch.Level, WordCount: ch.Words, Anchors: anchors}
		}
		raw, _ := json.Marshal(map[string]any{
			"reader": map[string]any{
				"bookInfo": map[string]any{
					"title": b.Title, "author": b.Author, "cover": b.Cover, "intro": b.Intro, "soldout": soldout,
				},
				"chapterInfos": infos,
			},
		})
		state = "window.__INITIAL_STATE__=" + string(raw) + ";"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head><body>
<nav>%s</nav><h1>The Book</h1>
<script>%s</script>
<script>window.__scan = %t;</script><script>%s</script></body></html>`,
		html.EscapeString(b.Title), navBar(s.loggedIn(r), avatar), state, !noScan, loginScript)
}

// bookList serves /misc/booklist/<id> with a Nuxt style payload.
func (s *Site) bookList(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/misc/booklist/")
	s.mu.Lock()
	entries, ok := s.lists[id]
	s.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteString(",")
		}
		title, _ := json.Marshal(e.Title)
		fmt.Fprintf(&b, `"%s":{bookId:"%s",title:%s}`, e.BookID, e.BookID, title)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"></head><body><div id="__nuxt"></div>
<script>window.__NUXT__={layout:"default",data:[{bookEntities:{%s}}],error:null};</script></body></html>`, b.String())
}

const readerScript = `
const PAGES = %s;
const LAST_FOOTER = %s;
let page = 0;
window.__telemetry = [];

function render() {
  const canvas = document.getElementById('c');
  const ctx = canvas.getContext('2d');
  for (const l of PAGES[page]) {
    ctx.font = l.font;
    ctx.fillStyle = l.color;
    ctx.fillText(l.text, l.x, l.y);
  }
  ctx.restore();
  const holder = document.getElementById('footer');
  holder.innerHTML = '';
  const label = page < PAGES.length - 1 ? '下一页' : LAST_FOOTER;
  if (label) {
    const b = document.createElement('button');
    b.className = 'readerFooter_button';
    b.innerText = label;
    b.onclick = () => { if (label === '下一页') { page++; render(); } };
    holder.appendChild(b);
  }
}

async function probe(name, url, init) {
  try {
    const r = await fetch(url, init);
    window.__telemetry.push({name, status: r.status, body: await r.text()});
  } catch (e) {
    window.__telemetry.push({name, error: String(e)});
  }
}

window.addEventListener('load', () => {
  setTimeout(render, 50);
  probe('logkv', '/hera/logkv?x=1');
  probe('chlog', '/i/hera/chlog', {method: 'POST', body: 'x'});
  probe('unknown', '/hera/unknown');
  probe('sentry', '/sentry/api/1/envelope/', {method: 'POST', body: 'x'});
  probe('river', '/river/single', {method: 'POST', body: 'x'});
  probe('cross', %s + '/hera/chlog', {method: 'POST', headers: {'Content-Type': 'application/json'}, body: '{}'});
});
`

// readerCSP stops the browser from loading remote images that the book text
// refers to: the tests must not touch the real network.
const readerCSP = "default-src 'self' 'unsafe-inline' data:; img-src 'self' data:; connect-src *"

func (s *Site) reader(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/web/reader/")

	s.mu.Lock()
	pg, ok := s.pages[path]
	book, isBook := s.books[path] // the bare reader URL of a book: only its state is needed
	mode := s.mode
	s.documentCooks = append(s.documentCooks, r.Header.Get("Cookie"))
	if ok {
		s.readerHits++
	}
	s.mu.Unlock()
	in := s.loggedIn(r)

	if !ok && isBook {
		// The server-rendered state tells the reader mode of the account; anonymous
		// visitors always get the default (horizontal).
		useHorizon := 1
		if in {
			useHorizon = mode
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head><body>
<script>window.__INITIAL_STATE__={"config":{"useHorizonReader":%d}};</script></body></html>`, html.EscapeString(book.Title), useHorizon)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	ch := pg.ch
	if ch.RedirectTo != "" {
		http.Redirect(w, r, ch.RedirectTo, http.StatusFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", readerCSP)

	horizontal := ch.Horizontal
	if pg.modeAware {
		horizontal = horizontal || !in || mode != 0
	}
	if horizontal {
		fmt.Fprint(w, `<!doctype html><html><head><meta charset="utf-8"><title>horizontal</title></head><body>
<div class="wr_horizontalReader"><button class="renderTarget_pager_button">下一页</button></div></body></html>`)
		return
	}

	screens := ch.Screens
	if len(screens) == 0 {
		screens = [][]Line{{Body("empty", 100)}}
	}
	footer := ch.LastFooter
	switch {
	case (ch.NeedsLogin || ch.Paywalled) && !in:
		footer = "登录"
	case ch.Paywalled:
		footer = PaywallFooter
	}
	screensJSON, _ := json.Marshal(screens)
	footerJSON, _ := json.Marshal(footer)
	otherJSON, _ := json.Marshal(s.other.URL)

	fmt.Fprintf(w, `<!doctype html><html><head><meta charset="utf-8"><title>%s</title></head><body>
<div class="readerTopBar_right">%s</div>
<div class="chapterTitle">%s</div>
<div class="passage-content">%s</div>
<canvas id="c" width="900" height="1400"></canvas>
<div id="footer"></div>
<script>`+readerScript+`</script></body></html>`,
		html.EscapeString(ch.Title), navBar(in, "/avatar.png"), html.EscapeString(ch.Title), ch.Extra,
		screensJSON, footerJSON, otherJSON)
}
