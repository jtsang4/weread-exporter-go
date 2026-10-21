package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

var errFlaky = errors.New("the page did not render")

func countURL(urls []string, want string) int {
	n := 0
	for _, u := range urls {
		if u == want {
			n++
		}
	}
	return n
}

func TestCaptureRetriesAChapterInTheSameBrowser(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{errFlaky, errFlaky}

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 {
		t.Fatalf("result = %+v", res)
	}
	if h.br.launchCount() != 1 {
		t.Fatalf("launches = %d: a flaky chapter does not need a new browser", h.br.launchCount())
	}
	if n := countURL(h.br.capturedURLs(), h.readerURL(1)); n != 3 {
		t.Fatalf("chapter 2 was attempted %d times, want 3", n)
	}
	mustContain(t, "log", h.logs(), "capturing the chapter failed")
}

func TestCaptureRestartsTheBrowserAfterRepeatedFailures(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{errFlaky, errFlaky, errFlaky}

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 {
		t.Fatalf("result = %+v", res)
	}
	if h.br.launchCount() != 2 {
		t.Fatalf("launches = %d, want 2", h.br.launchCount())
	}
	urls := h.br.capturedURLs()
	if n := countURL(urls, h.readerURL(0)); n != 1 {
		t.Fatalf("chapter 1 was captured %d times; the restart must resume where it stopped", n)
	}
	if !h.br.opened[0].closed || !h.br.opened[1].closed {
		t.Fatal("both browsers must be closed")
	}
	mustContain(t, "log", h.logs(), "restarting the browser")
	if got := h.site.ModeChanges(); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("mode changes = %v: a restart must not restore and switch again", got)
	}
}

func TestCaptureGivesUpAfterTheBrowserWasRestartedTooOften(t *testing.T) {
	h := newHarness(t)
	for range 20 {
		h.br.failures[h.readerURL(1)] = append(h.br.failures[h.readerURL(1)], errFlaky)
	}

	res, err := h.exporter().Run(context.Background())

	if err == nil {
		t.Fatal("expected an error")
	}
	mustContain(t, "error", err.Error(), "giving up")
	mustContain(t, "error", err.Error(), "the page did not render")
	if h.br.launchCount() != maxRestarts {
		t.Fatalf("launches = %d, want %d", h.br.launchCount(), maxRestarts)
	}
	if len(res) != 0 {
		t.Fatalf("results = %+v", res)
	}
	if h.site.Mode() != 1 {
		t.Fatal("the reader mode must be restored")
	}
}

func TestCaptureRetriesAFailingBrowserLaunch(t *testing.T) {
	h := newHarness(t)
	h.br.launchErr = []error{errors.New("chrome exited unexpectedly"), errors.New("chrome exited unexpectedly")}

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 || h.br.launchCount() != 3 {
		t.Fatalf("result = %+v, launches = %d", res, h.br.launchCount())
	}
}

func TestCaptureGivesUpOnABrowserThatNeverStarts(t *testing.T) {
	h := newHarness(t)
	h.br.launchErr = []error{errors.New("boom"), errors.New("boom"), errors.New("boom"), errors.New("boom")}
	_, err := h.exporter().Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "giving up") {
		t.Fatalf("err = %v", err)
	}
	if h.br.launchCount() != maxRestarts {
		t.Fatalf("launches = %d", h.br.launchCount())
	}
}

func TestCaptureDoesNotRetryLaunchErrorsThatCannotGoAway(t *testing.T) {
	for name, launchErr := range map[string]error{
		"no chrome":       &browser.ChromeNotFoundError{Tried: []string{"google-chrome"}},
		"no display":      browser.ErrNoDisplay,
		"default profile": browser.ErrDefaultProfileBlocked,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			h.br.launchErr = []error{launchErr, launchErr, launchErr}
			_, err := h.exporter().Run(context.Background())
			if err == nil || !errors.Is(err, launchErr) {
				t.Fatalf("err = %v", err)
			}
			if h.br.launchCount() != 1 {
				t.Fatalf("launches = %d, want 1", h.br.launchCount())
			}
			if h.site.Mode() != 1 || len(h.site.ModeChanges()) > 0 {
				t.Fatalf("the account's reader mode must not have been touched (mode %d, changes %v)", h.site.Mode(), h.site.ModeChanges())
			}
		})
	}
}

func TestCaptureStopsOnAChapterThatCannotBeRead(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(2)] = []error{&browser.ChapterUnavailableError{Footer: "会员卡 19元/月 · 开通后即可阅读"}}
	_, err := h.exporter().Run(context.Background())
	var unavailable *browser.ChapterUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("err = %v", err)
	}
	if n := countURL(h.br.capturedURLs(), h.readerURL(2)); n != 1 {
		t.Fatalf("an unreadable chapter was tried %d times; retrying cannot help", n)
	}
	if h.br.launchCount() != 1 {
		t.Fatal("no restart for an unreadable chapter")
	}
}

func TestSessionIsRenewedBeforeTheExportStarts(t *testing.T) {
	h := newHarness(t)
	before, err := weread.LoadCookies(h.cookieFile())
	if err != nil {
		t.Fatal(err)
	}
	h.site.ExpireSession()

	h.mustRun(h.exporter())

	if h.site.Renewals() != 1 {
		t.Fatalf("renewals = %d, want 1", h.site.Renewals())
	}
	if len(h.br.injected) == 0 || h.br.injected[0]["wr_skey"] == before.Get("wr_skey") {
		t.Fatalf("the browser got the expired session key: %v", h.br.injected)
	}
	after, err := weread.LoadCookies(h.cookieFile())
	if err != nil {
		t.Fatal(err)
	}
	if after.Get("wr_skey") == before.Get("wr_skey") {
		t.Fatal("the renewed session was not saved")
	}
}

func TestLoginRequiredDuringCaptureRenewsTheSession(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{browser.ErrLoginRequired}
	expired := false
	h.br.onCapture = func(url string) {
		if url == h.readerURL(1) && !expired {
			expired = true
			h.site.ExpireSession() // the session key runs out while the chapter loads
		}
	}

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 {
		t.Fatalf("result = %+v", res)
	}
	if h.site.Renewals() != 1 {
		t.Fatalf("renewals = %d", h.site.Renewals())
	}
	if len(h.br.injected) != 2 {
		t.Fatalf("the renewed cookies must be handed to the running browser: %d injections", len(h.br.injected))
	}
	if h.br.loginCalls != 0 {
		t.Fatal("the QR code is not needed when the session can be renewed")
	}
	// Asking for a login is not the chapter's fault: it does not use up an attempt.
	if h.br.launchCount() != 1 {
		t.Fatalf("launches = %d", h.br.launchCount())
	}
}

func TestLoginLostDuringCaptureAsksForANewLogin(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{browser.ErrLoginRequired}
	h.br.onCapture = func(url string) {
		if url == h.readerURL(1) && h.br.loginCookies == nil {
			// The account's refresh token is revoked: nothing but a new login helps.
			h.site.SetAccount(fakesiteAccount("new-refresh-token"))
			h.br.loginCookies = h.site.LoginCookies()
		}
	}

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 || h.br.loginCalls != 1 {
		t.Fatalf("result = %+v, login calls = %d", res, h.br.loginCalls)
	}
	saved, err := weread.LoadCookies(h.cookieFile())
	if err != nil {
		t.Fatal(err)
	}
	if saved.Get("wr_rt") != "new-refresh-token" {
		t.Fatalf("the new login was not saved: %v", saved.Names())
	}
}

func TestMissingLoginStartsTheQRLogin(t *testing.T) {
	h := newHarness(t)
	h.saveLogin(map[string]string{"wr_vid": "999", "wr_skey": "stale"}) // belongs to nobody
	h.br.loginCookies = h.site.LoginCookies()
	h.br.avatar = false

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 || h.br.loginCalls != 1 {
		t.Fatalf("result = %+v, login calls = %d", res, h.br.loginCalls)
	}
	mustContain(t, "log", h.logs(), "no longer valid")
	if len(h.br.injected) != 0 {
		t.Fatal("there was no login to inject before the QR login")
	}
}

func TestForceLoginIgnoresAValidLoginOnce(t *testing.T) {
	h := newHarness(t)
	h.cfg.ForceLogin = true
	h.br.loginCookies = h.site.LoginCookies()
	h.br.avatar = false

	h.mustRun(h.exporter())

	if h.br.loginCalls != 1 {
		t.Fatalf("login calls = %d, want exactly one: --force-login must not log in again after a browser restart", h.br.loginCalls)
	}
}

func TestHeadlessWithoutALoginFailsBeforeStartingABrowser(t *testing.T) {
	h := newHarness(t)
	h.saveLogin(map[string]string{"wr_vid": "999", "wr_skey": "stale"})
	h.cfg.Headless = true

	_, err := h.exporter().Run(context.Background())

	if !errors.Is(err, ErrLoginNeeded) {
		t.Fatalf("err = %v", err)
	}
	mustContain(t, "error", err.Error(), "weread-exporter login")
	if h.br.launchCount() != 0 {
		t.Fatal("a headless browser cannot show the QR code; there is no point in starting one")
	}
}

func TestHeadlessWithAProfileMayUseItsLogin(t *testing.T) {
	h := newHarness(t)
	h.saveLogin(map[string]string{"wr_vid": "999", "wr_skey": "stale"})
	h.cfg.Headless = true
	h.cfg.UserDataDir = t.TempDir()
	h.br.avatar = true
	h.br.loginCookies = h.site.LoginCookies() // what the profile's browser holds

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 || h.br.loginCalls != 0 {
		t.Fatalf("result = %+v, login calls = %d: the profile is signed in already", res, h.br.loginCalls)
	}
	mustContain(t, "log", h.logs(), "login of the browser profile")
}

func TestHorizontalReaderDuringCaptureIsSwitchedBack(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{browser.ErrHorizontalReader}
	flipped := false
	h.br.onCapture = func(url string) {
		if url == h.readerURL(1) && !flipped {
			flipped = true
			h.site.SetMode(1) // somebody switched the reader in a browser tab
		}
	}

	res := h.mustRun(h.exporter())[0]

	if res.Captured != 3 {
		t.Fatalf("result = %+v", res)
	}
	if got := h.site.ModeChanges(); len(got) != 3 || got[0] != 0 || got[1] != 0 || got[2] != 1 {
		t.Fatalf("mode changes = %v, want [0 0 1]", got)
	}
	if h.site.Mode() != 1 {
		t.Fatal("the original mode must be restored")
	}
}

func TestHorizontalReaderThatCannotBeFixedFails(t *testing.T) {
	h := newHarness(t)
	h.br.failures[h.readerURL(1)] = []error{browser.ErrHorizontalReader, browser.ErrHorizontalReader}
	_, err := h.exporter().Run(context.Background())
	if !errors.Is(err, ErrHorizontalReader) {
		t.Fatalf("err = %v", err)
	}
}

func TestKeepReaderModeRefusesAPagedAccount(t *testing.T) {
	h := newHarness(t)
	h.cfg.KeepReaderMode = true
	_, err := h.exporter().Run(context.Background())
	if !errors.Is(err, ErrHorizontalReader) {
		t.Fatalf("err = %v", err)
	}
	mustContain(t, "error", err.Error(), "keep-reader-mode")
	if len(h.site.ModeChanges()) != 0 {
		t.Fatal("--keep-reader-mode must not change the account")
	}
	if h.br.launchCount() != 0 {
		t.Fatal("no browser is needed to learn that the export cannot work")
	}
}

func TestKeepReaderModeWorksWithAVerticalAccount(t *testing.T) {
	h := newHarness(t)
	h.cfg.KeepReaderMode = true
	h.site.SetMode(0)
	res := h.mustRun(h.exporter())[0]
	if res.Captured != 3 || len(h.site.ModeChanges()) != 0 {
		t.Fatalf("result = %+v, mode changes %v", res, h.site.ModeChanges())
	}
}

func TestTruncatedChaptersAreOnlyReportedForLongChapters(t *testing.T) {
	h := newHarness(t) // chapters list 40 characters: below the check threshold
	h.mustRun(h.exporter())
	mustNotContain(t, "log", h.logs(), "looks too short")
}

func TestCaptureOneGivesUpOnAReaderThatKeepsAskingForALogin(t *testing.T) {
	h := newHarness(t)
	for range 5 {
		h.br.failures[h.readerURL(0)] = append(h.br.failures[h.readerURL(0)], browser.ErrLoginRequired)
	}
	_, err := h.exporter().Run(context.Background())
	if !errors.Is(err, ErrLoginNeeded) {
		t.Fatalf("err = %v", err)
	}
}
