package browser

import (
	"net/url"
	"strings"
)

// While a chapter is captured the page must not report reading activity back to
// WeRead: the telemetry endpoints are answered locally. The rules mirror the
// original tool:
//
//   - /hera/* (usage logs): preflight requests succeed, logkv/osslog/chlog are
//     acknowledged, anything else is blocked;
//   - /sentry/* (error reports) and /river/single* (reading statistics) are
//     acknowledged;
//   - everything else is left alone.

// interceptKind says what to do with a paused request.
type interceptKind int

const (
	interceptContinue interceptKind = iota // let the request through unchanged
	interceptFulfill                       // answer it locally
	interceptBlock                         // fail it
)

type header struct{ Name, Value string }

// interceptDecision is the outcome of matching a request against the rules.
type interceptDecision struct {
	Kind    interceptKind
	Status  int
	Headers []header
	Body    string
}

// interceptPatterns are the URL patterns subscribed to through the Fetch domain.
// Requests that match none of them are never paused.
var interceptPatterns = []string{"*/hera/*", "*/sentry/*", "*/river/single*"}

var corsHeaders = []header{
	{"Access-Control-Allow-Origin", "*"},
	{"Access-Control-Request-Method", "*"},
	{"Access-Control-Allow-Headers", "*"},
}

func withCORS(extra ...header) []header {
	return append(append([]header(nil), corsHeaders...), extra...)
}

// decideRequest applies the rules to a request.
func decideRequest(method, rawURL string) interceptDecision {
	path := rawURL
	if u, err := url.Parse(rawURL); err == nil {
		path = u.Path
	}

	switch {
	case strings.Contains(path, "/hera/"):
		switch {
		case method == "OPTIONS":
			return interceptDecision{Kind: interceptFulfill, Status: 200, Headers: withCORS()}
		case strings.Contains(path, "/hera/logkv") || strings.Contains(path, "/hera/osslog"):
			return interceptDecision{Kind: interceptFulfill, Status: 204, Headers: withCORS()}
		case strings.Contains(rawURL, "chlog"):
			return interceptDecision{Kind: interceptFulfill, Status: 200, Headers: withCORS()}
		}
		return interceptDecision{Kind: interceptBlock}
	case strings.Contains(path, "/sentry/"):
		return interceptDecision{Kind: interceptFulfill, Status: 200}
	case strings.Contains(path, "/river/single"):
		return interceptDecision{
			Kind:    interceptFulfill,
			Status:  200,
			Headers: withCORS(header{"Content-Type", "application/json"}),
			Body:    `{"err_code":0,"msg":"suc"}`,
		}
	}
	return interceptDecision{Kind: interceptContinue}
}
