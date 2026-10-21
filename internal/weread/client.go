package weread

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// RootURL is the WeRead web site.
	RootURL = "https://weread.qq.com"

	// DefaultUserAgent is sent with plain HTTP requests when none is configured.
	DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/130.0.0.0 Safari/537.36"

	defaultAttempts = 3
	maxBodyBytes    = 256 << 20 // refuse to buffer anything larger than 256 MiB
)

// Options configures a Client.
type Options struct {
	// BaseURL overrides the site root (tests point it at a fake server).
	BaseURL string
	// ProxyURL routes requests through a proxy (http://, https:// or socks5://).
	// Empty means "use HTTP(S)_PROXY from the environment, if any".
	ProxyURL string
	// UserAgent defaults to DefaultUserAgent.
	UserAgent string
	// Timeout bounds a single request; defaults to 60s.
	Timeout time.Duration
	// Attempts is the number of tries per request; defaults to 3.
	Attempts int
	// Logger receives debug/warn messages; defaults to slog.Default().
	Logger *slog.Logger
	// HTTPClient replaces the default transport entirely (tests).
	HTTPClient *http.Client
	// Sleep replaces time.Sleep-based backoff (tests).
	Sleep func(ctx context.Context, d time.Duration) error
}

// Client is a small retrying HTTP client for the WeRead web site.
type Client struct {
	http      *http.Client
	baseURL   string
	userAgent string
	attempts  int
	log       *slog.Logger
	sleep     func(ctx context.Context, d time.Duration) error
}

// NewClient builds a Client.
func NewClient(opts Options) (*Client, error) {
	c := &Client{
		baseURL:   strings.TrimRight(opts.BaseURL, "/"),
		userAgent: opts.UserAgent,
		attempts:  opts.Attempts,
		log:       opts.Logger,
		sleep:     opts.Sleep,
	}
	if c.baseURL == "" {
		c.baseURL = RootURL
	}
	if c.userAgent == "" {
		c.userAgent = DefaultUserAgent
	}
	if c.attempts <= 0 {
		c.attempts = defaultAttempts
	}
	if c.log == nil {
		c.log = slog.Default()
	}
	if c.sleep == nil {
		c.sleep = sleepContext
	}

	if opts.HTTPClient != nil {
		c.http = opts.HTTPClient
		return c, nil
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return nil, errors.New("weread: http.DefaultTransport is not an *http.Transport")
	}
	transport := base.Clone()
	if opts.ProxyURL != "" {
		proxy, err := url.Parse(opts.ProxyURL)
		if err != nil || proxy.Host == "" {
			return nil, fmt.Errorf("weread: invalid proxy url %q", opts.ProxyURL)
		}
		transport.Proxy = http.ProxyURL(proxy)
	}
	c.http = &http.Client{Timeout: timeout, Transport: transport}
	return c, nil
}

// BaseURL returns the site root used for requests.
func (c *Client) BaseURL() string { return c.baseURL }

// Response is a fully buffered HTTP response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// OK reports whether the status is 2xx.
func (r *Response) OK() bool { return r.Status >= 200 && r.Status < 300 }

// StatusError is returned for unexpected HTTP statuses.
type StatusError struct {
	URL    string
	Status int
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("weread: GET %s returned HTTP %d", e.URL, e.Status)
}

// Request describes one HTTP request.
type Request struct {
	Method string
	URL    string
	Header map[string]string
	Body   []byte
}

// Do performs the request, retrying on transport errors, HTTP 429 and 5xx with
// a short exponential backoff. Other statuses (including 4xx) are returned to
// the caller without error: whether they are acceptable depends on the endpoint.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	var lastErr error
	for attempt := 1; attempt <= c.attempts; attempt++ {
		if attempt > 1 {
			delay := time.Duration(1<<(attempt-2)) * time.Second
			c.log.Debug("retrying request", "url", req.URL, "attempt", attempt, "after", delay)
			if err := c.sleep(ctx, delay); err != nil {
				return nil, err
			}
		}

		resp, err := c.once(ctx, method, req)
		switch {
		case err == nil && resp.Status != http.StatusTooManyRequests && resp.Status < 500:
			return resp, nil
		case err == nil:
			lastErr = &StatusError{URL: req.URL, Status: resp.Status}
			if attempt == c.attempts {
				return resp, nil
			}
		default:
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			lastErr = err
		}
		c.log.Warn("request failed", "method", method, "url", req.URL, "attempt", attempt, "error", lastErr)
	}
	return nil, fmt.Errorf("weread: %s %s failed after %d attempts: %w", method, req.URL, c.attempts, lastErr)
}

func (c *Client) once(ctx context.Context, method string, req Request) (*Response, error) {
	var body io.Reader
	if req.Body != nil {
		body = bytes.NewReader(req.Body)
	}
	hreq, err := http.NewRequestWithContext(ctx, method, req.URL, body)
	if err != nil {
		return nil, err
	}
	hreq.Header.Set("User-Agent", c.userAgent)
	for k, v := range req.Header {
		hreq.Header.Set(k, v)
	}
	resp, err := c.http.Do(hreq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBodyBytes {
		return nil, errors.New("response body too large")
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: data}, nil
}

// Get fetches target and requires a 2xx status.
func (c *Client) Get(ctx context.Context, target string, header map[string]string) (*Response, error) {
	resp, err := c.Do(ctx, Request{URL: target, Header: header})
	if err != nil {
		return nil, err
	}
	if !resp.OK() {
		return nil, &StatusError{URL: target, Status: resp.Status}
	}
	return resp, nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
