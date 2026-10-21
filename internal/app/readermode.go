package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/fsutil"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// The capture hook only understands the vertical (scrolling) reader, but the
// reader mode is a setting of the WeRead account, not of the browser. So an
// export switches the account to the vertical reader and puts the previous mode
// back when it is done. A marker file in the cache directory records the pending
// restore, so that it still happens after a crash (see recoverReaderMode and the
// `reader-mode restore` command).

// DefaultProbeBook is the numeric id of the book whose reader page is read to
// learn the account's reader mode when no book was named (any book will do).
const DefaultProbeBook = "34336681"

var probeBookID = weread.MustHash(DefaultProbeBook)

// PendingRestore is a reader-mode change that has to be undone.
type PendingRestore struct {
	VID     string            `json:"vid"`
	Restore weread.ReaderMode `json:"restore"`
	Since   time.Time         `json:"since"`
}

func (e *Exporter) markerPath() string {
	return filepath.Join(e.cfg.CacheDir, "reader-mode-restore.json")
}

func (e *Exporter) writeMarker(r PendingRestore) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return fsutil.WriteFileAtomic(e.markerPath(), append(data, '\n'), 0o600)
}

// readMarker returns the pending restore, or nil if there is none.
func (e *Exporter) readMarker() (*PendingRestore, error) {
	data, err := os.ReadFile(e.markerPath())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r PendingRestore
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse %s: %w", e.markerPath(), err)
	}
	return &r, nil
}

func (e *Exporter) removeMarker() {
	if err := os.Remove(e.markerPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		e.log.Warn("could not remove the reader-mode marker", "file", e.markerPath(), "error", err)
	}
}

// ensureVertical makes the account use the vertical reader, remembering the
// previous mode so that restoreReaderMode can put it back.
func (e *Exporter) ensureVertical(ctx context.Context, bookID string) error {
	vid := e.cookies.VID()
	if vid == "" {
		return fmt.Errorf("%w: the reader mode can only be checked when signed in; %s", ErrLoginNeeded, loginHint)
	}
	mode, err := e.api.ReaderMode(ctx, e.cookies, bookID)
	if err != nil {
		return fmt.Errorf("read the account's reader mode: %w", err)
	}
	if mode == weread.ReaderModeVertical {
		e.log.Debug("the account already uses the vertical reader")
		return nil
	}
	if e.cfg.KeepReaderMode {
		return fmt.Errorf("%w and --keep-reader-mode forbids switching it; choose \"上下滚动阅读\" in the web reader's settings, or run `weread-exporter reader-mode vertical`", ErrHorizontalReader)
	}
	if err := e.refreshSession(ctx); err != nil {
		return err
	}

	e.log.Info("switching the account to the vertical reader for the export; the previous mode is restored afterwards", "was", mode.String())
	rec := PendingRestore{VID: vid, Restore: mode, Since: e.now().UTC()}
	if e.modeRestore != nil {
		rec = *e.modeRestore // already switched earlier in this run; keep the original mode
	}
	if err := e.writeMarker(rec); err != nil {
		return fmt.Errorf("record the pending reader-mode restore: %w", err)
	}
	if err := e.api.SetReaderMode(ctx, e.cookies, weread.ReaderModeVertical); err != nil {
		e.removeMarker()
		return fmt.Errorf("switch the account to the vertical reader: %w", err)
	}
	e.modeRestore = &rec

	after, err := e.api.ReaderMode(ctx, e.cookies, bookID)
	if err != nil {
		return fmt.Errorf("verify the reader mode: %w", err)
	}
	if after != weread.ReaderModeVertical {
		return fmt.Errorf("%w: the account is still in %s mode after switching it", ErrHorizontalReader, after)
	}
	return nil
}

// restoreReaderMode puts back the reader mode ensureVertical replaced. It runs
// even after the export was cancelled.
func (e *Exporter) restoreReaderMode(ctx context.Context) error {
	if e.modeRestore == nil {
		return nil
	}
	rec := *e.modeRestore
	if err := e.applyRestore(ctx, rec); err != nil {
		e.log.Error("could not restore the account's reader mode; run `weread-exporter reader-mode restore` once you are online",
			"error", err)
		return fmt.Errorf("restore the reader mode: %w", err)
	}
	e.modeRestore = nil
	return nil
}

// applyRestore sets the recorded mode and forgets the marker.
func (e *Exporter) applyRestore(ctx context.Context, rec PendingRestore) error {
	ctx, cancel := detached(ctx, time.Minute)
	defer cancel()
	if e.cookies.VID() != rec.VID {
		return fmt.Errorf("the saved login belongs to a different account than the one whose reader mode changed (%s)", rec.VID)
	}
	if err := e.refreshSession(ctx); err != nil {
		return err
	}
	e.log.Info("restoring the account's reader mode", "mode", rec.Restore.String())
	if err := e.api.SetReaderMode(ctx, e.cookies, rec.Restore); err != nil {
		return err
	}
	e.removeMarker()
	return nil
}

// recoverReaderMode finishes a restore that an interrupted run left behind.
func (e *Exporter) recoverReaderMode(ctx context.Context) {
	rec, err := e.readMarker()
	if err != nil {
		e.log.Warn("ignoring an unreadable reader-mode marker", "error", err)
		return
	}
	if rec == nil {
		return
	}
	if e.cookies.VID() != rec.VID {
		e.log.Warn("a reader-mode restore is pending for another account; leaving it alone", "file", e.markerPath())
		return
	}
	e.log.Warn("an earlier run was interrupted before it restored the account's reader mode")
	if err := e.applyRestore(ctx, *rec); err != nil {
		e.log.Warn("could not restore the reader mode now; it stays recorded", "error", err)
	}
}

// ReaderModeStatus describes the account's reader mode.
type ReaderModeStatus struct {
	Mode    weread.ReaderMode
	Pending *PendingRestore // a restore an earlier run left behind
}

// ReaderMode reports the account's reader mode. It needs a valid login.
func (e *Exporter) ReaderMode(ctx context.Context) (*ReaderModeStatus, error) {
	if err := e.refreshSession(ctx); err != nil {
		return nil, err
	}
	var (
		mode    weread.ReaderMode
		lastErr error
		found   bool
	)
	for _, id := range e.probeBooks() {
		m, err := e.api.ReaderMode(ctx, e.cookies, id)
		if err == nil {
			mode, found = m, true
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		e.log.Debug("could not read the reader mode from this book's page", "book", id, "error", err)
		lastErr = err
	}
	if !found {
		return nil, fmt.Errorf("read the account's reader mode: %w", lastErr)
	}
	pending, err := e.readMarker()
	if err != nil {
		e.log.Warn("ignoring an unreadable reader-mode marker", "error", err)
	}
	return &ReaderModeStatus{Mode: mode, Pending: pending}, nil
}

// SetReaderMode switches the account to the given mode and forgets any pending
// restore (the user decided).
func (e *Exporter) SetReaderMode(ctx context.Context, mode weread.ReaderMode) error {
	if err := e.refreshSession(ctx); err != nil {
		return err
	}
	if err := e.api.SetReaderMode(ctx, e.cookies, mode); err != nil {
		return err
	}
	e.removeMarker()
	return nil
}

// RestoreReaderMode undoes the switch an interrupted run left behind. It
// reports whether there was something to restore.
func (e *Exporter) RestoreReaderMode(ctx context.Context) (bool, error) {
	rec, err := e.readMarker()
	if err != nil {
		return false, err
	}
	if rec == nil {
		return false, nil
	}
	if err := e.applyRestore(ctx, *rec); err != nil {
		return false, err
	}
	return true, nil
}

// probeBooks are the books whose reader page tells the account's reader mode:
// the book being exported, if any, and the default.
func (e *Exporter) probeBooks() []string {
	var ids []string
	if ref, err := weread.ParseRef(e.cfg.BookRef); err == nil && ref.Kind == weread.RefBook {
		ids = append(ids, ref.ID)
	}
	if len(ids) == 0 || ids[0] != probeBookID {
		ids = append(ids, probeBookID)
	}
	return ids
}

// probeBook is the book a page is opened for when no particular book is needed.
func (e *Exporter) probeBook() string { return e.probeBooks()[0] }
