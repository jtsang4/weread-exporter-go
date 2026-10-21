package weread

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

var quietLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// sleepRecorder replaces real backoff sleeps in tests.
type sleepRecorder struct{ delays []time.Duration }

func (s *sleepRecorder) sleep(_ context.Context, d time.Duration) error {
	s.delays = append(s.delays, d)
	return nil
}

func newTestClient(t *testing.T, baseURL string, rec *sleepRecorder) *Client {
	t.Helper()
	opts := Options{BaseURL: baseURL, Logger: quietLogger}
	if rec != nil {
		opts.Sleep = rec.sleep
	}
	c, err := NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientRetriesServerErrorsWithBackoff(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, "boom", http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	rec := &sleepRecorder{}
	resp, err := newTestClient(t, srv.URL, rec).Get(context.Background(), srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(resp.Body) != "ok" || calls.Load() != 3 {
		t.Fatalf("body=%q calls=%d", resp.Body, calls.Load())
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; !reflect.DeepEqual(rec.delays, want) {
		t.Fatalf("backoff = %v, want %v", rec.delays, want)
	}
}

func TestClientRetriesTooManyRequests(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	if _, err := newTestClient(t, srv.URL, &sleepRecorder{}).Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d", calls.Load())
	}
}

func TestClientGivesUpAfterAllAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(t, srv.URL, &sleepRecorder{}).Get(context.Background(), srv.URL, nil)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("err = %v", err)
	}
	if calls.Load() != 3 {
		t.Fatalf("calls = %d, want 3", calls.Load())
	}
}

func TestClientDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL, &sleepRecorder{})
	resp, err := c.Do(context.Background(), Request{URL: srv.URL})
	if err != nil || resp.Status != http.StatusNotFound || resp.OK() {
		t.Fatalf("Do: resp=%+v err=%v", resp, err)
	}
	if _, err := c.Get(context.Background(), srv.URL, nil); err == nil {
		t.Fatal("Get must fail on 404")
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2 (no retries)", calls.Load())
	}
}

func TestClientRetriesTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more

	rec := &sleepRecorder{}
	_, err := newTestClient(t, url, rec).Do(context.Background(), Request{URL: url})
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if len(rec.delays) != 2 {
		t.Fatalf("delays = %v", rec.delays)
	}
}

func TestClientStopsWhenContextIsCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	c, err := NewClient(Options{BaseURL: srv.URL, Logger: quietLogger, Sleep: func(ctx context.Context, d time.Duration) error {
		cancel()
		return ctx.Err()
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Do(ctx, Request{URL: srv.URL}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestClientSendsHeadersAndBody(t *testing.T) {
	var gotMethod, gotUA, gotCookie, gotType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotUA, gotCookie, gotType = r.Method, r.UserAgent(), r.Header.Get("Cookie"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()

	c, err := NewClient(Options{BaseURL: srv.URL, UserAgent: "test-agent/1.0", Logger: quietLogger})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Do(context.Background(), Request{
		Method: http.MethodPost, URL: srv.URL,
		Header: map[string]string{"Cookie": "a=b", "Content-Type": "application/json"},
		Body:   []byte(`{"x":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotMethod != "POST" || gotUA != "test-agent/1.0" || gotCookie != "a=b" || gotType != "application/json" || gotBody != `{"x":1}` {
		t.Fatalf("method=%q ua=%q cookie=%q type=%q body=%q", gotMethod, gotUA, gotCookie, gotType, gotBody)
	}
}

func TestClientDefaultsAndURLHelpers(t *testing.T) {
	c, err := NewClient(Options{Logger: quietLogger})
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL() != RootURL {
		t.Fatalf("BaseURL = %q", c.BaseURL())
	}
	if got := c.BookDetailURL("abc"); got != "https://weread.qq.com/web/bookDetail/abc" {
		t.Fatal(got)
	}
	if got := c.ReaderURL("abc", 0); got != "https://weread.qq.com/web/reader/abc" {
		t.Fatal(got)
	}
	if got := c.ReaderURL("abc", 41); got != "https://weread.qq.com/web/reader/abck34132fc02293416a75f431d" {
		t.Fatal(got)
	}
	if got := c.BookListURL("1_x"); got != "https://weread.qq.com/misc/booklist/1_x" {
		t.Fatal(got)
	}
}

func TestNewClientRejectsBadProxy(t *testing.T) {
	for _, p := range []string{"://nope", "no-host"} {
		if _, err := NewClient(Options{ProxyURL: p}); err == nil {
			t.Errorf("proxy %q accepted", p)
		}
	}
	if _, err := NewClient(Options{ProxyURL: "socks5://127.0.0.1:1080"}); err != nil {
		t.Fatalf("valid proxy rejected: %v", err)
	}
}
