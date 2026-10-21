package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/chromedp/chromedp"
)

// Reader page probes. The reader is a canvas drawn by the site; the injected
// hook (internal/assets/hook.js) turns the drawing calls into Markdown and the
// footer button leads to the next page of the chapter or to the next chapter.
const (
	// footerOrPagerExpr is true once the reader shows its footer button (the
	// vertical reader) or the paged reader's controls (the horizontal reader).
	footerOrPagerExpr = `!!document.querySelector('button.readerFooter_button, .wr_horizontalReader, .renderTarget_pager_button')`
	horizontalExpr    = `!!document.querySelector('.wr_horizontalReader, .renderTarget_pager_button')`
	footerTextExpr    = `(() => { const b = document.getElementsByClassName('readerFooter_button')[0]; return b ? b.innerText : ""; })()`
	footerSelector    = "button.readerFooter_button"
	markdownLenExpr   = `canvasContextHandler.data.markdown.length`
	completeExpr      = `canvasContextHandler.data.complete === true`
	probeExpr         = `JSON.stringify({
  path: location.pathname,
  title: document.title,
  hook: typeof canvasContextHandler === "object",
  horizontal: !!document.querySelector('.wr_horizontalReader, .renderTarget_pager_button')
})`

	// maxPages bounds the "next page" clicks per chapter, a guard against a loop.
	maxPages = 2000
	// pagePoll is how often the page is polled.
	pagePoll = 250 * time.Millisecond
	// settle is how long the Markdown must stay unchanged after a page turn.
	settle = 600 * time.Millisecond
)

// ErrEmptyCapture means the hook produced no text for the chapter.
var ErrEmptyCapture = errors.New("browser: the capture hook produced no text")

type pageProbe struct {
	Path       string `json:"path"`
	Title      string `json:"title"`
	Hook       bool   `json:"hook"`
	Horizontal bool   `json:"horizontal"`
}

func (s *Session) probe(ctx context.Context) (pageProbe, error) {
	var p pageProbe
	raw, err := s.evalString(ctx, probeExpr)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return p, fmt.Errorf("browser: decode page probe: %w", err)
	}
	return p, nil
}

func (s *Session) evalInt(ctx context.Context, expr string) (int, error) {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	return chromedp.Run(c, chromedp.Evaluate[int](expr))
}

// click presses the element with a real mouse click and falls back to a script
// click when the element cannot be clicked that way.
func (s *Session) click(ctx context.Context, selector string) error {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	err := chromedp.Do(c, chromedp.Click(chromedp.CSS(selector)))
	cancel()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.log.Debug("mouse click failed, clicking from script", "selector", selector, "error", err)
	quoted, merr := json.Marshal(selector)
	if merr != nil {
		return merr
	}
	return s.evalVoid(ctx, fmt.Sprintf("document.querySelector(%s).click()", quoted))
}

// CaptureChapter opens a reader URL, walks through all pages of the chapter and
// returns the chapter text as Markdown. loadTimeout bounds the page load.
//
// The returned error wraps ErrLoginRequired when the reader asks for a login,
// ErrHorizontalReader when the account uses the paged reader, and is a
// *ChapterUnavailableError when the chapter cannot be read at all.
func (s *Session) CaptureChapter(ctx context.Context, chapterURL string, loadTimeout time.Duration) (string, error) {
	md, err := s.captureChapter(ctx, chapterURL, loadTimeout)
	if err != nil && ctx.Err() == nil && !errors.Is(err, ErrLoginRequired) {
		s.dump(ctx, chapterURL)
	}
	return md, err
}

func (s *Session) captureChapter(ctx context.Context, chapterURL string, loadTimeout time.Duration) (string, error) {
	if err := s.Navigate(ctx, chapterURL, loadTimeout); err != nil {
		return "", err
	}

	p, err := s.probe(ctx)
	if err != nil {
		return "", fmt.Errorf("browser: inspect reader page: %w", err)
	}
	switch {
	case !strings.HasPrefix(p.Path, "/web/reader/"):
		return "", &UnexpectedPageError{URL: chapterURL, Title: p.Title, Reason: "the browser was redirected to " + p.Path}
	case !p.Hook:
		return "", &UnexpectedPageError{URL: chapterURL, Title: p.Title, Reason: "the capture hook is not active in the page"}
	case p.Horizontal:
		return "", ErrHorizontalReader
	}

pages:
	for page := 1; ; page++ {
		if page > maxPages {
			return "", fmt.Errorf("browser: gave up after %d pages of one chapter", maxPages)
		}
		found, err := s.waitFor(ctx, footerOrPagerExpr, s.opts.FooterTimeout, pagePoll)
		if err != nil {
			return "", err
		}
		if !found {
			// The last chapter of a book has no footer button.
			s.log.Debug("the reader has no footer button; treating this as the end of the book", "url", chapterURL)
			break pages
		}
		if horizontal, err := s.evalBool(ctx, horizontalExpr); err != nil {
			return "", err
		} else if horizontal {
			return "", ErrHorizontalReader
		}

		text, err := s.evalString(ctx, footerTextExpr)
		if err != nil {
			return "", err
		}
		text = strings.TrimSpace(text)
		s.log.Debug("reader footer", "page", page, "button", text)

		switch {
		case text == "下一页":
			if err := s.turnPage(ctx); err != nil {
				return "", err
			}
		case text == "下一章":
			break pages
		case strings.HasPrefix(text, "登录"):
			return "", ErrLoginRequired
		default:
			return "", &ChapterUnavailableError{Footer: text}
		}
	}
	return s.readMarkdown(ctx)
}

// turnPage clicks the footer's "next page" button and waits for the new page to be drawn.
func (s *Session) turnPage(ctx context.Context) error {
	if err := s.evalVoid(ctx, `canvasContextHandler.data.markdown += "\n\n"`); err != nil {
		return err
	}
	before, err := s.evalInt(ctx, markdownLenExpr)
	if err != nil {
		return err
	}
	if err := s.click(ctx, footerSelector); err != nil {
		return fmt.Errorf("browser: click the next page button: %w", err)
	}
	if err := sleep(ctx, s.opts.PageInterval); err != nil {
		return err
	}

	// Wait until the next page has been drawn and the text stopped changing.
	deadline := time.Now().Add(10 * time.Second)
	last, changed, stableSince := before, false, time.Now()
	for {
		n, err := s.evalInt(ctx, markdownLenExpr)
		if err == nil && n != last {
			last, changed, stableSince = n, changed || n != before, time.Now()
		}
		switch {
		case changed && time.Since(stableSince) >= settle:
			return nil
		case !changed && !time.Now().Before(deadline):
			s.log.Debug("no new text after turning the page")
			return nil
		}
		if err := sleep(ctx, pagePoll); err != nil {
			return err
		}
	}
}

// readMarkdown waits for the hook to finish drawing and returns its Markdown.
func (s *Session) readMarkdown(ctx context.Context) (string, error) {
	done, err := s.waitFor(ctx, completeExpr, s.opts.CompleteTimeout, pagePoll)
	if err != nil {
		return "", err
	}
	if !done {
		s.log.Warn("the capture hook did not report completion in time; using the text it has")
	}
	const read = `canvasContextHandler.data.markdown || ""`
	md, err := s.evalString(ctx, read)
	if err != nil {
		return "", err
	}
	if md == "" {
		if err := s.evalVoid(ctx, "canvasContextHandler.updateMarkdown()"); err != nil {
			return "", err
		}
		if md, err = s.evalString(ctx, read); err != nil {
			return "", err
		}
	}
	if md == "" {
		return "", ErrEmptyCapture
	}
	return md, nil
}

var unsafeLabel = regexp.MustCompile(`[^0-9A-Za-z._-]+`)

// dump saves the page's HTML and a screenshot for troubleshooting.
func (s *Session) dump(ctx context.Context, label string) {
	if s.opts.DiagnosticsDir == "" {
		return
	}
	if err := os.MkdirAll(s.opts.DiagnosticsDir, 0o755); err != nil {
		s.log.Warn("cannot create the diagnostics directory", "error", err)
		return
	}
	if i := strings.LastIndex(label, "/"); i >= 0 {
		label = label[i+1:]
	}
	label = unsafeLabel.ReplaceAllString(label, "_")
	if len(label) > 80 {
		label = label[:80]
	}
	if label == "" {
		label = "page"
	}
	stamp := time.Now().Format("20060102-150405")
	base := filepath.Join(s.opts.DiagnosticsDir, stamp+"-"+label)

	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	if html, err := chromedp.Run(c, chromedp.Evaluate[string]("document.documentElement.outerHTML")); err == nil {
		if err := os.WriteFile(base+".html", []byte(html), 0o644); err == nil {
			s.log.Info("saved the page for troubleshooting", "file", base+".html")
		}
	}
	if png, err := chromedp.Run(c, chromedp.CaptureScreenshot()); err == nil {
		if err := os.WriteFile(base+".png", png, 0o644); err == nil {
			s.log.Info("saved a screenshot for troubleshooting", "file", base+".png")
		}
	}
}
