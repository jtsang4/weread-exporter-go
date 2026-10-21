package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"testing"

	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/fakesite"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

func TestLoginKeepsAValidLogin(t *testing.T) {
	h := newHarness(t)
	res, err := h.exporter().Login(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.AlreadySignedIn || res.User.Name != testName || res.CookieFile != h.cookieFile() {
		t.Fatalf("result = %+v", res)
	}
	if h.br.launchCount() != 0 {
		t.Fatal("no browser is needed when the login still works")
	}
}

func TestLoginRenewsAnExpiredSessionWithoutABrowser(t *testing.T) {
	h := newHarness(t)
	h.site.ExpireSession()
	res, err := h.exporter().Login(context.Background())
	if err != nil || !res.AlreadySignedIn {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if h.site.Renewals() != 1 || h.br.launchCount() != 0 {
		t.Fatalf("renewals = %d, launches = %d", h.site.Renewals(), h.br.launchCount())
	}
}

func TestLoginOpensTheBrowserWithoutAValidLogin(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.cookieFile()); err != nil {
		t.Fatal(err)
	}
	h.br.avatar = false
	h.br.loginCookies = h.site.LoginCookies()
	h.cfg.BookRef = "" // the login command takes no book

	res, err := h.exporter().Login(context.Background())
	if err != nil {
		t.Fatalf("%v\n%s", err, h.logs())
	}
	if res.AlreadySignedIn || res.User.Name != testName {
		t.Fatalf("result = %+v", res)
	}
	if h.br.loginCalls != 1 || h.br.launchCount() != 1 || !h.br.opened[0].closed {
		t.Fatalf("login calls %d, launches %d", h.br.loginCalls, h.br.launchCount())
	}
	if len(h.br.navigations) != 1 || h.br.navigations[0] != h.site.URL()+"/web/bookDetail/"+probeBookID {
		t.Fatalf("navigations = %v", h.br.navigations)
	}
	saved, err := weread.LoadCookies(h.cookieFile())
	if err != nil || saved.VID() != testVID {
		t.Fatalf("saved login: %v, %v", saved.Names(), err)
	}
	if h.br.opened[0].opts.Headless {
		t.Fatal("the QR code needs a visible window")
	}
}

func TestLoginForceReplacesAValidLogin(t *testing.T) {
	h := newHarness(t)
	h.cfg.ForceLogin = true
	h.br.avatar = false
	h.br.loginCookies = h.site.LoginCookies()
	res, err := h.exporter().Login(context.Background())
	if err != nil || res.AlreadySignedIn {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if h.br.loginCalls != 1 {
		t.Fatalf("login calls = %d", h.br.loginCalls)
	}
}

func TestLoginKeepsTheOldLoginWhenTheNewOneFails(t *testing.T) {
	h := newHarness(t)
	h.cfg.ForceLogin = true
	h.br.avatar = false
	h.br.loginErr = browser.ErrLoginTimeout
	before := readFile(t, h.cookieFile())

	if _, err := h.exporter().Login(context.Background()); !errors.Is(err, browser.ErrLoginTimeout) {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, h.cookieFile()) != before {
		t.Fatal("a failed login must not destroy the saved one")
	}
}

func TestLoginRefusesHeadlessWithoutAProfile(t *testing.T) {
	h := newHarness(t)
	h.cfg.ForceLogin = true
	h.cfg.Headless = true
	_, err := h.exporter().Login(context.Background())
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v, want a usage error", err)
	}
	if h.br.launchCount() != 0 {
		t.Fatal("no browser")
	}
}

func TestLoginRejectsCookiesTheSiteDoesNotAccept(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.cookieFile()); err != nil {
		t.Fatal(err)
	}
	h.br.avatar = false
	h.br.loginCookies = map[string]string{"wr_vid": testVID, "wr_skey": "forged", "wr_rt": "nope"}
	_, err := h.exporter().Login(context.Background())
	if !errors.Is(err, ErrLoginNeeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginWithASignedInProfileAdoptsItsCookies(t *testing.T) {
	h := newHarness(t)
	if err := os.Remove(h.cookieFile()); err != nil {
		t.Fatal(err)
	}
	h.cfg.UserDataDir = t.TempDir()
	h.br.avatar = true
	h.br.loginCookies = h.site.LoginCookies()
	res, err := h.exporter().Login(context.Background())
	if err != nil || res.User.Name != testName {
		t.Fatalf("result = %+v, %v", res, err)
	}
	if h.br.loginCalls != 0 {
		t.Fatal("the profile is signed in: no QR code")
	}
}

func TestLogout(t *testing.T) {
	h := newHarness(t)
	e := h.exporter()
	removed, err := e.Logout(context.Background())
	if err != nil || !removed {
		t.Fatalf("removed = %v, %v", removed, err)
	}
	if exists(h.cookieFile()) {
		t.Fatal("the cookie file is still there")
	}
	removed, err = h.exporter().Logout(context.Background())
	if err != nil || removed {
		t.Fatalf("second logout: removed = %v, %v", removed, err)
	}
}

func TestLogoutRestoresAPendingReaderModeFirst(t *testing.T) {
	h := newHarness(t)
	h.site.SetMode(0)
	h.writeMarker(testVID, weread.ReaderModeHorizontal)
	if _, err := h.exporter().Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.site.Mode() != 1 || exists(h.markerPath()) {
		t.Fatalf("mode = %d, marker = %v", h.site.Mode(), exists(h.markerPath()))
	}
}

func TestInfoOfABook(t *testing.T) {
	h := newHarness(t)
	h.prefill(1, 3)
	info, err := h.exporter().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	b := info.Book
	if b == nil || b.Meta.Title != "A Test Book" || len(b.Meta.Chapters) != 3 || b.SoldOut || b.FromCache {
		t.Fatalf("info = %+v", b)
	}
	if got := b.Cached; len(got) != 3 || !got[0] || got[1] || !got[2] || b.CachedCount() != 2 {
		t.Fatalf("cached = %v", got)
	}
}

func TestInfoOfASoldOutBook(t *testing.T) {
	h := newHarness(t)
	book := testBook()
	book.SoldOut = true
	h.site.AddBook(book)
	info, err := h.exporter().Info(context.Background())
	if err != nil || !info.Book.SoldOut {
		t.Fatalf("info = %+v, %v", info, err)
	}
}

func TestInfoOfABookList(t *testing.T) {
	h := newHarness(t)
	h.site.AddBookList("352003193_abc", []fakesite.ListEntry{{BookID: "1234567", Title: "A Test Book"}, {BookID: "7654321", Title: "Second"}})
	h.cfg.BookRef = "352003193_abc"
	info, err := h.exporter().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.ListID != "352003193_abc" || len(info.List) != 2 || info.List[0].ID != testBookID || info.List[1].Title != "Second" {
		t.Fatalf("info = %+v", info)
	}
}

func TestInfoFallsBackToTheCache(t *testing.T) {
	h := newHarness(t)
	h.prefill(1)
	h.onRequest = func(*http.Request) error { return errOffline }
	info, err := h.exporter().Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !info.Book.FromCache || info.Book.CachedCount() != 1 {
		t.Fatalf("info = %+v", info.Book)
	}
}

func TestInfoFailsOfflineWithoutACache(t *testing.T) {
	h := newHarness(t)
	h.onRequest = func(*http.Request) error { return errOffline }
	if _, err := h.exporter().Info(context.Background()); err == nil {
		t.Fatal("expected an error")
	}
}

func TestInfoRejectsABadReference(t *testing.T) {
	h := newHarness(t)
	h.cfg.BookRef = "???"
	_, err := h.exporter().Info(context.Background())
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v", err)
	}
}
