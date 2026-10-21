package browser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/chromedp"
)

// PDFOptions configures PrintHTMLToPDF.
type PDFOptions struct {
	// ChromePath is the browser executable; "" searches for one (see FindChrome).
	ChromePath string
	// NoSandbox disables Chrome's sandbox (needed in some containers).
	NoSandbox bool
	// Timeout bounds the whole rendering (default 10 minutes).
	Timeout time.Duration
	// ChromeOutput receives Chrome's own stdout/stderr (nil discards it).
	ChromeOutput io.Writer
	// Logger receives progress messages; nil discards them.
	Logger *slog.Logger
}

// PrintHTMLToPDF renders a local HTML file with a headless Chrome and returns
// the PDF. The page size and margins come from the document's @page rule.
//
// Page.printToPDF only exists in headless mode, so this always starts its own
// browser with a throw-away profile, independent of the capture session. Scripts
// are disabled in the rendered page.
func PrintHTMLToPDF(ctx context.Context, htmlPath string, opts PDFOptions) ([]byte, error) {
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Minute
	}
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	chromePath, err := FindChrome(opts.ChromePath)
	if err != nil {
		return nil, err
	}
	pageURL, err := fileURL(htmlPath)
	if err != nil {
		return nil, err
	}

	allocOpts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", "new"),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
	}
	if runtime.GOOS == "linux" {
		allocOpts = append(allocOpts, chromedp.Flag("disable-dev-shm-usage", true))
	}
	if opts.NoSandbox {
		allocOpts = append(allocOpts, chromedp.NoSandbox)
	}
	if opts.ChromeOutput != nil {
		allocOpts = append(allocOpts, chromedp.CombinedOutput(opts.ChromeOutput))
	}

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, allocOpts...)
	defer cancelAlloc()
	quiet := func(format string, args ...any) { log.Debug(fmt.Sprintf(format, args...), "source", "chromedp") }
	tabCtx, cancelTab := chromedp.NewContext(allocCtx, chromedp.WithErrorf(quiet))
	defer cancelTab()

	log.Info("rendering the PDF with headless Chrome", "path", chromePath)
	if err := chromedp.Do(tabCtx); err != nil {
		return nil, fmt.Errorf("browser: start Chrome for the PDF: %w", err)
	}

	runCtx, cancel := context.WithTimeout(tabCtx, opts.Timeout)
	defer cancel()
	if _, err := chromedp.Call(runCtx, emulation.SetScriptExecutionDisabled, emulation.SetScriptExecutionDisabledParams{Value: true}); err != nil {
		return nil, fmt.Errorf("browser: disable scripts: %w", err)
	}
	if err := chromedp.Do(runCtx, chromedp.Navigate(pageURL)); err != nil {
		return nil, fmt.Errorf("browser: load %s: %w", htmlPath, wrapTimeout(err, opts.Timeout))
	}
	pdf, err := chromedp.Run(runCtx, chromedp.PrintToPDF(
		chromedp.PDFPrintBackground(),
		chromedp.PDFPreferCSSPageSize(),
		chromedp.PDFOutlineAndTagged(),
		chromedp.PDFStream(),
	))
	if err != nil {
		return nil, fmt.Errorf("browser: print to PDF: %w", wrapTimeout(err, opts.Timeout))
	}
	if len(pdf) == 0 {
		return nil, errors.New("browser: Chrome produced an empty PDF")
	}
	return pdf, nil
}

func wrapTimeout(err error, d time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("timed out after %s: %w", d, err)
	}
	return err
}

// fileURL converts a local path to a file:// URL.
func fileURL(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash // Windows drive letter: file:///C:/...
	}
	return (&url.URL{Scheme: "file", Path: slash}).String(), nil
}
