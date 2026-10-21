package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/fakesite"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// testBookID is the hashed id of the book the tests export.
var testBookID = weread.MustHash("1234567")

const (
	testVID  = "424242"
	testName = "Reader One"
)

// errOffline is what requests fail with while a test pretends the network is down.
var errOffline = errors.New("network is unreachable")

// syncBuffer is a bytes.Buffer that tolerates concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// siteTransport sends every request to the fake site, whatever host it names.
// WeRead's image hosts are https, the fake site is plain http.
type siteTransport struct {
	target *url.URL
	hook   func(*http.Request) error // sees every request; an error fails it
}

func (t siteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if t.hook != nil {
		if err := t.hook(r); err != nil {
			return nil, err
		}
	}
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = t.target.Scheme
	r2.URL.Host = t.target.Host
	r2.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(r2)
}

func noSleep(ctx context.Context, _ time.Duration) error { return ctx.Err() }

// stubBrowser stands in for Chrome: it hands out sessions that return scripted
// Markdown for reader URLs.
type stubBrowser struct {
	mu sync.Mutex

	pages     map[string]string // reader URL -> Markdown
	failures  map[string][]error
	launchErr []error // returned by the first launches

	launches    int
	opened      []*stubSession
	captured    []string
	navigations []string
	injected    []map[string]string

	// avatar is what WaitForAvatar reports.
	avatar bool
	// loginCookies is what Login returns (nil: ErrLoginTimeout).
	loginCookies map[string]string
	loginErr     error
	loginCalls   int
	// onCapture runs before each capture; it may cancel the run or tamper with the site.
	onCapture func(url string)
}

func newStubBrowser() *stubBrowser {
	return &stubBrowser{pages: map[string]string{}, failures: map[string][]error{}, avatar: true}
}

func (b *stubBrowser) launch(_ context.Context, opts browser.Options) (browserSession, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.launches++
	if len(b.launchErr) > 0 {
		err := b.launchErr[0]
		b.launchErr = b.launchErr[1:]
		return nil, err
	}
	s := &stubSession{b: b, opts: opts}
	b.opened = append(b.opened, s)
	return s, nil
}

func (b *stubBrowser) launchCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.launches
}

func (b *stubBrowser) capturedURLs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.captured...)
}

type stubSession struct {
	b      *stubBrowser
	opts   browser.Options
	closed bool
}

func (s *stubSession) InjectCookies(_ context.Context, cookies map[string]string) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	cp := map[string]string{}
	for k, v := range cookies {
		cp[k] = v
	}
	s.b.injected = append(s.b.injected, cp)
	return nil
}

func (s *stubSession) Navigate(_ context.Context, url string, _ time.Duration) error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.navigations = append(s.b.navigations, url)
	return nil
}

func (s *stubSession) WaitForAvatar(context.Context, time.Duration) (bool, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	return s.b.avatar, nil
}

func (s *stubSession) Login(context.Context) (map[string]string, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.loginCalls++
	if s.b.loginErr != nil {
		return nil, s.b.loginErr
	}
	if s.b.loginCookies == nil {
		return nil, browser.ErrLoginTimeout
	}
	return s.b.loginCookies, nil
}

func (s *stubSession) Cookies(context.Context) (map[string]string, error) {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	if s.b.loginCookies == nil {
		return map[string]string{}, nil
	}
	return s.b.loginCookies, nil
}

func (s *stubSession) CaptureChapter(ctx context.Context, url string, _ time.Duration) (string, error) {
	s.b.mu.Lock()
	hook := s.b.onCapture
	s.b.mu.Unlock()
	if hook != nil {
		hook(url)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.b.captured = append(s.b.captured, url)
	if errs := s.b.failures[url]; len(errs) > 0 {
		s.b.failures[url] = errs[1:]
		return "", errs[0]
	}
	md, ok := s.b.pages[url]
	if !ok {
		return "", &browser.ChapterUnavailableError{Footer: "no such page"}
	}
	return md, nil
}

func (s *stubSession) Close() error {
	s.b.mu.Lock()
	defer s.b.mu.Unlock()
	s.closed = true
	return nil
}

// harness is a fake site plus an exporter configured for it.
type harness struct {
	t    *testing.T
	site *fakesite.Site
	br   *stubBrowser
	cfg  Config
	log  *syncBuffer
	out  *syncBuffer

	sleeps []time.Duration
	// onRequest sees the HTTP requests of the exporter (not the browser's); an
	// error makes the request fail.
	onRequest func(*http.Request) error
}

// chapterMarkdown is the text the stub browser "captures" for chapter n (1-based).
func chapterMarkdown(n int) string {
	return fmt.Sprintf("## Chapter %d\n\nText of chapter %d, with a **bold** word.\n\n", n, n)
}

// testBook is a book of three chapters.
func testBook() fakesite.Book {
	return fakesite.Book{
		ID:     testBookID,
		Title:  "A Test Book",
		Author: "Some Author",
		Intro:  "An intro.",
		Chapters: []fakesite.Chapter{
			{UID: 41, Title: "Chapter 1", Words: 40},
			{UID: 42, Title: "Chapter 2", Words: 40},
			{UID: 43, Title: "Chapter 3", Words: 40},
		},
	}
}

// newHarness builds a site with testBook and an exporter configuration; the
// account is signed in unless anonymous is set.
func newHarness(t *testing.T) *harness {
	t.Helper()
	site := fakesite.New(t)
	site.AddBook(testBook())
	site.SetAccount(fakesite.Account{VID: testVID, Name: testName})
	site.SetMode(int(weread.ReaderModeHorizontal))

	dir := t.TempDir()
	h := &harness{
		t:    t,
		site: site,
		br:   newStubBrowser(),
		log:  &syncBuffer{},
		out:  &syncBuffer{},
		cfg: Config{
			BookRef:     testBookID,
			Formats:     []string{FormatEPUB},
			CacheDir:    filepath.Join(dir, "cache"),
			OutputDir:   filepath.Join(dir, "output"),
			BaseURL:     site.URL(),
			LoadTimeout: 5 * time.Second,
		},
	}
	for i := range testBook().Chapters {
		h.br.pages[h.readerURL(i)] = chapterMarkdown(i + 1)
	}
	h.saveLogin(site.LoginCookies())
	return h
}

func (h *harness) readerURL(pos int) string {
	h.t.Helper()
	return h.site.ChapterURL(testBookID, testBook().Chapters[pos].UID)
}

func (h *harness) cookieFile() string {
	cfg := h.cfg
	cfg.applyDefaults()
	return cfg.CookieFile
}

// saveLogin writes the login cookies the exporter loads.
func (h *harness) saveLogin(cookies map[string]string) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(h.cookieFile()), 0o755); err != nil {
		h.t.Fatal(err)
	}
	c := weread.NewCookies(h.cookieFile())
	c.Replace(cookies)
	if err := c.Save(); err != nil {
		h.t.Fatal(err)
	}
}

// exporter builds the exporter with the stub browser; tweak maps over the
// result before it is used.
func (h *harness) exporter(tweak ...func(*Exporter)) *Exporter {
	h.t.Helper()
	logger := slog.New(slog.NewTextHandler(h.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	target, err := url.Parse(h.site.URL())
	if err != nil {
		h.t.Fatal(err)
	}
	client := &http.Client{Transport: siteTransport{target: target, hook: h.onRequest}, Timeout: 30 * time.Second}
	e, err := New(h.cfg, logger, withTestClient(client), WithOutput(h.out), WithInput(strings.NewReader("")))
	if err != nil {
		h.t.Fatal(err)
	}
	e.launch = h.br.launch
	e.sleep = func(ctx context.Context, d time.Duration) error {
		h.sleeps = append(h.sleeps, d)
		return ctx.Err()
	}
	e.printPDF = func(context.Context, string, browser.PDFOptions) ([]byte, error) {
		return nil, errors.New("PDF printing is not stubbed in this test")
	}
	for _, fn := range tweak {
		fn(e)
	}
	return e
}

// withTestClient is WithHTTPClient with instant retries.
func withTestClient(c *http.Client) Option {
	return func(e *Exporter) error {
		api, err := weread.NewClient(weread.Options{
			BaseURL: e.cfg.BaseURL, Logger: e.log, HTTPClient: c, Sleep: noSleep,
		})
		if err != nil {
			return err
		}
		e.api = api
		return nil
	}
}

func (h *harness) store() *book.Store {
	return book.NewStore(h.cacheDir(), testBookID)
}

func (h *harness) cacheDir() string {
	cfg := h.cfg
	cfg.applyDefaults()
	return cfg.CacheDir
}

// prefill puts chapters (1-based numbers) into the cache as raw captures, with
// the book's meta.json.
func (h *harness) prefill(chapters ...int) {
	h.t.Helper()
	st := h.store()
	if err := st.EnsureDirs(); err != nil {
		h.t.Fatal(err)
	}
	meta := &book.Meta{Title: "A Test Book", Author: "Some Author", Intro: "An intro."}
	for _, ch := range testBook().Chapters {
		meta.Chapters = append(meta.Chapters, book.Chapter{ID: ch.UID, Title: ch.Title, Level: 1, Words: ch.Words, Anchors: []book.Anchor{}})
	}
	if err := st.SaveMeta(meta); err != nil {
		h.t.Fatal(err)
	}
	for _, n := range chapters {
		if err := st.SaveChapter(n-1, meta.Chapters[n-1].ID, chapterMarkdown(n)); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *harness) logs() string { return h.log.String() }

// mustRun runs the export and fails the test on error.
func (h *harness) mustRun(e *Exporter) []*Result {
	h.t.Helper()
	res, err := e.Run(context.Background())
	if err != nil {
		h.t.Fatalf("Run: %v\nlog:\n%s", err, h.logs())
	}
	return res
}

func fakesiteAccount(refreshToken string) fakesite.Account {
	return fakesite.Account{VID: testVID, Name: testName, RefreshToken: refreshToken}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func mustContain(t *testing.T, what, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("%s does not contain %q:\n%s", what, want, text)
	}
}

func mustNotContain(t *testing.T, what, text, unwanted string) {
	t.Helper()
	if strings.Contains(text, unwanted) {
		t.Errorf("%s contains %q:\n%s", what, unwanted, text)
	}
}

var _ io.Writer = (*syncBuffer)(nil)

// newBlockingReader returns a reader whose Read never returns (until the
// writer is closed), like a terminal nobody types into.
func newBlockingReader() (io.Reader, io.Closer) {
	pr, pw := io.Pipe()
	return pr, pw
}
