package browser

import (
	"strings"
	"testing"
)

func TestDecideRequest(t *testing.T) {
	tests := []struct {
		name         string
		method, url  string
		kind         interceptKind
		status       int
		cors         bool
		bodyContains string
	}{
		{"hera preflight", "OPTIONS", "https://oss.weread.qq.com/hera/chlog", interceptFulfill, 200, true, ""},
		{"hera preflight any path", "OPTIONS", "https://weread.qq.com/i/hera/whatever", interceptFulfill, 200, true, ""},
		{"logkv", "GET", "https://oss.weread.qq.com/hera/logkv?x=1", interceptFulfill, 204, true, ""},
		{"osslog", "POST", "https://weread.qq.com/hera/osslog", interceptFulfill, 204, true, ""},
		{"chlog post", "POST", "https://weread.qq.com/i/hera/chlog", interceptFulfill, 200, true, ""},
		{"chlog in query", "POST", "https://weread.qq.com/hera/other?target=chlog", interceptFulfill, 200, true, ""},
		{"unknown hera is blocked", "POST", "https://weread.qq.com/hera/report", interceptBlock, 0, false, ""},
		{"sentry", "POST", "https://weread.qq.com/sentry/api/1/envelope/", interceptFulfill, 200, false, ""},
		{"river single", "POST", "https://weread.qq.com/river/single", interceptFulfill, 200, true, `"err_code":0`},
		{"river single suffix", "POST", "https://weread.qq.com/river/single/report", interceptFulfill, 200, true, `"msg":"suc"`},
		{"chapter api passes", "POST", "https://weread.qq.com/web/book/chapter/e_0", interceptContinue, 0, false, ""},
		{"reader document passes", "GET", "https://weread.qq.com/web/reader/abc", interceptContinue, 0, false, ""},
		{"hera only in query passes", "GET", "https://weread.qq.com/web/other?next=/hera/x", interceptContinue, 0, false, ""},
		{"garbage url", "GET", "::not a url::", interceptContinue, 0, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := decideRequest(tc.method, tc.url)
			if d.Kind != tc.kind {
				t.Fatalf("kind = %v, want %v", d.Kind, tc.kind)
			}
			if d.Status != tc.status {
				t.Errorf("status = %d, want %d", d.Status, tc.status)
			}
			hasCORS := false
			for _, h := range d.Headers {
				if h.Name == "Access-Control-Allow-Origin" && h.Value == "*" {
					hasCORS = true
				}
			}
			if hasCORS != tc.cors {
				t.Errorf("CORS headers = %v, want %v (%v)", hasCORS, tc.cors, d.Headers)
			}
			if tc.bodyContains != "" && !strings.Contains(d.Body, tc.bodyContains) {
				t.Errorf("body %q does not contain %q", d.Body, tc.bodyContains)
			}
		})
	}
}

func TestInterceptPatternsCoverTheRules(t *testing.T) {
	// Every URL that decideRequest handles specially must be subscribed to,
	// otherwise the request would never be paused.
	covered := map[string]bool{}
	for _, p := range interceptPatterns {
		covered[p] = true
	}
	for _, want := range []string{"*/hera/*", "*/sentry/*", "*/river/single*"} {
		if !covered[want] {
			t.Errorf("pattern %q missing from interceptPatterns", want)
		}
	}
}
