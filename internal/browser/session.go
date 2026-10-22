package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	cdpbrowser "github.com/chromedp/cdproto/browser"
	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/jtsang4/weread-exporter-go/internal/assets"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// Options configures Launch.
type Options struct {
	// ChromePath is the browser executable; "" searches for one (see FindChrome).
	ChromePath string
	// Headless runs Chrome without a window.
	Headless bool
	// NoSandbox disables Chrome's sandbox (some containers need it; root on
	// Linux gets it automatically).
	NoSandbox bool
	// UserDataDir is a persistent Chrome profile directory. Empty means a fresh
	// temporary profile that is deleted when the browser exits.
	UserDataDir string
	// UseDefaultProfile uses Chrome's default profile directory instead. Chrome
	// 136 and later refuse remote debugging on it.
	UseDefaultProfile bool
	// MockUserAgent sends a random desktop Chrome user agent.
	MockUserAgent bool
	// UserAgent overrides the user agent (takes precedence over MockUserAgent).
	UserAgent string
	// ProxyServer is passed to Chrome as --proxy-server, e.g. http://127.0.0.1:8888.
	ProxyServer string
	// BaseURL is the site root; empty means https://weread.qq.com.
	BaseURL string
	// WindowWidth and WindowHeight set the window size (default 1920x1080).
	WindowWidth, WindowHeight int

	// LoginTimeout bounds the wait for the QR-code login (default 5 minutes).
	LoginTimeout time.Duration
	// AvatarTimeout bounds the wait for the avatar after cookies are injected (default 30s).
	AvatarTimeout time.Duration
	// FooterTimeout bounds the wait for the reader's footer button (default 60s).
	FooterTimeout time.Duration
	// CompleteTimeout bounds the wait for the hook to finish a chapter (default 10s).
	CompleteTimeout time.Duration
	// PageInterval is the pause after turning to the next page of a chapter (default 1s).
	PageInterval time.Duration
	// StepTimeout bounds every individual DevTools call (default 30s).
	StepTimeout time.Duration

	// DebugConsole forwards the page's console output to the logger and makes
	// the capture hook verbose.
	DebugConsole bool
	// DiagnosticsDir receives a page dump (HTML and screenshot) when a capture
	// fails; empty disables the dumps.
	DiagnosticsDir string
	// ConfirmHeadless is asked whether to continue when the browser looks
	// headless or automated. Nil means "stop".
	ConfirmHeadless func(ctx context.Context, reasons []string) (bool, error)
	// ChromeOutput receives Chrome's own stdout/stderr (nil discards it).
	ChromeOutput io.Writer
	// Logger receives progress messages; nil discards them.
	Logger *slog.Logger

	// allowHeadlessLogin lets Login run without a window (tests simulate the scan).
	allowHeadlessLogin bool
}

func (o Options) withDefaults() Options {
	if o.BaseURL == "" {
		o.BaseURL = weread.RootURL
	}
	o.BaseURL = strings.TrimRight(o.BaseURL, "/")
	if o.WindowWidth <= 0 {
		o.WindowWidth = 1920
	}
	if o.WindowHeight <= 0 {
		o.WindowHeight = 1080
	}
	if o.LoginTimeout <= 0 {
		o.LoginTimeout = 5 * time.Minute
	}
	if o.AvatarTimeout <= 0 {
		o.AvatarTimeout = 30 * time.Second
	}
	if o.FooterTimeout <= 0 {
		o.FooterTimeout = 60 * time.Second
	}
	if o.CompleteTimeout <= 0 {
		o.CompleteTimeout = 10 * time.Second
	}
	if o.PageInterval <= 0 {
		o.PageInterval = time.Second
	}
	if o.StepTimeout <= 0 {
		o.StepTimeout = 30 * time.Second
	}
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	return o
}

// Session is a running Chrome with one tab, prepared for capturing the WeRead reader.
type Session struct {
	opts    Options
	log     *slog.Logger
	baseURL string

	chromePath    string
	chromeVersion int // major version, 0 when unknown

	tabCtx      context.Context
	cancelTab   context.CancelFunc
	cancelAlloc context.CancelFunc
	started     bool // the browser process is up (chromedp.Do(tabCtx) succeeded)
	closeOnce   sync.Once
	closeErr    error
}

// chromeFlag is one command-line switch for Chrome (value: string or bool).
type chromeFlag struct {
	Name  string
	Value any
}

// chromeFlags returns the Chrome switches for the options. profileDir is the
// profile directory to use ("" lets chromedp create a temporary one).
func chromeFlags(o Options, goos, profileDir string) []chromeFlag {
	flags := []chromeFlag{
		{"no-first-run", true},
		{"no-default-browser-check", true},
		// The capture must keep running when the window is hidden or in the background.
		{"disable-background-timer-throttling", true},
		{"disable-renderer-backgrounding", true},
		{"disable-backgrounding-occluded-windows", true},
	}
	if goos == "linux" {
		// /dev/shm is tiny in containers and makes Chrome crash.
		flags = append(flags, chromeFlag{"disable-dev-shm-usage", true})
	}
	if o.Headless {
		flags = append(flags, chromeFlag{"headless", "new"})
	}
	if o.NoSandbox {
		flags = append(flags, chromeFlag{"no-sandbox", true})
	}
	if !o.UseDefaultProfile {
		flags = append(flags, chromeFlag{"window-size", fmt.Sprintf("%d,%d", o.WindowWidth, o.WindowHeight)})
	}
	if profileDir != "" {
		flags = append(flags, chromeFlag{"user-data-dir", profileDir})
	}
	if o.ProxyServer != "" {
		flags = append(flags, chromeFlag{"proxy-server", o.ProxyServer})
	}
	switch {
	case o.UserAgent != "":
		flags = append(flags, chromeFlag{"user-agent", o.UserAgent})
	case o.MockUserAgent:
		flags = append(flags, chromeFlag{"user-agent", randomUserAgent()})
	}
	return flags
}

// randomUserAgent is a Windows desktop Chrome user agent with a random major version.
func randomUserAgent() string {
	//nolint:gosec // a user agent string needs no cryptographic randomness
	major := 90 + rand.IntN(41)
	return fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.0.0 Safari/537.36", major)
}

// ErrNoDisplay is returned when a visible browser is requested but the system has no display.
var ErrNoDisplay = errors.New("browser: no display available for a visible Chrome window (use --headless, or run under xvfb-run)")

func checkDisplay(goos string, getenv func(string) string) error {
	if goos == "linux" && getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
		return ErrNoDisplay
	}
	return nil
}

// ErrDefaultProfileBlocked is returned for --use-default-profile with a Chrome
// that no longer allows remote debugging on its default profile.
var ErrDefaultProfileBlocked = errors.New("browser: Chrome 136 and later refuse remote debugging on the default profile; " +
	"use --user-data-dir with a dedicated directory instead")

// Launch starts Chrome and prepares its tab: fingerprint patches, the capture
// hook, telemetry blocking and the viewport. The browser lives until Close is
// called or ctx is cancelled.
func Launch(ctx context.Context, opts Options) (*Session, error) {
	o := opts.withDefaults()
	log := o.Logger

	chromePath, err := FindChrome(o.ChromePath)
	if err != nil {
		return nil, err
	}
	if !o.Headless {
		if err := checkDisplay(runtime.GOOS, os.Getenv); err != nil {
			return nil, err
		}
	}

	version, verr := ChromeMajorVersion(ctx, chromePath)
	if verr != nil {
		log.Debug("cannot determine the Chrome version", "error", verr)
	}

	profileDir := o.UserDataDir
	if o.UseDefaultProfile {
		if version >= 136 {
			return nil, ErrDefaultProfileBlocked
		}
		if profileDir = defaultProfileDir(runtime.GOOS, os.Getenv); profileDir == "" {
			return nil, errors.New("browser: cannot locate Chrome's default profile directory; use --user-data-dir")
		}
	}
	if profileDir != "" {
		if err := os.MkdirAll(profileDir, 0o700); err != nil {
			return nil, fmt.Errorf("browser: create profile directory: %w", err)
		}
	}

	allocOpts := []chromedp.ExecAllocatorOption{chromedp.ExecPath(chromePath)}
	for _, f := range chromeFlags(o, runtime.GOOS, profileDir) {
		allocOpts = append(allocOpts, chromedp.Flag(f.Name, f.Value))
	}
	if o.ChromeOutput != nil {
		allocOpts = append(allocOpts, chromedp.CombinedOutput(o.ChromeOutput))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	quiet := func(format string, args ...any) { log.Debug(fmt.Sprintf(format, args...), "source", "chromedp") }
	tabCtx, cancelTab := chromedp.NewContext(allocCtx, chromedp.WithErrorf(quiet))

	s := &Session{
		opts:          o,
		log:           log,
		baseURL:       o.BaseURL,
		chromePath:    chromePath,
		chromeVersion: version,
		tabCtx:        tabCtx,
		cancelTab:     cancelTab,
		cancelAlloc:   cancelAlloc,
	}

	log.Info("starting Chrome", "path", chromePath, "version", version, "headless", o.Headless)
	if err := chromedp.Do(tabCtx); err != nil {
		_ = s.Close()
		return nil, fmt.Errorf("browser: start Chrome: %w", err)
	}
	s.started = true
	if err := s.prepare(ctx); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}

// Close shuts the browser down (idempotent).
func (s *Session) Close() error {
	s.closeOnce.Do(func() {
		var err error
		if s.started {
			// Ask Chrome to quit so that it can write its profile (the saved
			// login) to disk.
			ctx, cancel := context.WithTimeout(s.tabCtx, 10*time.Second)
			err = chromedp.Cancel(ctx)
			cancel()
		}
		// Chrome never started when s.started is false; chromedp.Cancel would
		// leave the cancel function below waiting for a browser that is not there.
		s.cancelTab()
		s.cancelAlloc()
		if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.closeErr = err
		}
	})
	return s.closeErr
}

// op returns a context for one operation. It carries the browser tab and ends
// when the session or ctx ends.
func (s *Session) op(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(s.tabCtx)
	stop := context.AfterFunc(ctx, cancel)
	return c, func() {
		stop()
		cancel()
	}
}

// opTimeout is op with a deadline.
func (s *Session) opTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	c, cancel := s.op(ctx)
	c, cancelTimeout := context.WithTimeout(c, d)
	return c, func() {
		cancelTimeout()
		cancel()
	}
}

// evalBool, evalString and evalVoid run a JavaScript expression in the page,
// bounded by the step timeout.
func (s *Session) evalBool(ctx context.Context, expr string) (bool, error) {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	ok, err := chromedp.Run(c, chromedp.Evaluate[bool](expr))
	return ok, contextErr(ctx, err)
}

func (s *Session) evalString(ctx context.Context, expr string) (string, error) {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	str, err := chromedp.Run(c, chromedp.Evaluate[string](expr))
	return str, contextErr(ctx, err)
}

func (s *Session) evalVoid(ctx context.Context, expr string) error {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	return contextErr(ctx, chromedp.Do(c, chromedp.Evaluate[chromedp.Void](expr)))
}

// contextErr prefers the error of the caller's context over the generic
// "context canceled" chromedp reports when an operation's context ends: a
// deadline that ran out must still look like context.DeadlineExceeded, which
// is what the capture loop acts on. Windows surfaces the cancellation first.
func contextErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// waitFor polls a boolean JavaScript expression until it is true. It reports
// false (and no error) when the timeout elapses first; an error means ctx ended.
func (s *Session) waitFor(ctx context.Context, expr string, timeout, interval time.Duration) (bool, error) {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := s.evalBool(ctx, expr)
		switch {
		case err == nil && ok:
			return true, nil
		case err != nil && ctx.Err() != nil:
			return false, ctx.Err()
		case err != nil && s.tabCtx.Err() != nil:
			return false, s.tabCtx.Err()
		case err != nil:
			s.log.Debug("script evaluation failed while waiting", "error", err)
		}
		if !time.Now().Before(deadline) {
			return false, nil
		}
		if err := sleep(ctx, interval); err != nil {
			return false, err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// prepare configures the freshly started tab.
func (s *Session) prepare(ctx context.Context) error {
	c, cancel := s.op(ctx)
	defer cancel()

	// A device scale factor of 0.3 makes the reader's canvas a lot smaller (and
	// cheaper to draw) while the layout keeps the 1920px-wide desktop design.
	if _, err := chromedp.Call(c, emulation.SetDeviceMetricsOverride, emulation.SetDeviceMetricsOverrideParams{
		Width: 0, Height: 0, DeviceScaleFactor: 0.3,
	}); err != nil {
		return fmt.Errorf("browser: set viewport: %w", err)
	}

	if s.opts.Headless && s.opts.UserAgent == "" && !s.opts.MockUserAgent {
		if err := s.hideHeadlessUserAgent(c); err != nil {
			s.log.Debug("cannot adjust the headless user agent", "error", err)
		}
	}

	scripts := []string{assets.StealthJS}
	if s.opts.DebugConsole {
		scripts = append(scripts, "window.__wrxDebug = true;")
	}
	scripts = append(scripts, assets.HookJS)
	for _, src := range scripts {
		if _, err := chromedp.Call(c, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{Source: src}); err != nil {
			return fmt.Errorf("browser: install page script: %w", err)
		}
	}

	if err := s.installInterception(); err != nil {
		return err
	}
	if s.opts.DebugConsole {
		s.forwardConsole()
	}

	// Judge the browser from a document the scripts above have run in.
	if err := chromedp.Do(c, chromedp.Navigate("about:blank")); err != nil {
		return fmt.Errorf("browser: open blank page: %w", err)
	}
	reasons, err := chromedp.Run(c, chromedp.Evaluate[[]string](assets.HeadlessCheckJS))
	if err != nil {
		return fmt.Errorf("browser: inspect browser: %w", err)
	}
	if len(reasons) > 0 {
		s.log.Warn("the browser looks automated or headless; continuing may get the account banned",
			"reasons", strings.Join(reasons, "; "))
		if s.opts.ConfirmHeadless == nil {
			return ErrHeadlessRefused
		}
		ok, err := s.opts.ConfirmHeadless(ctx, reasons)
		if err != nil {
			return err
		}
		if !ok {
			return ErrHeadlessRefused
		}
	}
	return nil
}

// hideHeadlessUserAgent removes "Headless" from the user agent that new
// headless Chrome sends.
func (s *Session) hideHeadlessUserAgent(ctx context.Context) error {
	v, err := chromedp.CallBrowser(ctx, cdpbrowser.GetVersion, cdp.Empty{})
	if err != nil {
		return err
	}
	if !strings.Contains(v.UserAgent, "HeadlessChrome") {
		return nil
	}
	_, err = chromedp.Call(ctx, emulation.SetUserAgentOverride, emulation.SetUserAgentOverrideParams{
		UserAgent: strings.Replace(v.UserAgent, "HeadlessChrome", "Chrome", 1),
	})
	return err
}

// installInterception answers the telemetry requests locally (see rules.go).
func (s *Session) installInterception() error {
	patterns := make([]*fetch.RequestPattern, len(interceptPatterns))
	for i, p := range interceptPatterns {
		patterns[i] = &fetch.RequestPattern{URLPattern: p}
	}
	events := chromedp.Events(s.tabCtx, fetch.RequestPaused)
	if _, err := chromedp.Call(s.tabCtx, fetch.Enable, fetch.EnableParams{Patterns: patterns}); err != nil {
		return fmt.Errorf("browser: enable request interception: %w", err)
	}
	go func() {
		for ev, err := range events {
			if err != nil {
				return
			}
			go s.answerPaused(ev)
		}
	}()
	return nil
}

func (s *Session) answerPaused(ev fetch.EventRequestPaused) {
	if ev.Request == nil {
		return
	}
	d := decideRequest(ev.Request.Method, ev.Request.URL)
	var err error
	switch d.Kind {
	case interceptFulfill:
		headers := make([]*fetch.HeaderEntry, len(d.Headers))
		for i, h := range d.Headers {
			headers[i] = &fetch.HeaderEntry{Name: h.Name, Value: h.Value}
		}
		_, err = chromedp.Call(s.tabCtx, fetch.FulfillRequest, fetch.FulfillRequestParams{
			RequestID:       ev.RequestID,
			ResponseCode:    int64(d.Status),
			ResponseHeaders: headers,
			Body:            []byte(d.Body),
		})
	case interceptBlock:
		_, err = chromedp.Call(s.tabCtx, fetch.FailRequest, fetch.FailRequestParams{
			RequestID:   ev.RequestID,
			ErrorReason: network.ErrorReasonBlockedByClient,
		})
	default:
		_, err = chromedp.Call(s.tabCtx, fetch.ContinueRequest, fetch.ContinueRequestParams{RequestID: ev.RequestID})
	}
	if err != nil && s.tabCtx.Err() == nil {
		s.log.Debug("answering an intercepted request failed", "url", ev.Request.URL, "error", err)
	}
}

// forwardConsole logs everything the page writes to its console.
func (s *Session) forwardConsole() {
	messages := chromedp.Console(s.tabCtx)
	go func() {
		for m, err := range messages {
			if err != nil {
				return
			}
			s.log.Debug("page console", "type", string(m.Type), "text", m.Text)
		}
	}()
}

// UserAgent returns the user agent the browser reports.
func (s *Session) UserAgent(ctx context.Context) (string, error) {
	return s.evalString(ctx, "navigator.userAgent")
}

// ChromeVersion is the major version of the browser (0 if unknown).
func (s *Session) ChromeVersion() int { return s.chromeVersion }

// Navigate loads url and waits for the load event, at most timeout.
func (s *Session) Navigate(ctx context.Context, url string, timeout time.Duration) error {
	c, cancel := s.opTimeout(ctx, timeout)
	defer cancel()
	if err := chromedp.Do(c, chromedp.Navigate(url)); err != nil {
		return fmt.Errorf("browser: open %s: %w", url, contextErr(ctx, err))
	}
	return nil
}
