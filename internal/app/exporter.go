package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/export"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// browserSession is the part of *browser.Session the exporter uses; tests
// replace it.
type browserSession interface {
	InjectCookies(ctx context.Context, cookies map[string]string) error
	Navigate(ctx context.Context, url string, timeout time.Duration) error
	WaitForAvatar(ctx context.Context, timeout time.Duration) (bool, error)
	Login(ctx context.Context) (map[string]string, error)
	Cookies(ctx context.Context) (map[string]string, error)
	CaptureChapter(ctx context.Context, url string, loadTimeout time.Duration) (string, error)
	Close() error
}

type launchFunc func(ctx context.Context, opts browser.Options) (browserSession, error)

type pdfFunc func(ctx context.Context, htmlPath string, opts browser.PDFOptions) ([]byte, error)

func defaultLaunch(ctx context.Context, opts browser.Options) (browserSession, error) {
	s, err := browser.Launch(ctx, opts)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Exporter exports books. Build one with New; it is not safe for concurrent use.
type Exporter struct {
	cfg      Config
	log      *slog.Logger
	api      *weread.Client
	cookies  *weread.Cookies
	extraCSS string

	out      io.Writer // user-facing results
	in       io.Reader // answers to prompts
	launch   launchFunc
	printPDF pdfFunc
	findMOBI func(explicit string) (string, error)
	sleep    func(ctx context.Context, d time.Duration) error
	now      func() time.Time

	// state of the current run
	modeRestore   *PendingRestore
	forcedLogin   bool // --force-login has been carried out
	lastValidated time.Time
	mobiConverter string // program that converts EPUB to MOBI, when MOBI is requested
}

// Option customises an Exporter.
type Option func(*Exporter) error

// WithHTTPClient replaces the HTTP client used for the site and image requests.
func WithHTTPClient(c *http.Client) Option {
	return func(e *Exporter) error {
		api, err := weread.NewClient(weread.Options{
			BaseURL: e.cfg.BaseURL, ProxyURL: e.cfg.ProxyServer, Logger: e.log, HTTPClient: c,
			UserAgent: weread.DefaultUserAgent,
		})
		if err != nil {
			return err
		}
		e.api = api
		return nil
	}
}

// WithOutput sets where results (file names, the book table) are printed.
func WithOutput(w io.Writer) Option {
	return func(e *Exporter) error { e.out = w; return nil }
}

// WithInput sets where prompt answers are read from.
func WithInput(r io.Reader) Option {
	return func(e *Exporter) error { e.in = r; return nil }
}

// New validates cfg and prepares an Exporter. It reads the saved login cookies
// but does not touch the network.
func New(cfg Config, log *slog.Logger, opts ...Option) (*Exporter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	e := &Exporter{
		cfg:      cfg,
		log:      log,
		out:      io.Discard,
		in:       os.Stdin,
		launch:   defaultLaunch,
		printPDF: browser.PrintHTMLToPDF,
		findMOBI: export.FindMOBIConverter,
		sleep:    sleepContext,
		now:      time.Now,
	}
	if cfg.CSSFile != "" {
		css, err := os.ReadFile(cfg.CSSFile)
		if err != nil {
			return nil, fmt.Errorf("read CSS file: %w", err)
		}
		e.extraCSS = string(css)
	}
	api, err := weread.NewClient(weread.Options{
		BaseURL:   cfg.BaseURL,
		ProxyURL:  cfg.ProxyServer,
		Logger:    log,
		UserAgent: weread.DefaultUserAgent,
	})
	if err != nil {
		return nil, err
	}
	e.api = api
	for _, opt := range opts {
		if err := opt(e); err != nil {
			return nil, err
		}
	}
	e.cookies, err = weread.LoadCookies(cfg.CookieFile)
	if err != nil {
		return nil, err
	}
	return e, nil
}

// Config returns the effective configuration (defaults applied).
func (e *Exporter) Config() Config { return e.cfg }

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// printf writes a line of user-facing output.
func (e *Exporter) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(e.out, format, args...)
}

// browserOptions are the browser settings derived from the configuration.
func (e *Exporter) browserOptions() browser.Options {
	return browser.Options{
		ChromePath:        e.cfg.ChromePath,
		Headless:          e.cfg.Headless,
		NoSandbox:         e.cfg.NoSandbox,
		UserDataDir:       e.cfg.UserDataDir,
		UseDefaultProfile: e.cfg.UseDefaultProfile,
		MockUserAgent:     e.cfg.MockUserAgent,
		ProxyServer:       e.cfg.ProxyServer,
		BaseURL:           e.cfg.BaseURL,
		DiagnosticsDir:    filepath.Join(e.cfg.CacheDir, "diagnostics"),
		DebugConsole:      e.cfg.DebugBrowser,
		ConfirmHeadless:   e.confirmHeadless,
		Logger:            e.log,
	}
}

// confirmHeadless asks whether to go on although the browser looks automated.
func (e *Exporter) confirmHeadless(ctx context.Context, reasons []string) (bool, error) {
	e.log.Warn("the browser looks automated or headless; WeRead may ban accounts that scrape", "signs", strings.Join(reasons, "; "))
	return e.confirm(ctx, "Continue anyway? This may get the account banned.")
}

// confirm asks a yes/no question (default no). It answers yes when --yes was
// given and no when there is nobody to ask.
func (e *Exporter) confirm(ctx context.Context, question string) (bool, error) {
	if e.cfg.AssumeYes {
		e.log.Info("assuming yes", "question", question)
		return true, nil
	}
	if f, ok := e.in.(*os.File); ok {
		if info, err := f.Stat(); err != nil || info.Mode()&os.ModeCharDevice == 0 {
			e.log.Warn("not asking because input is not a terminal (use --yes to continue)", "question", question)
			return false, nil
		}
	}
	_, _ = fmt.Fprintf(os.Stderr, "%s [y/N] ", question)

	answers := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(e.in).ReadString('\n')
		answers <- strings.TrimSpace(line)
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case a := <-answers:
		switch strings.ToLower(a) {
		case "y", "yes":
			return true, nil
		}
		return false, nil
	}
}

// detached returns a context that outlives ctx (cleanup after Ctrl-C) but not
// longer than d.
func detached(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), d)
}

// isCancel reports whether err is a cancellation or timeout of the whole run.
func isCancel(err error) bool {
	return errors.Is(err, context.Canceled)
}
