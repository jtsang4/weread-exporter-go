package weread

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func readerPage(mode string) []byte {
	return []byte(`<html><script>window.__INITIAL_STATE__={"config":{"theme":"dark","useHorizonReader":` + mode + `},"reader":{}};</script></html>`)
}

func TestParseReaderMode(t *testing.T) {
	cases := []struct {
		name string
		page []byte
		want ReaderMode
	}{
		{"horizontal number", readerPage("1"), ReaderModeHorizontal},
		{"horizontal string", readerPage(`"1"`), ReaderModeHorizontal},
		{"vertical number", readerPage("0"), ReaderModeVertical},
		{"vertical string", readerPage(`"0"`), ReaderModeVertical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseReaderMode(tc.page)
			if err != nil || got != tc.want {
				t.Fatalf("got %v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestParseReaderModeErrors(t *testing.T) {
	for name, page := range map[string]string{
		"no state":       `<html></html>`,
		"no config":      `<script>window.__INITIAL_STATE__={"reader":{}}</script>`,
		"no mode field":  `<script>window.__INITIAL_STATE__={"config":{"theme":"dark"}}</script>`,
		"broken payload": `<script>window.__INITIAL_STATE__={"config":</script>`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseReaderMode([]byte(page)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestReaderModeString(t *testing.T) {
	if ReaderModeVertical.String() != "vertical" || ReaderModeHorizontal.String() != "horizontal" || ReaderMode(7).String() != "ReaderMode(7)" {
		t.Fatal("unexpected String()")
	}
}

func TestClientReaderMode(t *testing.T) {
	var gotPath, gotCookie string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotCookie = r.URL.Path, r.Header.Get("Cookie")
		_, _ = w.Write(readerPage("1"))
	}))
	defer srv.Close()

	cookies := cookiesFor(t, "42", map[string]string{"wr_skey": "k"})
	mode, err := newTestClient(t, srv.URL, nil).ReaderMode(context.Background(), cookies, "abc12345")
	if err != nil || mode != ReaderModeHorizontal {
		t.Fatalf("mode=%v err=%v", mode, err)
	}
	if gotPath != "/web/reader/abc12345" || !strings.Contains(gotCookie, "wr_skey=k") {
		t.Fatalf("path=%q cookie=%q", gotPath, gotCookie)
	}
}

func TestSetReaderMode(t *testing.T) {
	var gotMethod, gotPath, gotType, gotCookie, gotReferer, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotType, gotCookie, gotReferer = r.Header.Get("Content-Type"), r.Header.Get("Cookie"), r.Header.Get("Referer")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		_, _ = io.WriteString(w, `{"useHorizonReader":"0"}`)
	}))
	defer srv.Close()

	cookies := cookiesFor(t, "42", map[string]string{"wr_skey": "k"})
	if err := newTestClient(t, srv.URL, nil).SetReaderMode(context.Background(), cookies, ReaderModeVertical); err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" || gotPath != "/web/user_config/modify" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	if gotBody != `{"useHorizonReader":0}` || !strings.HasPrefix(gotType, "application/json") {
		t.Fatalf("body=%q type=%q", gotBody, gotType)
	}
	if !strings.Contains(gotCookie, "wr_skey=k") || !strings.HasPrefix(gotReferer, srv.URL) {
		t.Fatalf("cookie=%q referer=%q", gotCookie, gotReferer)
	}
}

func TestSetReaderModeErrors(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		body        string
		invalidUser bool
	}{
		{"session expired", 200, `{"errCode":-2012,"errMsg":"登录超时"}`, true},
		{"user not found", 200, `{"errCode":-2010}`, true},
		{"other error code", 200, `{"errCode":-1,"errMsg":"nope"}`, false},
		{"server ignored the change", 200, `{"useHorizonReader":"1"}`, false},
		{"http error", 403, `forbidden`, false},
		{"not json", 200, `<html>`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer srv.Close()

			err := newTestClient(t, srv.URL, nil).SetReaderMode(context.Background(), cookiesFor(t, "42", nil), ReaderModeVertical)
			if err == nil {
				t.Fatal("expected an error")
			}
			if got := errors.Is(err, ErrInvalidUser); got != tc.invalidUser {
				t.Fatalf("errors.Is(ErrInvalidUser) = %v, want %v (err: %v)", got, tc.invalidUser, err)
			}
		})
	}
}
