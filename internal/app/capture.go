package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/book"
	"github.com/jtsang4/weread-exporter-go/internal/browser"
)

const (
	// maxAttempts is how often one chapter is tried in the same browser.
	maxAttempts = 3
	// maxRestarts is how often the browser is restarted for one book before the
	// export gives up (the original tool retried forever).
	maxRestarts = 3
	// retryPause is the base pause between two attempts at a chapter.
	retryPause = 2 * time.Second
	// truncationRatio flags a chapter whose captured text is much shorter than
	// the length the table of contents promises: the text normally has 1.1 to
	// 1.2 characters per listed character (Markdown markup adds some).
	truncationRatio = 0.6
	// minTruncationCheck is the shortest listed length the check applies to.
	minTruncationCheck = 200
)

// restartError asks for a fresh browser: the current one kept failing.
type restartError struct{ cause error }

func (e *restartError) Error() string { return e.cause.Error() }
func (e *restartError) Unwrap() error { return e.cause }

// captureChapters captures the chapters at the given positions into the cache.
// Chapters that cannot be read at all (not free, not owned) and problems that a
// retry cannot fix end the export with an error.
func (e *Exporter) captureChapters(ctx context.Context, bookID string, meta *book.Meta, store *book.Store, todo []int) error {
	if err := store.EnsureDirs(); err != nil {
		return err
	}
	remaining := append([]int(nil), todo...)
	restarts := 0
	for len(remaining) > 0 {
		sess, err := e.startSession(ctx, bookID)
		if err != nil {
			if ctx.Err() != nil || !launchRetryable(err) {
				return err
			}
			restarts++
			e.log.Warn("could not start the browser", "error", err, "attempt", restarts, "of", maxRestarts)
			if restarts >= maxRestarts {
				return fmt.Errorf("giving up after %d failed attempts to start the browser: %w", restarts, err)
			}
			if err := e.sleep(ctx, retryPause); err != nil {
				return err
			}
			continue
		}

		var done int
		err = e.captureLoop(ctx, sess, bookID, meta, store, remaining, &done)
		remaining = remaining[done:]
		closeErr := sess.Close()
		if closeErr != nil {
			e.log.Debug("closing the browser failed", "error", closeErr)
		}
		var restart *restartError
		switch {
		case err == nil:
			return nil
		case errors.As(err, &restart) && ctx.Err() == nil:
			restarts++
			e.log.Warn("restarting the browser", "reason", restart.cause, "attempt", restarts, "of", maxRestarts)
			if restarts >= maxRestarts {
				return fmt.Errorf("giving up after restarting the browser %d times: %w", restarts, restart.cause)
			}
			if err := e.sleep(ctx, retryPause); err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

// launchRetryable says whether starting the browser may work the next time.
func launchRetryable(err error) bool {
	var notFound *browser.ChromeNotFoundError
	switch {
	case errors.As(err, &notFound),
		errors.Is(err, browser.ErrNoDisplay),
		errors.Is(err, browser.ErrDefaultProfileBlocked),
		errors.Is(err, browser.ErrHeadlessRefused),
		errors.Is(err, browser.ErrLoginNeedsWindow),
		errors.Is(err, browser.ErrLoginTimeout),
		errors.Is(err, ErrLoginNeeded),
		errors.Is(err, ErrHorizontalReader):
		return false
	}
	return true
}

// captureLoop captures chapters one after the other until the list is done or
// something goes wrong. done counts the chapters that were stored.
func (e *Exporter) captureLoop(ctx context.Context, sess browserSession, bookID string, meta *book.Meta, store *book.Store, todo []int, done *int) error {
	for n, pos := range todo {
		ch := meta.Chapters[pos]
		e.log.Info("capturing chapter", "n", pos+1, "of", len(meta.Chapters), "title", ch.Title, "progress", fmt.Sprintf("%d/%d", n+1, len(todo)))

		md, err := e.captureOne(ctx, sess, bookID, ch)
		if err != nil {
			var unavailable *browser.ChapterUnavailableError
			if errors.As(err, &unavailable) {
				return fmt.Errorf("chapter %d %q cannot be read: %w\n(use --chapters to export only the chapters that are free or that you own; `weread-exporter info -b %s` lists them)",
					pos+1, ch.Title, err, e.cfg.BookRef)
			}
			var restart *restartError
			if errors.As(err, &restart) {
				return err
			}
			return fmt.Errorf("chapter %d %q: %w", pos+1, ch.Title, err)
		}
		e.warnIfTruncated(pos, ch, md)
		if err := store.SaveChapter(pos, ch.ID, md); err != nil {
			return fmt.Errorf("save chapter %d: %w", pos+1, err)
		}
		*done = n + 1
		e.log.Debug("chapter saved", "file", store.ChapterPath(pos, ch.ID), "bytes", len(md))

		if n < len(todo)-1 {
			// Be gentle with the site: one chapter every few seconds is what a reader does.
			if err := e.sleep(ctx, e.cfg.LoadInterval); err != nil {
				return err
			}
		}
	}
	return nil
}

// captureOne captures one chapter, working around login prompts and the reader mode.
func (e *Exporter) captureOne(ctx context.Context, sess browserSession, bookID string, ch book.Chapter) (string, error) {
	url := e.api.ReaderURL(bookID, ch.ID)
	var last error
	loginRounds, modeRounds := 0, 0
	for attempt := 1; attempt <= maxAttempts; {
		if err := e.keepSessionFresh(ctx, sess); err != nil {
			return "", err
		}
		md, err := sess.CaptureChapter(ctx, url, e.cfg.LoadTimeout)
		switch {
		case err == nil:
			return md, nil
		case ctx.Err() != nil:
			return "", ctx.Err()
		case errors.Is(err, browser.ErrLoginRequired):
			if loginRounds++; loginRounds > 2 {
				return "", fmt.Errorf("%w: the reader keeps asking for a login (%v)", ErrLoginNeeded, err)
			}
			if err := e.handleLoginRequired(ctx, sess); err != nil {
				return "", err
			}
			continue // not the chapter's fault: no attempt is used up
		case errors.Is(err, browser.ErrHorizontalReader):
			if modeRounds++; modeRounds > 1 {
				return "", fmt.Errorf("%w: the reader is still paged after switching the account to the vertical reader", ErrHorizontalReader)
			}
			if err := e.ensureVertical(ctx, bookID); err != nil {
				return "", err
			}
			continue
		case !browser.Retryable(err):
			return "", err
		}

		last = err
		e.log.Warn("capturing the chapter failed", "title", ch.Title, "attempt", attempt, "of", maxAttempts, "error", err)
		attempt++
		if attempt <= maxAttempts {
			if err := e.sleep(ctx, time.Duration(attempt-1)*retryPause); err != nil {
				return "", err
			}
		}
	}
	return "", &restartError{cause: fmt.Errorf("chapter %q failed %d times: %w", ch.Title, maxAttempts, last)}
}

// warnIfTruncated points out chapters whose captured text looks too short.
func (e *Exporter) warnIfTruncated(pos int, ch book.Chapter, md string) {
	if ch.Words < minTruncationCheck {
		return
	}
	got := len([]rune(md))
	if float64(got) < truncationRatio*float64(ch.Words) {
		e.log.Warn("the captured text looks too short; the chapter may be cut off (delete its cached file to capture it again)",
			"chapter", pos+1, "title", ch.Title, "captured", got, "listed", ch.Words)
	}
}
