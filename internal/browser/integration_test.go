package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/fakesite"
)

// The tests in this file drive a real headless Chrome against the fake site.
// They are skipped with -short and when no Chrome is installed.

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// testLogger buffers the logs and prints them only when the test fails (a
// background goroutine may log after the test has finished).
func testLogger(t *testing.T) (*slog.Logger, *lockedBuffer) {
	t.Helper()
	buf := &lockedBuffer{}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("session log:\n%s", buf.String())
		}
	})
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})), buf
}

// requireChrome skips the test with -short and when no Chrome is installed,
// unless WEREAD_TEST_REQUIRE_CHROME is set (CI does): then a missing browser is
// an error, so the browser tests cannot be skipped by accident.
func requireChrome(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("browser tests are skipped with -short")
	}
	if _, err := FindChrome(""); err != nil {
		if os.Getenv("WEREAD_TEST_REQUIRE_CHROME") != "" {
			t.Fatalf("Chrome is required (WEREAD_TEST_REQUIRE_CHROME is set): %v", err)
		}
		t.Skipf("no Chrome available: %v", err)
	}
}

// testOptions are options for a headless browser pointed at the fake site, with
// timeouts short enough for tests.
func testOptions(t *testing.T, site *fakesite.Site) (Options, *lockedBuffer) {
	t.Helper()
	logger, buf := testLogger(t)
	return Options{
		Headless:           true,
		NoSandbox:          true,
		BaseURL:            site.URL(),
		FooterTimeout:      3 * time.Second,
		CompleteTimeout:    5 * time.Second,
		PageInterval:       50 * time.Millisecond,
		LoginTimeout:       20 * time.Second,
		AvatarTimeout:      5 * time.Second,
		StepTimeout:        15 * time.Second,
		DiagnosticsDir:     filepath.Join(t.TempDir(), "diagnostics"),
		Logger:             logger,
		allowHeadlessLogin: true,
		ConfirmHeadless:    func(context.Context, []string) (bool, error) { return true, nil },
	}, buf
}

func launch(t *testing.T, opts Options) *Session {
	t.Helper()
	requireChrome(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	s, err := Launch(ctx, opts)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// launchFor starts a browser for the fake site.
func launchFor(t *testing.T, site *fakesite.Site, mutate ...func(*Options)) *Session {
	t.Helper()
	requireChrome(t)
	opts, _ := testOptions(t, site)
	for _, m := range mutate {
		m(&opts)
	}
	return launch(t, opts)
}

func capture(t *testing.T, s *Session, url string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	return s.CaptureChapter(ctx, url, 20*time.Second)
}

func mustCapture(t *testing.T, s *Session, url string) string {
	t.Helper()
	md, err := capture(t, s, url)
	if err != nil {
		t.Fatalf("CaptureChapter: %v", err)
	}
	return md
}

func TestCaptureSimpleChapter(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("simple", fakesite.Chapter{
		Title: "Chapter One",
		Screens: [][]fakesite.Line{{
			{Text: "Big Title", Font: fakesite.HeadingFont, Color: fakesite.BodyColor, X: 40, Y: 80},
			fakesite.Body("First paragraph line.", 140),
			fakesite.Body("Second paragraph.", 220),
		}},
		LastFooter: "下一章",
	})
	s := launchFor(t, site)
	got := mustCapture(t, s, site.ReaderURL("simple"))
	want := "## Chapter One\n\n\n\n## Big Title\n\nFirst paragraph line.\n\nSecond paragraph."
	if got != want {
		t.Errorf("markdown:\n got %q\nwant %q", got, want)
	}
}

func TestCaptureMultiPageChapter(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("paged", fakesite.Chapter{
		Title: "Long",
		Screens: [][]fakesite.Line{
			{fakesite.Body("Page one text.", 100), fakesite.Body("Page one more.", 160)},
			{fakesite.Body("Page two text.", 300), fakesite.Body("Page two more.", 360)},
			{fakesite.Body("Page three text.", 500)},
		},
		LastFooter: "下一章",
	})
	s := launchFor(t, site)
	got := mustCapture(t, s, site.ReaderURL("paged"))
	t.Logf("%q", got)
	for _, part := range []string{"Page one text.", "Page one more.", "Page two text.", "Page two more.", "Page three text."} {
		if strings.Count(got, part) != 1 {
			t.Errorf("%q should appear exactly once in %q", part, got)
		}
	}
	// The pages are separated like paragraphs and keep their order.
	for _, pair := range [][2]string{{"Page one more.", "Page two text."}, {"Page two more.", "Page three text."}} {
		if !strings.Contains(got, pair[0]+"\n\n") || strings.Index(got, pair[0]) > strings.Index(got, pair[1]) {
			t.Errorf("%q must come before %q as its own paragraph: %q", pair[0], pair[1], got)
		}
	}
}

func TestCaptureLastChapterHasNoFooter(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("last", fakesite.Chapter{
		Title: "Epilogue",
		Screens: [][]fakesite.Line{
			{fakesite.Body("The end begins.", 100)},
			{fakesite.Body("The end ends.", 300)},
		},
		LastFooter: "", // the last page of the last chapter has no button
	})
	s := launchFor(t, site, func(o *Options) { o.FooterTimeout = time.Second })
	got := mustCapture(t, s, site.ReaderURL("last"))
	if !strings.Contains(got, "The end begins.") || !strings.Contains(got, "The end ends.") {
		t.Errorf("both pages should be captured: %q", got)
	}
}

func TestCaptureStyles(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("styles", fakesite.Chapter{
		Title: "Styles",
		Screens: [][]fakesite.Line{{
			fakesite.At("Part Title", 30, 40, 80),
			fakesite.At("Sub Title", 24, 40, 140),
			// A paragraph with an inline highlight and a footnote marker.
			fakesite.At("Use the ", 20, 40, 200),
			{Text: "highlighted", Font: fakesite.BodyFont, Color: fakesite.HighlightColor, X: 160, Y: 200},
			fakesite.At(" word", 20, 300, 200),
			fakesite.At("1", 16, 360, 200),
			fakesite.At(" and go on.", 20, 380, 200),
			fakesite.At("After the code block.", 20, 40, 400),
			fakesite.At("After the picture.", 20, 40, 540),
			fakesite.At("After the rule.", 20, 40, 680),
		}},
		LastFooter: "下一章",
		Extra: `<pre style="transform: translate(40px, 330px)">fmt.Println("hi")</pre>` +
			`<img style="transform: translate(40px, 470px)" src="/img/pic.png">` +
			`<hr style="transform: translate(40px, 610px)">`,
	})
	s := launchFor(t, site)
	got := mustCapture(t, s, site.ReaderURL("styles"))
	want := "## Styles\n\n\n\n## Part Title\n\n### Sub Title\n\nUse the `highlighted` word<sup>1</sup> and go on." +
		"\n\n```\nfmt.Println(\"hi\")\n```\n\nAfter the code block." +
		"\n\n![](/img/pic.png)\n\n\nAfter the picture." +
		"\n\n------\n\n\nAfter the rule."
	if got != want {
		t.Errorf("markdown:\n got %q\nwant %q", got, want)
	}
}

func TestCaptureLoginRequired(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("private", fakesite.Chapter{
		Title: "Members only", Screens: [][]fakesite.Line{{fakesite.Body("secret", 100)}}, LastFooter: "下一章", NeedsLogin: true,
	})
	opts, _ := testOptions(t, site)
	s := launch(t, opts)
	_, err := capture(t, s, site.ReaderURL("private"))
	if !errors.Is(err, ErrLoginRequired) {
		t.Fatalf("err = %v, want ErrLoginRequired", err)
	}
	if Retryable(err) {
		t.Error("a login problem is not retryable")
	}
	if _, statErr := os.Stat(opts.DiagnosticsDir); statErr == nil {
		t.Error("no page dump is expected for a plain login prompt")
	}

	// With the login cookies the very same chapter is readable.
	if err := s.InjectCookies(context.Background(), map[string]string{"wr_vid": "4242", "wr_skey": "k"}); err != nil {
		t.Fatal(err)
	}
	got := mustCapture(t, s, site.ReaderURL("private"))
	if !strings.Contains(got, "secret") {
		t.Errorf("markdown = %q", got)
	}
}

func TestCaptureUnavailableChapter(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	const prompt = "会员卡 19元/月 · 开通后即可阅读"
	site.AddPage("paid", fakesite.Chapter{Title: "Paid", Screens: [][]fakesite.Line{{fakesite.Body("teaser", 100)}}, LastFooter: prompt})
	opts, _ := testOptions(t, site)
	s := launch(t, opts)
	_, err := capture(t, s, site.ReaderURL("paid"))
	var unavailable *ChapterUnavailableError
	if !errors.As(err, &unavailable) || unavailable.Footer != prompt {
		t.Fatalf("err = %v, want ChapterUnavailableError for %q", err, prompt)
	}
	if Retryable(err) {
		t.Error("an unavailable chapter is not retryable")
	}
	// The failed page was saved for troubleshooting.
	entries, readErr := os.ReadDir(opts.DiagnosticsDir)
	if readErr != nil {
		t.Fatalf("diagnostics directory: %v", readErr)
	}
	var html, png bool
	for _, e := range entries {
		html = html || strings.HasSuffix(e.Name(), ".html")
		png = png || strings.HasSuffix(e.Name(), ".png")
	}
	if !html || !png {
		t.Errorf("want an .html and a .png dump, got %v", entries)
	}
}

func TestCaptureHorizontalReader(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("horizontal", fakesite.Chapter{Title: "H", Horizontal: true})
	s := launchFor(t, site)
	_, err := capture(t, s, site.ReaderURL("horizontal"))
	if !errors.Is(err, ErrHorizontalReader) {
		t.Fatalf("err = %v, want ErrHorizontalReader", err)
	}
	if Retryable(err) {
		t.Error("the reader mode is not fixed by retrying")
	}
}

func TestCaptureRedirect(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("moved", fakesite.Chapter{RedirectTo: "/web/bookDetail/elsewhere"})
	s := launchFor(t, site)
	_, err := capture(t, s, site.ReaderURL("moved"))
	var unexpected *UnexpectedPageError
	if !errors.As(err, &unexpected) {
		t.Fatalf("err = %v, want UnexpectedPageError", err)
	}
	if !strings.Contains(unexpected.Reason, "/web/bookDetail/elsewhere") {
		t.Errorf("reason = %q", unexpected.Reason)
	}
	if !Retryable(err) {
		t.Error("an unexpected page may be transient")
	}
}

func TestCaptureEmptyReader(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	// The page exists, but nothing is ever drawn.
	site.AddPage("blank", fakesite.Chapter{Title: "Blank", Screens: [][]fakesite.Line{{}}, LastFooter: "下一章"})
	s := launchFor(t, site, func(o *Options) { o.CompleteTimeout = time.Second })
	_, err := capture(t, s, site.ReaderURL("blank"))
	if !errors.Is(err, ErrEmptyCapture) {
		t.Fatalf("err = %v, want ErrEmptyCapture", err)
	}
	if !Retryable(err) {
		t.Error("an empty capture is retryable")
	}
}

func TestCaptureHonoursCancellation(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	// A page that never shows a footer keeps the capture waiting.
	site.AddPage("slow", fakesite.Chapter{Title: "Slow", Screens: [][]fakesite.Line{{fakesite.Body("x", 100)}}, LastFooter: ""})
	site.AddPage("fine", fakesite.Chapter{Title: "Fine", Screens: [][]fakesite.Line{{fakesite.Body("still works", 100)}}, LastFooter: "下一章"})
	s := launchFor(t, site, func(o *Options) { o.FooterTimeout = time.Minute })

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := s.CaptureChapter(ctx, site.ReaderURL("slow"), 20*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Errorf("cancellation took %s", took)
	}
	// The session survives a cancelled operation.
	if got := mustCapture(t, s, site.ReaderURL("fine")); !strings.Contains(got, "still works") {
		t.Errorf("the session should still work after a cancelled capture, got %q", got)
	}
}

func TestNavigateTimeout(t *testing.T) {
	t.Parallel()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	site := fakesite.New(t)
	s := launchFor(t, site)
	start := time.Now()
	err := s.Navigate(context.Background(), slow.URL, time.Second)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want a deadline error", err)
	}
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("navigation took %s", took)
	}
}

func TestTelemetryIsAnsweredLocally(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("tele", fakesite.Chapter{Title: "T", Screens: [][]fakesite.Line{{fakesite.Body("text", 100)}}, LastFooter: "下一章"})
	s := launchFor(t, site)
	mustCapture(t, s, site.ReaderURL("tele"))

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// Wait for all six probes of the page.
	ok, err := s.waitFor(ctx, `window.__telemetry.length >= 6`, 10*time.Second, 100*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("telemetry probes did not finish: ok=%v err=%v", ok, err)
	}
	raw, err := s.evalString(ctx, `JSON.stringify(window.__telemetry)`)
	if err != nil {
		t.Fatal(err)
	}
	var probes []struct {
		Name   string `json:"name"`
		Status int    `json:"status"`
		Body   string `json:"body"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(raw), &probes); err != nil {
		t.Fatal(err)
	}
	byName := map[string]int{}
	errored := map[string]bool{}
	bodies := map[string]string{}
	for _, p := range probes {
		byName[p.Name] = p.Status
		errored[p.Name] = p.Error != ""
		bodies[p.Name] = p.Body
	}
	for name, want := range map[string]int{"logkv": 204, "chlog": 200, "sentry": 200, "river": 200, "cross": 200} {
		if byName[name] != want || errored[name] {
			t.Errorf("probe %s: status %d (error %v), want %d", name, byName[name], errored[name], want)
		}
	}
	if !errored["unknown"] {
		t.Errorf("an unknown /hera/ endpoint must be blocked, got status %d", byName["unknown"])
	}
	if !strings.Contains(bodies["river"], `"err_code":0`) {
		t.Errorf("river body = %q", bodies["river"])
	}
	if leaked := site.Leaked(); len(leaked) != 0 {
		t.Errorf("telemetry reached the server: %v", leaked)
	}
}

func TestInjectCookies(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	s := launchFor(t, site)
	ctx := context.Background()

	// Anonymous first.
	if err := s.Navigate(ctx, site.URL()+"/web/bookDetail/b1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if st, err := s.AvatarState(ctx); err != nil || st != AvatarNone {
		t.Fatalf("anonymous avatar state = %q, %v", st, err)
	}

	err := s.InjectCookies(ctx, map[string]string{
		"wr_vid":              "4242",
		"wr_skey":             "abc",
		"wr_theme":            "light", // replaced by dark
		"wr_useHorizonReader": "1",     // display preference: not copied
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Cookies(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got["wr_vid"] != "4242" || got["wr_skey"] != "abc" {
		t.Errorf("login cookies missing: %v", got)
	}
	if got[ThemeCookie] != ThemeDark {
		t.Errorf("theme cookie = %q, want %q", got[ThemeCookie], ThemeDark)
	}
	if _, ok := got["wr_useHorizonReader"]; ok {
		t.Errorf("the reader-mode cookie must not be copied: %v", got)
	}

	if err := s.Navigate(ctx, site.URL()+"/web/bookDetail/b1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.WaitForAvatar(ctx, 5*time.Second); err != nil || !ok {
		t.Fatalf("avatar did not appear after injecting cookies: %v, %v", ok, err)
	}
	if st, _ := s.AvatarState(ctx); st != AvatarUser {
		t.Errorf("avatar state = %q", st)
	}
	var sawLogin bool
	for _, h := range site.CookieHeaders() {
		sawLogin = sawLogin || (strings.Contains(h, "wr_vid=4242") && strings.Contains(h, "wr_theme=dark"))
	}
	if !sawLogin {
		t.Errorf("the server never saw the injected cookies: %q", site.CookieHeaders())
	}

	if err := s.ClearCookies(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Cookies(ctx); len(got) != 0 {
		t.Errorf("cookies after ClearCookies: %v", got)
	}
}

func TestAvatarDefaultState(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	s := launchFor(t, site)
	ctx := context.Background()
	if err := s.InjectCookies(ctx, map[string]string{"wr_vid": "1", "wr_skey": "k"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Navigate(ctx, site.URL()+"/web/bookDetail/b1?avatar=default", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if st, err := s.AvatarState(ctx); err != nil || st != AvatarDefault {
		t.Fatalf("avatar state = %q, %v; want %q", st, err, AvatarDefault)
	}
	if ok, err := s.WaitForAvatar(ctx, time.Second); err != nil || ok {
		t.Errorf("the placeholder is not a signed-in avatar: ok=%v err=%v", ok, err)
	}
}

func TestLogin(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	s := launchFor(t, site)
	ctx := context.Background()
	if err := s.Navigate(ctx, site.URL()+"/web/bookDetail/b1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	cookies, err := s.Login(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cookies["wr_vid"] != "4242" || cookies["wr_skey"] != "fresh-skey" || cookies["wr_rt"] != "refresh-token" {
		t.Errorf("cookies after login: %v", cookies)
	}
	if st, _ := s.AvatarState(ctx); st != AvatarUser {
		t.Errorf("avatar state after login = %q", st)
	}
	// Signed in already: there is no login button any more.
	if _, err := s.Login(ctx); !errors.Is(err, ErrNoLoginButton) {
		t.Errorf("second Login: %v, want ErrNoLoginButton", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.NoScan = true
	s := launchFor(t, site, func(o *Options) { o.LoginTimeout = 2500 * time.Millisecond })
	ctx := context.Background()
	if err := s.Navigate(ctx, site.URL()+"/web/bookDetail/b1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(ctx); !errors.Is(err, ErrLoginTimeout) {
		t.Fatalf("err = %v, want ErrLoginTimeout", err)
	}
}

func TestLoginNeedsAWindowInHeadlessMode(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	s := launchFor(t, site, func(o *Options) { o.allowHeadlessLogin = false })
	if err := s.Navigate(context.Background(), site.URL()+"/web/bookDetail/b1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Login(context.Background()); !errors.Is(err, ErrLoginNeedsWindow) {
		t.Fatalf("err = %v, want ErrLoginNeedsWindow", err)
	}
}

func TestHeadlessUserAgent(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)

	s := launchFor(t, site)
	ua, err := s.UserAgent(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ua, "Headless") || !strings.Contains(ua, "Chrome/") {
		t.Errorf("default headless user agent = %q", ua)
	}
	if s.ChromeVersion() == 0 {
		t.Error("the Chrome version should be known")
	}

	const custom = "Mozilla/5.0 (X11; Linux x86_64) CustomAgent/1.0"
	s2 := launchFor(t, site, func(o *Options) { o.UserAgent = custom })
	if ua, _ := s2.UserAgent(context.Background()); ua != custom {
		t.Errorf("custom user agent = %q", ua)
	}

	s3 := launchFor(t, site, func(o *Options) { o.MockUserAgent = true })
	ua, _ = s3.UserAgent(context.Background())
	if !regexp.MustCompile(`^Mozilla/5\.0 \(Windows NT 10\.0; Win64; x64\) .* Chrome/\d+\.0\.0\.0 Safari/537\.36$`).MatchString(ua) {
		t.Errorf("mock user agent = %q (must not carry quotes or the real version)", ua)
	}
}

func TestHeadlessCheck(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)

	// A normal headless session passes the check after the stealth patches.
	opts, _ := testOptions(t, site)
	var reasons []string
	opts.ConfirmHeadless = func(_ context.Context, r []string) (bool, error) {
		reasons = r
		return true, nil
	}
	launch(t, opts)
	if len(reasons) != 0 {
		t.Errorf("the stealth patches should hide the headless signs, still reported: %v", reasons)
	}

	// A browser that announces itself as headless is reported and can be refused.
	requireChrome(t)
	opts2, _ := testOptions(t, site)
	opts2.UserAgent = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 HeadlessChrome/120.0.0.0 Safari/537.36"
	var reasons2 []string
	opts2.ConfirmHeadless = func(_ context.Context, r []string) (bool, error) {
		reasons2 = r
		return false, nil
	}
	_, err := Launch(context.Background(), opts2)
	if !errors.Is(err, ErrHeadlessRefused) {
		t.Fatalf("Launch with a declined headless confirmation: %v, want ErrHeadlessRefused", err)
	}
	if len(reasons2) == 0 || !strings.Contains(strings.Join(reasons2, ";"), "HeadlessChrome") {
		t.Errorf("reasons = %v", reasons2)
	}

	// Without a confirmation callback the answer is "stop".
	opts3, _ := testOptions(t, site)
	opts3.UserAgent = opts2.UserAgent
	opts3.ConfirmHeadless = nil
	if _, err := Launch(context.Background(), opts3); !errors.Is(err, ErrHeadlessRefused) {
		t.Fatalf("Launch without a confirmation callback: %v, want ErrHeadlessRefused", err)
	}

	// A confirmation error is passed on.
	opts4, _ := testOptions(t, site)
	opts4.UserAgent = opts2.UserAgent
	boom := errors.New("prompt failed")
	opts4.ConfirmHeadless = func(context.Context, []string) (bool, error) { return false, boom }
	if _, err := Launch(context.Background(), opts4); !errors.Is(err, boom) {
		t.Fatalf("Launch with a failing confirmation: %v, want %v", err, boom)
	}
}

func TestDebugConsoleForwarding(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	site.AddPage("debug", fakesite.Chapter{Title: "D", Screens: [][]fakesite.Line{{fakesite.Body("hello", 100)}}, LastFooter: "下一章"})
	opts, logs := testOptions(t, site)
	opts.DebugConsole = true
	s := launch(t, opts)
	mustCapture(t, s, site.ReaderURL("debug"))
	// Console events arrive asynchronously.
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(logs.String(), "page console") && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(logs.String(), "page console") {
		t.Errorf("the page console was not forwarded:\n%s", logs.String())
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	requireChrome(t)
	opts, _ := testOptions(t, site)
	s, err := Launch(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("first Close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if _, err := s.Cookies(context.Background()); err == nil {
		t.Error("a closed session must not work")
	}
}

func TestLaunchCancelledContext(t *testing.T) {
	t.Parallel()
	requireChrome(t)
	site := fakesite.New(t)
	opts, _ := testOptions(t, site)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Launch(ctx, opts); err == nil {
		t.Fatal("Launch with a cancelled context should fail")
	}
}

func TestLaunchWithMissingChrome(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	opts, _ := testOptions(t, site)
	opts.ChromePath = filepath.Join(t.TempDir(), "no-such-chrome")
	_, err := Launch(context.Background(), opts)
	var nf *ChromeNotFoundError
	if !errors.As(err, &nf) {
		t.Fatalf("err = %v, want ChromeNotFoundError", err)
	}
}

func TestLaunchPersistentProfile(t *testing.T) {
	t.Parallel()
	site := fakesite.New(t)
	dir := filepath.Join(t.TempDir(), "profile")
	ctx := context.Background()

	opts, _ := testOptions(t, site)
	opts.UserDataDir = dir
	s := launch(t, opts)
	if err := s.Navigate(ctx, site.URL()+"/web/bookDetail/b1", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Default")); err != nil {
		t.Errorf("the profile directory was not used: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// The directory outlives the browser (it holds the saved login).
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("profile directory removed: %v", err)
	}
}

func TestPrintHTMLToPDF(t *testing.T) {
	t.Parallel()
	requireChrome(t)
	dir := t.TempDir()
	page := filepath.Join(dir, "book.html")
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><title>测试书</title><style>@page { size: A4; margin: 14mm 12mm; } h1 { page-break-before: always; }</style></head><body>`)
	for i := 1; i <= 3; i++ {
		b.WriteString(`<h1>第` + strconv.Itoa(i) + `章</h1><p>这是一个用于测试 PDF 导出的段落。</p>`)
	}
	b.WriteString(`<script>document.body.innerHTML = "script ran"</script></body></html>`)
	if err := os.WriteFile(page, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	logger, _ := testLogger(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	pdf, err := PrintHTMLToPDF(ctx, page, PDFOptions{NoSandbox: true, Logger: logger, Timeout: 50 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) < 1000 {
		t.Fatalf("not a PDF: %d bytes, starts %q", len(pdf), pdf[:min(8, len(pdf))])
	}
	pages := len(regexp.MustCompile(`/Type\s*/Page[^s]`).FindAll(pdf, -1))
	if pages < 3 {
		t.Errorf("PDF has %d pages, want at least 3 (one per chapter)", pages)
	}

	if _, err := PrintHTMLToPDF(ctx, filepath.Join(dir, "missing.html"), PDFOptions{NoSandbox: true}); err == nil {
		t.Error("a missing HTML file must be an error")
	}
	if _, err := PrintHTMLToPDF(ctx, page, PDFOptions{ChromePath: filepath.Join(dir, "no-chrome")}); err == nil {
		t.Error("a missing Chrome must be an error")
	}
}

func TestFileURL(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "a b.html")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := fileURL(f)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "file:///") || !strings.HasSuffix(got, "a%20b.html") {
		t.Errorf("fileURL = %q", got)
	}
	if _, err := fileURL(filepath.Join(dir, "missing.html")); err == nil {
		t.Error("a missing file must be an error")
	}
}
