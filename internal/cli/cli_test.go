package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/pflag"

	"github.com/jtsang4/weread-exporter-go/internal/app"
	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/fakesite"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

var bookID = weread.MustHash("1234567")

// run executes the command line and returns the exit status and what was
// written to the standard output and error.
func run(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	return runWith(t, context.Background(), "", args...)
}

func runWith(t *testing.T, ctx context.Context, stdin string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Execute(ctx, args, Streams{In: strings.NewReader(stdin), Out: &out, Err: &errBuf})
	return code, out.String(), errBuf.String()
}

// fixture is a fake site, an account that is signed in, and a cache directory.
type fixture struct {
	t     *testing.T
	site  *fakesite.Site
	cache string
	out   string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	site := fakesite.New(t)
	site.AddBook(fakesite.Book{
		ID: bookID, Title: "A Test Book", Author: "Some Author", Intro: "Intro.",
		Chapters: []fakesite.Chapter{
			{UID: 41, Title: "Chapter 1", Words: 40},
			{UID: 42, Title: "Chapter 2", Words: 40, Level: 2},
			{UID: 43, Title: "Chapter 3", Words: 40},
		},
	})
	// The book whose page tells the reader mode when a command names no book.
	site.AddBook(fakesite.Book{ID: weread.MustHash(app.DefaultProbeBook), Title: "Probe", Chapters: []fakesite.Chapter{{UID: 1, Title: "x"}}})
	site.SetAccount(fakesite.Account{VID: "424242", Name: "Reader One"})
	site.SetMode(1)

	dir := t.TempDir()
	f := &fixture{t: t, site: site, cache: filepath.Join(dir, "cache"), out: filepath.Join(dir, "out")}
	if err := os.MkdirAll(f.cache, 0o755); err != nil {
		t.Fatal(err)
	}
	cookies := weread.NewCookies(filepath.Join(f.cache, "cookie.txt"))
	cookies.Replace(site.LoginCookies())
	if err := cookies.Save(); err != nil {
		t.Fatal(err)
	}
	return f
}

// args prepends the flags that point a command at the fixture.
func (f *fixture) args(rest ...string) []string {
	return append([]string{"--base-url", f.site.URL(), "--cache-dir", f.cache}, rest...)
}

// subcommand puts the flags after the subcommand name.
func (f *fixture) subcommand(name string, rest ...string) []string {
	return append([]string{name, "--base-url", f.site.URL(), "--cache-dir", f.cache}, rest...)
}

// cacheBook stores the whole book in the cache, so that no browser is needed.
func (f *fixture) cacheBook() {
	f.t.Helper()
	store := book.NewStore(f.cache, bookID)
	if err := store.EnsureDirs(); err != nil {
		f.t.Fatal(err)
	}
	meta := &book.Meta{Title: "A Test Book", Author: "Some Author", Intro: "Intro."}
	for i, title := range []string{"Chapter 1", "Chapter 2", "Chapter 3"} {
		meta.Chapters = append(meta.Chapters, book.Chapter{ID: int64(41 + i), Title: title, Level: 1, Words: 40, Anchors: []book.Anchor{}})
	}
	if err := store.SaveMeta(meta); err != nil {
		f.t.Fatal(err)
	}
	for i, ch := range meta.Chapters {
		if err := store.SaveChapter(i, ch.ID, fmt.Sprintf("## %s\n\nText of chapter %d.\n\n", ch.Title, i+1)); err != nil {
			f.t.Fatal(err)
		}
	}
}

func TestNoArgumentsShowTheHelp(t *testing.T) {
	code, out, errOut := run(t)
	if code != ExitOK || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"Usage:", "weread-exporter login", "--book-id", "--output-format", "reader-mode", "completion"} {
		if !strings.Contains(out, want) {
			t.Errorf("help lacks %q", want)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || !strings.HasPrefix(out, "weread-exporter ") || !strings.Contains(out, "platform:") {
		t.Fatalf("exit %d, %q", code, out)
	}
	code, out, _ = run(t, "--version")
	if code != ExitOK || !strings.HasPrefix(out, "weread-exporter ") || strings.Count(out, "\n") != 1 {
		t.Fatalf("exit %d, %q", code, out)
	}
}

func TestUsageErrorsExitWithStatus2(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown flag":        {"--bogus"},
		"unknown command":     {"explode"},
		"unexpected arg":      {"login", "now"},
		"missing book":        {"--headless"},
		"bad format":          {"-b", "1234567", "-o", "rtf"},
		"bad chapters":        {"-b", "1234567", "--chapters", "x"},
		"bad book":            {"-b", "not a book"},
		"bad duration":        {"-b", "1234567", "--load-interval", "soon"},
		"negative duration":   {"-b", "1234567", "--load-timeout", "-5"},
		"flag without value":  {"-b"},
		"bad log format":      {"-b", "1234567", "--log-format", "xml"},
		"info without book":   {"info"},
		"conflicting profile": {"-b", "1234567", "--use-default-profile", "--user-data-dir", "/tmp/x"},
		"list with chapters":  {"-b", "352003193_abc", "--chapters", "1"},
		"missing css file":    {"-b", "1234567", "--css-file", "/definitely/not/here.css"},
		"bad proxy":           {"-b", "1234567", "--proxy-server", "localhost"},
		"mode: bad sub":       {"reader-mode", "sideways"},
	} {
		code, out, errOut := run(t, args...)
		if code != ExitUsage {
			t.Errorf("%s: exit %d, want %d\nstdout: %s\nstderr: %s", name, code, ExitUsage, out, errOut)
			continue
		}
		if !strings.Contains(errOut, "weread-exporter:") || !strings.Contains(errOut, "--help") {
			t.Errorf("%s: the message should say what is wrong and where to look: %q", name, errOut)
		}
		if out != "" {
			t.Errorf("%s: stdout = %q", name, out)
		}
	}
}

func TestExitCodes(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{nil, ExitOK},
		{errors.New("boom"), ExitFailure},
		{&app.UsageError{Msg: "bad"}, ExitUsage},
		{fmt.Errorf("wrapped: %w", &app.UsageError{Msg: "bad"}), ExitUsage},
		{app.ErrLoginNeeded, ExitLoginNeeded},
		{fmt.Errorf("start: %w", app.ErrLoginNeeded), ExitLoginNeeded},
		{context.Canceled, ExitInterrupted},
		{fmt.Errorf("chapter 3: %w", context.Canceled), ExitInterrupted},
		{&app.BatchError{Total: 2, Failed: map[string]error{"x": errors.New("boom")}}, ExitFailure},
		{app.ErrBookUnavailable, ExitFailure},
	}
	for _, tt := range tests {
		if got := exitCode(tt.err); got != tt.want {
			t.Errorf("exitCode(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}

func TestDurationValue(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{"30", 30 * time.Second, false},
		{"0", 0, false},
		{" 45 ", 45 * time.Second, false},
		{"1.5", 1500 * time.Millisecond, false},
		{"90s", 90 * time.Second, false},
		{"2m", 2 * time.Minute, false},
		{"500ms", 500 * time.Millisecond, false},
		{"1h30m", 90 * time.Minute, false},
		{"", 0, true},
		{"abc", 0, true},
		{"-1", 0, true},
		{"-5s", 0, true},
		{"10x", 0, true},
	}
	for _, tt := range tests {
		var d time.Duration
		v := newDurationValue(&d, time.Hour)
		err := v.Set(tt.in)
		if tt.bad {
			if err == nil {
				t.Errorf("Set(%q) = %v, want an error", tt.in, d)
			}
			continue
		}
		if err != nil || d != tt.want {
			t.Errorf("Set(%q) = %v, %v; want %v", tt.in, d, err, tt.want)
		}
	}

	var d time.Duration
	v := newDurationValue(&d, 90*time.Second)
	if v.String() != "90s" || v.Type() != "duration" {
		t.Errorf("String = %q, Type = %q", v.String(), v.Type())
	}
	d = 1500 * time.Millisecond
	if v.String() != "1.5s" {
		t.Errorf("String = %q", v.String())
	}
	var nilValue *durationValue
	if nilValue.String() != "0s" {
		t.Errorf("a nil value prints %q", nilValue.String())
	}
}

func TestFlagsTranslateToTheConfiguration(t *testing.T) {
	var (
		c commonFlags
		b browserFlags
		x exportFlags
	)
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	c.register(fs)
	b.register(fs)
	x.register(fs)
	err := fs.Parse([]string{
		"-b", "1234567", "-o", "epub", "-o", "pdf,md", "--chapters", "1-3", "--output-dir", "o", "--cache-dir", "c",
		"--cookie-file", "c/ck", "--css-file", "x.css", "--load-interval", "5", "--load-timeout", "2m",
		"--headless", "--no-sandbox", "--chrome-path", "/chrome", "--user-data-dir", "/profile", "--mock-user-agent",
		"--force-login", "--debug-browser", "-y", "--overwrite", "--keep-reader-mode", "--mobi-converter", "/conv",
		"--proxy-server", "http://127.0.0.1:8888", "-v", "--base-url", "http://localhost:1",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg := buildConfig(&c, &b, &x)
	want := app.Config{
		BookRef: "1234567", Formats: []string{"epub", "pdf", "md"}, Chapters: "1-3",
		CacheDir: "c", OutputDir: "o", CookieFile: "c/ck", CSSFile: "x.css",
		ChromePath: "/chrome", Headless: true, NoSandbox: true, UserDataDir: "/profile", MockUserAgent: true,
		ProxyServer: "http://127.0.0.1:8888", ForceLogin: true,
		LoadTimeout: 2 * time.Minute, LoadInterval: 5 * time.Second,
		Overwrite: true, KeepReaderMode: true, MOBIConverter: "/conv", AssumeYes: true,
		BaseURL: "http://localhost:1", DebugBrowser: true, Verbose: true,
	}
	if fmt.Sprintf("%+v", cfg) != fmt.Sprintf("%+v", want) {
		t.Fatalf("config =\n%+v\nwant\n%+v", cfg, want)
	}

	// Defaults of the original tool.
	fs = pflag.NewFlagSet("test", pflag.ContinueOnError)
	c, b, x = commonFlags{}, browserFlags{}, exportFlags{}
	c.register(fs)
	b.register(fs)
	x.register(fs)
	_ = fs.Parse([]string{"-b", "1"})
	cfg = buildConfig(&c, &b, &x)
	if cfg.LoadTimeout != 60*time.Second || cfg.LoadInterval != 30*time.Second || cfg.CacheDir != "cache" || cfg.OutputDir != "output" || cfg.Headless {
		t.Fatalf("defaults = %+v", cfg)
	}
}

func TestEveryDocumentedFlagExists(t *testing.T) {
	root := newRootCmd(&env{ctx: context.Background()})
	root.InitDefaultHelpFlag()
	root.InitDefaultVersionFlag()
	for _, name := range []string{
		"book-id", "output-format", "load-timeout", "load-interval", "css-file", "headless", "force-login",
		"use-default-profile", "mock-user-agent", "proxy-server", // flags of the original tool
		"cache-dir", "output-dir", "cookie-file", "chrome-path", "no-sandbox", "user-data-dir", "yes", "overwrite",
		"keep-reader-mode", "mobi-converter", "chapters", "verbose", "log-format", "debug-browser", "version", "help",
	} {
		if root.Flags().Lookup(name) == nil {
			t.Errorf("--%s is missing", name)
		}
	}
	for _, short := range []string{"b", "o", "v", "y", "h"} {
		if root.Flags().ShorthandLookup(short) == nil {
			t.Errorf("-%s is missing", short)
		}
	}
	if f := root.Flags().Lookup("base-url"); f == nil || !f.Hidden {
		t.Error("--base-url must exist and stay out of the help")
	}
}

func TestPrettyLogger(t *testing.T) {
	var buf bytes.Buffer
	log, err := newLogger(&buf, false, "text")
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("hidden")
	log.Info("capturing chapter", "n", 3, "title", "第三章 起源", "file", `C:\books\a b.epub`, "empty", "", "err", errors.New("boom=1"))
	log.With("book", "x").WithGroup("g").Warn("careful", "k", 1.5, slog.Group("inner", "a", true))
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %q", lines)
	}
	if !strings.Contains(lines[0], ` INFO  capturing chapter n=3 title="第三章 起源" file="C:\books\a b.epub" empty="" err="boom=1"`) {
		t.Errorf("line 0 = %q", lines[0])
	}
	if !strings.Contains(lines[1], " WARN  careful book=x g.k=1.5 g.inner.a=true") {
		t.Errorf("line 1 = %q", lines[1])
	}
	if len(lines[0]) < 9 || lines[0][2] != ':' || lines[0][5] != ':' {
		t.Errorf("a line starts with the time: %q", lines[0])
	}

	buf.Reset()
	log, _ = newLogger(&buf, true, "text")
	log.Debug("shown now")
	if !strings.Contains(buf.String(), "DEBUG shown now") {
		t.Errorf("verbose logging: %q", buf.String())
	}

	buf.Reset()
	log, err = newLogger(&buf, false, "json")
	if err != nil {
		t.Fatal(err)
	}
	log.Info("hello", "n", 1)
	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil || rec["msg"] != "hello" || rec["n"] != float64(1) {
		t.Errorf("json log = %q, %v", buf.String(), err)
	}
	if _, err := newLogger(io.Discard, false, "yaml"); err == nil {
		t.Error("an unknown log format must be rejected")
	}
}

func TestInfoCommand(t *testing.T) {
	f := newFixture(t)
	code, out, errOut := run(t, f.subcommand("info", "-b", "1234567")...)
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"A Test Book", "Some Author", bookID, "on sale", "Chapters: 3, 0 of them cached", "Chapter 1", "  Chapter 2"} {
		if !strings.Contains(out, want) {
			t.Errorf("info lacks %q:\n%s", want, out)
		}
	}

	f.cacheBook()
	_, out, _ = run(t, f.subcommand("info", "-b", bookID)...)
	if !strings.Contains(out, "3 of them cached") {
		t.Errorf("cached chapters are not counted:\n%s", out)
	}

	code, out, errOut = run(t, f.subcommand("info", "-b", "1234567", "--json")...)
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	var got jsonBook
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got.ID != bookID || got.Title != "A Test Book" || len(got.Chapters) != 3 || !got.Chapters[1].Cached || got.Chapters[2].Number != 3 || got.Chapters[0].ID != 41 {
		t.Fatalf("json = %+v", got)
	}
}

func TestInfoCommandForABookList(t *testing.T) {
	f := newFixture(t)
	f.site.AddBookList("352003193_abc", []fakesite.ListEntry{{BookID: "1234567", Title: "A Test Book"}, {BookID: "7654321", Title: "Another"}})
	code, out, errOut := run(t, f.subcommand("info", "-b", "352003193_abc")...)
	if code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	for _, want := range []string{"Book list 352003193_abc: 2 books", bookID, "Another"} {
		if !strings.Contains(out, want) {
			t.Errorf("lacks %q:\n%s", want, out)
		}
	}
	_, out, _ = run(t, f.subcommand("info", "-b", "352003193_abc", "--json")...)
	var got jsonList
	if err := json.Unmarshal([]byte(out), &got); err != nil || got.ListID != "352003193_abc" || len(got.Books) != 2 || got.Books[1].Title != "Another" {
		t.Fatalf("json = %+v, %v", got, err)
	}
}

func TestInfoCommandOfASoldOutBook(t *testing.T) {
	f := newFixture(t)
	f.site.AddBook(fakesite.Book{ID: bookID, Title: "Gone", SoldOut: true, Chapters: []fakesite.Chapter{{UID: 1, Title: "x"}}})
	_, out, _ := run(t, f.subcommand("info", "-b", bookID)...)
	if !strings.Contains(out, "sold out") {
		t.Errorf("%s", out)
	}
}

func TestExportFromTheCacheWritesFilesAndPrintsTheirPaths(t *testing.T) {
	f := newFixture(t)
	f.cacheBook()
	code, out, errOut := run(t, f.args("-b", "1234567", "-o", "epub", "-o", "txt", "-o", "md", "--output-dir", f.out)...)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	want := []string{
		filepath.Join(f.out, "A Test Book.epub"),
		filepath.Join(f.out, "A Test Book.txt"),
		filepath.Join(f.out, "A Test Book.md"),
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("stdout = %q, want the written files %q", lines, want)
	}
	for _, p := range want {
		if info, err := os.Stat(p); err != nil || info.Size() == 0 {
			t.Errorf("%s: %v", p, err)
		}
	}
	if !strings.Contains(errOut, "every chapter is cached") || !strings.Contains(errOut, "done") {
		t.Errorf("progress log:\n%s", errOut)
	}

	// A second run keeps the files and says so.
	code, out, errOut = run(t, f.args("-b", "1234567", "-o", "epub", "--output-dir", f.out)...)
	if code != ExitOK || out != "" || !strings.Contains(errOut, "use --overwrite") {
		t.Fatalf("second run: exit %d, stdout %q, stderr:\n%s", code, out, errOut)
	}
}

func TestExportOfAPartialSelectionIsNamedAfterIt(t *testing.T) {
	f := newFixture(t)
	f.cacheBook()
	code, out, errOut := run(t, f.args("-b", bookID, "-o", "txt", "--chapters", "2-3", "--output-dir", f.out)...)
	if code != ExitOK {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
	if got := strings.TrimSpace(out); got != filepath.Join(f.out, "A Test Book (chapters 2-3).txt") {
		t.Fatalf("stdout = %q", got)
	}
}

func TestLoginCommandWithAValidLogin(t *testing.T) {
	f := newFixture(t)
	code, out, errOut := run(t, f.subcommand("login")...)
	if code != ExitOK || !strings.Contains(out, "Already signed in as Reader One") {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
}

func TestLoginCommandRefusesHeadlessWithoutALogin(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.cache, "cookie.txt")); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := run(t, f.subcommand("login", "--headless")...)
	if code != ExitUsage || !strings.Contains(errOut, "QR code") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestExportWithoutALoginExitsWithStatus3(t *testing.T) {
	f := newFixture(t)
	if err := os.Remove(filepath.Join(f.cache, "cookie.txt")); err != nil {
		t.Fatal(err)
	}
	// Chapters have to be captured, which needs a login; a headless browser cannot ask for one.
	code, _, errOut := run(t, f.args("-b", "1234567", "--headless", "--output-dir", f.out)...)
	if code != ExitLoginNeeded {
		t.Fatalf("exit %d, want %d\n%s", code, ExitLoginNeeded, errOut)
	}
	if !strings.Contains(errOut, "weread-exporter login") {
		t.Errorf("the message must say how to sign in:\n%s", errOut)
	}
}

func TestLogoutCommand(t *testing.T) {
	f := newFixture(t)
	code, out, _ := run(t, f.subcommand("logout")...)
	if code != ExitOK || !strings.Contains(out, "Deleted the saved login") {
		t.Fatalf("exit %d, %q", code, out)
	}
	if _, err := os.Stat(filepath.Join(f.cache, "cookie.txt")); !os.IsNotExist(err) {
		t.Fatalf("the cookie file is still there: %v", err)
	}
	code, out, _ = run(t, f.subcommand("logout")...)
	if code != ExitOK || !strings.Contains(out, "no saved login") {
		t.Fatalf("exit %d, %q", code, out)
	}
}

func TestReaderModeCommands(t *testing.T) {
	f := newFixture(t)

	code, out, errOut := run(t, f.subcommand("reader-mode")...)
	if code != ExitOK || !strings.Contains(out, "Reader mode: horizontal") {
		t.Fatalf("exit %d, %q, %s", code, out, errOut)
	}
	code, out, _ = run(t, f.subcommand("reader-mode", "status")...)
	if code != ExitOK || !strings.Contains(out, "Reader mode: horizontal") {
		t.Fatalf("exit %d, %q", code, out)
	}

	code, out, errOut = run(t, "reader-mode", "vertical", "--base-url", f.site.URL(), "--cache-dir", f.cache)
	if code != ExitOK || !strings.Contains(out, "vertical reader") || f.site.Mode() != 0 {
		t.Fatalf("exit %d, %q, mode %d\n%s", code, out, f.site.Mode(), errOut)
	}
	code, out, _ = run(t, "reader-mode", "horizontal", "--base-url", f.site.URL(), "--cache-dir", f.cache)
	if code != ExitOK || !strings.Contains(out, "horizontal reader") || f.site.Mode() != 1 {
		t.Fatalf("exit %d, %q, mode %d", code, out, f.site.Mode())
	}

	code, out, _ = run(t, "reader-mode", "restore", "--base-url", f.site.URL(), "--cache-dir", f.cache)
	if code != ExitOK || !strings.Contains(out, "Nothing to restore") {
		t.Fatalf("exit %d, %q", code, out)
	}

	// An export that was killed left a marker behind.
	f.site.SetMode(0)
	marker := `{"vid":"424242","restore":1,"since":"2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(filepath.Join(f.cache, "reader-mode-restore.json"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}
	_, out, _ = run(t, f.subcommand("reader-mode")...)
	if !strings.Contains(out, "Reader mode: vertical") || !strings.Contains(out, "reader-mode restore") {
		t.Fatalf("the pending restore must be mentioned: %q", out)
	}
	code, out, _ = run(t, "reader-mode", "restore", "--base-url", f.site.URL(), "--cache-dir", f.cache)
	if code != ExitOK || !strings.Contains(out, "was restored") || f.site.Mode() != 1 {
		t.Fatalf("exit %d, %q, mode %d", code, out, f.site.Mode())
	}
}

func TestReaderModeWithoutALoginExitsWithStatus3(t *testing.T) {
	f := newFixture(t)
	_ = os.Remove(filepath.Join(f.cache, "cookie.txt"))
	code, _, errOut := run(t, f.subcommand("reader-mode")...)
	if code != ExitLoginNeeded {
		t.Fatalf("exit %d\n%s", code, errOut)
	}
}

func TestInterruptedRunExitsWith130(t *testing.T) {
	f := newFixture(t)
	f.cacheBook()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, errOut := runWith(t, ctx, "", f.args("-b", "1234567", "--output-dir", f.out)...)
	if code != ExitInterrupted || !strings.Contains(errOut, "interrupted") {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
}

func TestShellCompletion(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		code, out, errOut := run(t, "completion", shell)
		if code != ExitOK || len(out) < 100 {
			t.Errorf("completion %s: exit %d, %d bytes, %s", shell, code, len(out), errOut)
		}
	}
	code, out, _ := run(t, "__complete", "-o", "")
	if code != ExitOK {
		t.Fatalf("exit %d", code)
	}
	for _, format := range app.Formats() {
		if !strings.Contains(out, format) {
			t.Errorf("the output formats are not offered for completion: %q", out)
			break
		}
	}
}

func TestSubcommandHelpMentionsItsFlags(t *testing.T) {
	for cmd, flags := range map[string][]string{
		"login":       {"--force-login", "--headless", "--chrome-path"},
		"info":        {"--book-id", "--json"},
		"logout":      {"--cache-dir"},
		"reader-mode": {"vertical", "horizontal", "restore"},
	} {
		code, out, errOut := run(t, cmd, "--help")
		if code != ExitOK {
			t.Errorf("%s --help: exit %d %s", cmd, code, errOut)
			continue
		}
		for _, flag := range flags {
			if !strings.Contains(out, flag) {
				t.Errorf("%s --help lacks %s", cmd, flag)
			}
		}
		if strings.Contains(out, "--base-url") {
			t.Errorf("%s --help shows the hidden test flag", cmd)
		}
	}
}
