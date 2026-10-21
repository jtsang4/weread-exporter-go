package browser

import (
	"fmt"
	"regexp"
	"testing"
)

func flagMap(o Options, goos, profile string) map[string]any {
	m := map[string]any{}
	for _, f := range chromeFlags(o.withDefaults(), goos, profile) {
		m[f.Name] = f.Value
	}
	return m
}

func TestChromeFlagsDefaults(t *testing.T) {
	f := flagMap(Options{}, "darwin", "")
	for name, want := range map[string]any{
		"no-first-run":                           true,
		"no-default-browser-check":               true,
		"disable-background-timer-throttling":    true,
		"disable-renderer-backgrounding":         true,
		"disable-backgrounding-occluded-windows": true,
		"window-size":                            "1920,1080",
	} {
		if f[name] != want {
			t.Errorf("flag %s = %v, want %v", name, f[name], want)
		}
	}
	for _, absent := range []string{"headless", "no-sandbox", "user-data-dir", "proxy-server", "user-agent", "disable-dev-shm-usage", "enable-automation"} {
		if _, ok := f[absent]; ok {
			t.Errorf("flag %s must not be set by default", absent)
		}
	}
}

func TestChromeFlagsOptions(t *testing.T) {
	f := flagMap(Options{
		Headless: true, NoSandbox: true, ProxyServer: "http://127.0.0.1:8888",
		UserAgent: "MyAgent/1.0", MockUserAgent: true, WindowWidth: 800, WindowHeight: 600,
	}, "linux", "/profile")
	want := map[string]any{
		"headless":              "new",
		"no-sandbox":            true,
		"proxy-server":          "http://127.0.0.1:8888",
		"user-agent":            "MyAgent/1.0", // an explicit agent beats the random one
		"window-size":           "800,600",
		"user-data-dir":         "/profile",
		"disable-dev-shm-usage": true,
	}
	for name, v := range want {
		if f[name] != v {
			t.Errorf("flag %s = %v, want %v", name, f[name], v)
		}
	}
}

func TestChromeFlagsMockUserAgent(t *testing.T) {
	re := regexp.MustCompile(`^Mozilla/5\.0 \(Windows NT 10\.0; Win64; x64\) AppleWebKit/537\.36 \(KHTML, like Gecko\) Chrome/(\d+)\.0\.0\.0 Safari/537\.36$`)
	for range 50 {
		ua, _ := flagMap(Options{MockUserAgent: true}, "linux", "")["user-agent"].(string)
		m := re.FindStringSubmatch(ua)
		if m == nil {
			t.Fatalf("unexpected mock user agent %q", ua)
		}
		var major int
		if _, err := fmt.Sscan(m[1], &major); err != nil || major < 90 || major > 130 {
			t.Fatalf("mock user agent %q has major version %d, want 90..130", ua, major)
		}
	}
}

func TestChromeFlagsDefaultProfileKeepsWindowAlone(t *testing.T) {
	f := flagMap(Options{UseDefaultProfile: true}, "darwin", "/default/profile")
	if _, ok := f["window-size"]; ok {
		t.Error("the default profile restores its own window size")
	}
	if f["user-data-dir"] != "/default/profile" {
		t.Errorf("user-data-dir = %v", f["user-data-dir"])
	}
}

func TestCheckDisplay(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if err := checkDisplay("linux", env(nil)); err != ErrNoDisplay {
		t.Errorf("linux without display: %v", err)
	}
	for _, e := range []map[string]string{{"DISPLAY": ":0"}, {"WAYLAND_DISPLAY": "wayland-0"}} {
		if err := checkDisplay("linux", env(e)); err != nil {
			t.Errorf("linux with %v: %v", e, err)
		}
	}
	for _, goos := range []string{"darwin", "windows"} {
		if err := checkDisplay(goos, env(nil)); err != nil {
			t.Errorf("%s needs no DISPLAY: %v", goos, err)
		}
	}
}

func TestOptionsDefaults(t *testing.T) {
	o := Options{BaseURL: "http://example.test/"}.withDefaults()
	if o.BaseURL != "http://example.test" {
		t.Errorf("BaseURL = %q", o.BaseURL)
	}
	if o.Logger == nil || o.WindowWidth != 1920 || o.WindowHeight != 1080 {
		t.Errorf("defaults not applied: %+v", o)
	}
	if o.FooterTimeout.Seconds() != 60 || o.StepTimeout.Seconds() != 30 || o.LoginTimeout.Minutes() != 5 {
		t.Errorf("timeouts: footer=%v step=%v login=%v", o.FooterTimeout, o.StepTimeout, o.LoginTimeout)
	}
	if def := (Options{}).withDefaults(); def.BaseURL != "https://weread.qq.com" {
		t.Errorf("default BaseURL = %q", def.BaseURL)
	}
}

func TestRetryable(t *testing.T) {
	for _, err := range []error{ErrLoginRequired, ErrHorizontalReader, ErrHeadlessRefused, &ChapterUnavailableError{Footer: "x"},
		fmt.Errorf("wrapped: %w", ErrLoginRequired)} {
		if Retryable(err) {
			t.Errorf("%v must not be retryable", err)
		}
	}
	for _, err := range []error{ErrEmptyCapture, &StepTimeoutError{Step: "x"}, &UnexpectedPageError{URL: "u"}, fmt.Errorf("boom")} {
		if !Retryable(err) {
			t.Errorf("%v should be retryable", err)
		}
	}
	if Retryable(nil) {
		t.Error("nil is not retryable")
	}
}
