package weread

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	userOK      = `{"userVid":42,"name":"测试用户","avatar":"https://example.invalid/a.png"}`
	userExpired = `{"errCode":-2012,"errMsg":"登录超时"}`
	userMissing = `{"errCode":-2010,"errMsg":"用户不存在"}`
)

func cookiesFor(t *testing.T, vid string, extra map[string]string) *Cookies {
	t.Helper()
	c := NewCookies(filepath.Join(t.TempDir(), "cookie.txt"))
	if vid != "" {
		c.Set("wr_vid", vid)
	}
	for k, v := range extra {
		c.Set(k, v)
	}
	return c
}

func TestUserInfoOK(t *testing.T) {
	var gotQuery, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotCookie = r.URL.RawQuery, r.Header.Get("Cookie")
		_, _ = w.Write([]byte(userOK))
	}))
	defer srv.Close()

	info, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookiesFor(t, "42", map[string]string{"wr_skey": "k"}))
	if err != nil {
		t.Fatal(err)
	}
	if info.VID != 42 || info.Name != "测试用户" {
		t.Fatalf("info = %+v", info)
	}
	if gotQuery != "userVid=42" || !strings.Contains(gotCookie, "wr_skey=k") {
		t.Fatalf("query=%q cookie=%q", gotQuery, gotCookie)
	}
}

func TestUserInfoRenewsExpiredSession(t *testing.T) {
	var renewals atomic.Int32
	var gotMethod, gotType, gotBody, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/web/login/renewal":
			renewals.Add(1)
			body, _ := io.ReadAll(r.Body)
			gotMethod, gotType, gotBody, gotCookie = r.Method, r.Header.Get("Content-Type"), string(body), r.Header.Get("Cookie")
			http.SetCookie(w, &http.Cookie{Name: "wr_skey", Value: "fresh", Path: "/"})
			http.SetCookie(w, &http.Cookie{Name: "wr_pf", Value: "7", Path: "/"})
			_, _ = w.Write([]byte(`{"succ":1}`))
		case "/web/user":
			if strings.Contains(r.Header.Get("Cookie"), "wr_skey=fresh") {
				_, _ = w.Write([]byte(userOK))
				return
			}
			_, _ = w.Write([]byte(userExpired))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cookies := cookiesFor(t, "42", map[string]string{"wr_skey": "stale", "wr_rt": "long-lived"})
	info, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookies)
	if err != nil {
		t.Fatal(err)
	}
	if renewals.Load() != 1 || info.Name == "" {
		t.Fatalf("renewals=%d info=%+v", renewals.Load(), info)
	}
	if gotMethod != "POST" || !strings.HasPrefix(gotType, "application/json") || !strings.Contains(gotBody, `"rq"`) {
		t.Fatalf("renewal request: method=%q type=%q body=%q", gotMethod, gotType, gotBody)
	}
	if !strings.Contains(gotCookie, "wr_rt=long-lived") {
		t.Fatalf("the refresh token was not sent: %q", gotCookie)
	}
	if cookies.Get("wr_skey") != "fresh" || cookies.Get("wr_pf") != "7" || cookies.Get("wr_rt") != "long-lived" {
		t.Fatalf("cookies after renewal = %v", cookies.Map())
	}
	reloaded, err := LoadCookies(cookies.Path())
	if err != nil || reloaded.Get("wr_skey") != "fresh" {
		t.Fatalf("renewed cookies were not persisted: %v %v", reloaded, err)
	}
}

func TestRefreshCookiesRefusedKeepsCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A refused renewal must not be allowed to overwrite good cookies with
		// the anonymous ones the site hands out.
		http.SetCookie(w, &http.Cookie{Name: "wr_skey", Value: "anonymous", Path: "/"})
		_, _ = w.Write([]byte(`{"succ":0,"errCode":-2012,"errMsg":"登录超时"}`))
	}))
	defer srv.Close()

	cookies := cookiesFor(t, "42", map[string]string{"wr_skey": "mine", "wr_rt": "r"})
	err := newTestClient(t, srv.URL, nil).RefreshCookies(context.Background(), cookies)
	if !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("err = %v, want ErrInvalidUser", err)
	}
	if cookies.Get("wr_skey") != "mine" {
		t.Fatalf("a refused renewal changed the cookies: %v", cookies.Map())
	}
}

func TestRefreshCookiesHTTPAndJSONErrors(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"status": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "no", http.StatusForbidden) },
		"json":   func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) },
	} {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()
			err := newTestClient(t, srv.URL, nil).RefreshCookies(context.Background(), cookiesFor(t, "42", nil))
			if err == nil || errors.Is(err, ErrInvalidUser) {
				t.Fatalf("err = %v, want a transport/parse error", err)
			}
		})
	}
}

func TestUserInfoInvalidWhenRefreshDoesNotHelp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(userExpired))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookiesFor(t, "42", nil))
	if !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("err = %v, want ErrInvalidUser", err)
	}
}

func TestUserInfoUserNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(userMissing))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookiesFor(t, "42", nil))
	if !errors.Is(err, ErrInvalidUser) {
		t.Fatalf("err = %v, want ErrInvalidUser", err)
	}
}

func TestUserInfoNeedsVID(t *testing.T) {
	var called atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called.Store(true) }))
	defer srv.Close()

	for _, vid := range []string{"", "abc", "1;rm"} {
		_, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookiesFor(t, vid, nil))
		if !errors.Is(err, ErrInvalidUser) {
			t.Errorf("vid %q: err = %v, want ErrInvalidUser", vid, err)
		}
	}
	if called.Load() {
		t.Fatal("no request may be sent without a usable wr_vid")
	}
}

func TestUserInfoUnknownErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errCode":-1,"errMsg":"boom"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookiesFor(t, "42", nil))
	if err == nil || errors.Is(err, ErrInvalidUser) || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestUserInfoBadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL, nil).UserInfo(context.Background(), cookiesFor(t, "42", nil)); err == nil {
		t.Fatal("expected a parse error")
	}
}
