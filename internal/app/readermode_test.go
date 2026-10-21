package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

func (h *harness) markerPath() string { return filepath.Join(h.cacheDir(), "reader-mode-restore.json") }

func (h *harness) writeMarker(vid string, restore weread.ReaderMode) {
	h.t.Helper()
	e := h.exporter()
	if err := os.MkdirAll(h.cacheDir(), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := e.writeMarker(PendingRestore{VID: vid, Restore: restore, Since: time.Now()}); err != nil {
		h.t.Fatal(err)
	}
}

func TestRunRestoresTheReaderModeAnEarlierRunLeftBehind(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	// A run that was killed (kill -9, power loss) left the account vertical.
	h.site.SetMode(0)
	h.writeMarker(testVID, weread.ReaderModeHorizontal)

	h.mustRun(h.exporter())

	if h.site.Mode() != 1 {
		t.Fatal("the account was not put back into the paged reader")
	}
	if got := h.site.ModeChanges(); len(got) != 1 || got[0] != 1 {
		t.Fatalf("mode changes = %v", got)
	}
	if exists(h.markerPath()) {
		t.Fatal("the marker must be removed once the mode is restored")
	}
	mustContain(t, "log", h.logs(), "interrupted")
}

func TestRecoveryLeavesTheMarkerOfAnotherAccount(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.site.SetMode(0)
	h.writeMarker("777", weread.ReaderModeHorizontal)

	h.mustRun(h.exporter())

	if len(h.site.ModeChanges()) != 0 {
		t.Fatalf("mode changes = %v: another account's setting is not ours to change", h.site.ModeChanges())
	}
	if !exists(h.markerPath()) {
		t.Fatal("the marker of the other account must stay")
	}
}

func TestRecoveryIgnoresAnUnreadableMarker(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	if err := os.MkdirAll(h.cacheDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(h.markerPath(), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(h.exporter())
	mustContain(t, "log", h.logs(), "unreadable reader-mode marker")
}

func TestRecoveryThatFailsKeepsTheMarker(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 2, 3)
	h.site.SetMode(0)
	h.writeMarker(testVID, weread.ReaderModeHorizontal)
	h.onRequest = func(r *http.Request) error {
		if r.URL.Path == "/web/user_config/modify" {
			return errOffline
		}
		return nil
	}

	h.mustRun(h.exporter())

	if !exists(h.markerPath()) {
		t.Fatal("a restore that failed must stay recorded for the next run")
	}
	mustContain(t, "log", h.logs(), "stays recorded")
}

func TestFailedRestoreAtTheEndIsAnError(t *testing.T) {
	h := newHarness(t)
	h.onRequest = func(r *http.Request) error {
		if r.URL.Path == "/web/user_config/modify" && len(h.site.ModeChanges()) > 0 {
			return errOffline // switching worked, putting it back does not
		}
		return nil
	}
	res, err := h.exporter().Run(context.Background())
	if err == nil {
		t.Fatal("an account left in the wrong mode is an error")
	}
	mustContain(t, "error", err.Error(), "restore the reader mode")
	if len(res) != 1 || len(res[0].Files) != 1 {
		t.Fatalf("the export itself succeeded and must be reported: %+v", res)
	}
	if !exists(h.markerPath()) {
		t.Fatal("the pending restore must stay recorded")
	}
	mustContain(t, "log", h.logs(), "reader-mode restore")

	// Back online: the next run puts things right.
	h.onRequest = nil
	h.cfg.Overwrite = true
	h.mustRun(h.exporter())
	if h.site.Mode() != 1 || exists(h.markerPath()) {
		t.Fatalf("mode = %d, marker exists = %v", h.site.Mode(), exists(h.markerPath()))
	}
}

func TestMarkerRecordsTheOriginalModeEvenAfterRepeatedSwitches(t *testing.T) {
	h := newHarness(t)
	e := h.exporter()
	ctx := context.Background()
	if err := e.ensureVertical(ctx, testBookID); err != nil {
		t.Fatal(err)
	}
	h.site.SetMode(1) // flipped behind our back
	if err := e.ensureVertical(ctx, testBookID); err != nil {
		t.Fatal(err)
	}
	rec, err := e.readMarker()
	if err != nil || rec == nil || rec.Restore != weread.ReaderModeHorizontal || rec.VID != testVID {
		t.Fatalf("marker = %+v, %v", rec, err)
	}
	if err := e.restoreReaderMode(ctx); err != nil {
		t.Fatal(err)
	}
	if h.site.Mode() != 1 || exists(h.markerPath()) {
		t.Fatal("the original mode was not restored")
	}
	// Nothing to restore any more.
	if err := e.restoreReaderMode(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureVerticalNeedsALogin(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.cookieFile()); err != nil {
		t.Fatal(err)
	}
	err := h.exporter().ensureVertical(context.Background(), testBookID)
	if !errors.Is(err, ErrLoginNeeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestReaderModeCommands(t *testing.T) {
	h := newHarness(t)
	e := h.exporter()
	ctx := context.Background()

	status, err := e.ReaderMode(ctx)
	if err != nil || status.Mode != weread.ReaderModeHorizontal || status.Pending != nil {
		t.Fatalf("status = %+v, %v", status, err)
	}

	if err := e.SetReaderMode(ctx, weread.ReaderModeVertical); err != nil {
		t.Fatal(err)
	}
	if h.site.Mode() != 0 {
		t.Fatal("the mode was not switched")
	}

	h.writeMarker(testVID, weread.ReaderModeHorizontal)
	status, err = e.ReaderMode(ctx)
	if err != nil || status.Pending == nil || status.Pending.Restore != weread.ReaderModeHorizontal {
		t.Fatalf("status = %+v, %v", status, err)
	}

	restored, err := e.RestoreReaderMode(ctx)
	if err != nil || !restored || h.site.Mode() != 1 {
		t.Fatalf("restored = %v, %v, mode %d", restored, err, h.site.Mode())
	}
	restored, err = e.RestoreReaderMode(ctx)
	if err != nil || restored {
		t.Fatalf("with nothing pending: restored = %v, %v", restored, err)
	}

	// Choosing a mode explicitly cancels the pending restore: the user decided.
	h.writeMarker(testVID, weread.ReaderModeHorizontal)
	if err := e.SetReaderMode(ctx, weread.ReaderModeVertical); err != nil {
		t.Fatal(err)
	}
	if exists(h.markerPath()) {
		t.Fatal("an explicit choice must discard the pending restore")
	}
}

func TestReaderModeCommandsNeedAValidLogin(t *testing.T) {
	h := newHarness(t)
	h.saveLogin(map[string]string{"wr_vid": "999", "wr_skey": "stale"})
	e := h.exporter()
	if _, err := e.ReaderMode(context.Background()); !errors.Is(err, ErrLoginNeeded) {
		t.Fatalf("err = %v", err)
	}
	if err := e.SetReaderMode(context.Background(), weread.ReaderModeVertical); !errors.Is(err, ErrLoginNeeded) {
		t.Fatalf("err = %v", err)
	}
}
