package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestConfirm(t *testing.T) {
	tests := []struct {
		name   string
		yes    bool
		input  string
		answer bool
	}{
		{"assume yes", true, "", true},
		{"y", false, "y\n", true},
		{"Yes", false, "Yes\n", true},
		{"n", false, "n\n", false},
		{"empty line", false, "\n", false},
		{"end of input", false, "", false},
		{"nonsense", false, "maybe\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.cfg.AssumeYes = tt.yes
			e := h.exporter()
			e.in = strings.NewReader(tt.input)
			got, err := e.confirm(context.Background(), "Continue?")
			if err != nil || got != tt.answer {
				t.Fatalf("confirm = %v, %v; want %v", got, err, tt.answer)
			}
		})
	}
}

func TestConfirmGivesUpWhenCancelled(t *testing.T) {
	h := newHarness(t)
	e := h.exporter()
	pr, _ := newBlockingReader()
	e.in = pr
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.confirm(ctx, "Continue?"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestConfirmHeadlessExplainsTheRisk(t *testing.T) {
	h := newHarness(t)
	h.cfg.AssumeYes = true
	e := h.exporter()
	ok, err := e.confirmHeadless(context.Background(), []string{"navigator.webdriver is set"})
	if err != nil || !ok {
		t.Fatalf("ok = %v, err = %v", ok, err)
	}
	mustContain(t, "log", h.logs(), "navigator.webdriver is set")
}

func TestSleepContext(t *testing.T) {
	if err := sleepContext(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := sleepContext(context.Background(), 20*time.Millisecond); err != nil || time.Since(start) < 15*time.Millisecond {
		t.Fatalf("slept %v, %v", time.Since(start), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if err := sleepContext(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled context must win even without a delay: %v", err)
	}
}

func TestDetachedOutlivesItsParent(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	cancel()
	ctx, stop := detached(parent, time.Minute)
	defer stop()
	if ctx.Err() != nil {
		t.Fatal("cleanup after Ctrl-C must still be able to talk to the network")
	}
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("but not forever")
	}
}

func TestBatchError(t *testing.T) {
	boom := errors.New("boom")
	err := &BatchError{Total: 3, Failed: map[string]error{"b": boom, "a": ErrBookUnavailable}}
	msg := err.Error()
	if !strings.HasPrefix(msg, "2 of 3 books failed:") || strings.Index(msg, "a:") > strings.Index(msg, "b:") {
		t.Fatalf("message = %q (books must be listed in a stable order)", msg)
	}
	if !errors.Is(err, boom) || !errors.Is(err, ErrBookUnavailable) {
		t.Fatal("Unwrap must expose the individual errors")
	}
}

func TestBrowserOptionsFollowTheConfiguration(t *testing.T) {
	h := newHarness(t)
	h.cfg.Headless = true
	h.cfg.NoSandbox = true
	h.cfg.ChromePath = "/opt/chrome"
	h.cfg.ProxyServer = "http://127.0.0.1:8888"
	h.cfg.MockUserAgent = true
	h.cfg.DebugBrowser = true
	opts := h.exporter().browserOptions()
	if !opts.Headless || !opts.NoSandbox || opts.ChromePath != "/opt/chrome" || opts.ProxyServer != "http://127.0.0.1:8888" ||
		!opts.MockUserAgent || !opts.DebugConsole || opts.BaseURL != h.site.URL() {
		t.Fatalf("options = %+v", opts)
	}
	if opts.ConfirmHeadless == nil || opts.DiagnosticsDir == "" {
		t.Fatalf("options = %+v", opts)
	}
}
