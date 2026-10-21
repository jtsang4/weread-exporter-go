package app

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestParseChapterSpec(t *testing.T) {
	tests := []struct {
		in    string
		total int
		want  []int // 0-based positions
	}{
		{"1", 5, []int{0}},
		{"3", 5, []int{2}},
		{"2-4", 5, []int{1, 2, 3}},
		{"4-", 6, []int{3, 4, 5}},
		{"-2", 6, []int{0, 1}},
		{"1,3,5", 6, []int{0, 2, 4}},
		{"1-2,2-3", 6, []int{0, 1, 2}},       // overlapping ranges
		{"5,1", 6, []int{0, 4}},              // order of the items does not matter
		{" 2 - 3 , 6 ", 6, []int{1, 2, 5}},   // blanks
		{"3-100", 5, []int{2, 3, 4}},         // past the end is cut off
		{"1-", 1, []int{0}},                  // single-chapter book
		{"4,4,4", 6, []int{3}},               // duplicates
		{"10-12,1", 12, []int{0, 9, 10, 11}}, // multi-digit
		{"6-", 6, []int{5}},                  // starts at the last chapter
		{"1-1", 3, []int{0}},                 // degenerate range
		{"1-2,4-5,7-", 9, []int{0, 1, 3, 4, 6, 7, 8}},
	}
	for _, tt := range tests {
		spec, err := ParseChapterSpec(tt.in)
		if err != nil {
			t.Errorf("ParseChapterSpec(%q): %v", tt.in, err)
			continue
		}
		got, err := spec.Resolve(tt.total)
		if err != nil {
			t.Errorf("%q.Resolve(%d): %v", tt.in, tt.total, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%q over %d chapters = %v, want %v", tt.in, tt.total, got, tt.want)
		}
	}
}

func TestParseChapterSpecErrors(t *testing.T) {
	for _, in := range []string{"", ",", "1,,2", "0", "-0", "a", "1-b", "3-1", "-", "--", "1-2-3", "1.5", "-5-", " "} {
		_, err := ParseChapterSpec(in)
		var usage *UsageError
		if !errors.As(err, &usage) {
			t.Errorf("ParseChapterSpec(%q) = %v, want a usage error", in, err)
		}
	}
}

func TestChapterSpecResolveErrors(t *testing.T) {
	spec, _ := ParseChapterSpec("7")
	var usage *UsageError
	if _, err := spec.Resolve(5); !errors.As(err, &usage) {
		t.Errorf("a chapter after the last one: %v", err)
	}
	if _, err := spec.Resolve(0); err == nil {
		t.Error("a book without chapters")
	}
}

func TestChapterSpecString(t *testing.T) {
	for in, want := range map[string]string{
		"1":         "1",
		"2-4":       "2-4",
		"4-":        "4-",
		"-3":        "1-3",
		"1, 3 ,5-7": "1,3,5-7",
	} {
		spec, err := ParseChapterSpec(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := spec.String(); got != want {
			t.Errorf("String(%q) = %q, want %q", in, got, want)
		}
		again, err := ParseChapterSpec(spec.String())
		if err != nil || again.String() != spec.String() {
			t.Errorf("%q does not survive a round trip: %v, %v", in, again, err)
		}
	}
}

func TestNormalizeFormats(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{nil, []string{"epub"}},
		{[]string{}, []string{"epub"}},
		{[]string{"pdf"}, []string{"pdf"}},
		{[]string{"PDF", "Epub"}, []string{"epub", "pdf"}},
		{[]string{"mobi"}, []string{"epub", "mobi"}}, // MOBI is converted from the EPUB
		{[]string{"md", "txt", "epub"}, []string{"epub", "txt", "md"}},
		{[]string{"epub,pdf"}, []string{"epub", "pdf"}},
		{[]string{"txt", "txt"}, []string{"txt"}},
		{[]string{" md ", ""}, []string{"md"}},
		{[]string{"mobi", "pdf", "txt", "md", "epub"}, []string{"epub", "pdf", "mobi", "txt", "md"}},
	}
	for _, tt := range tests {
		got, err := NormalizeFormats(tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("NormalizeFormats(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	_, err := NormalizeFormats([]string{"epub", "docx"})
	var usage *UsageError
	if !errors.As(err, &usage) || !strings.Contains(err.Error(), "docx") {
		t.Errorf("an unknown format: %v", err)
	}
}

func TestFormats(t *testing.T) {
	got := Formats()
	if !reflect.DeepEqual(got, []string{"epub", "pdf", "mobi", "txt", "md"}) {
		t.Fatalf("Formats() = %v", got)
	}
	got[0] = "changed"
	if Formats()[0] != "epub" {
		t.Fatal("Formats() must return a copy")
	}
}

func TestConfigDefaults(t *testing.T) {
	cfg := Config{BookRef: "1234567"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg.CacheDir != "cache" || cfg.OutputDir != "output" || cfg.LoadTimeout != time.Minute {
		t.Fatalf("defaults = %+v", cfg)
	}
	if cfg.CookieFile != filepath.Join("cache", "cookie.txt") {
		t.Fatalf("cookie file = %q", cfg.CookieFile)
	}
	if !reflect.DeepEqual(cfg.Formats, []string{"epub"}) {
		t.Fatalf("formats = %v", cfg.Formats)
	}

	cfg = Config{CacheDir: "/data/c", CookieFile: "/etc/cookie"}
	_ = cfg.Validate()
	if cfg.CookieFile != "/etc/cookie" {
		t.Fatalf("an explicit cookie file must win: %q", cfg.CookieFile)
	}
	cfg = Config{CacheDir: filepath.Join("data", "c")}
	_ = cfg.Validate()
	if cfg.CookieFile != filepath.Join("data", "c", "cookie.txt") {
		t.Fatalf("the cookie file follows the cache directory: %q", cfg.CookieFile)
	}
}

func TestConfigValidate(t *testing.T) {
	css := filepath.Join(t.TempDir(), "x.css")
	if err := os.WriteFile(css, []byte("p{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := map[string]Config{
		"negative timeout":  {LoadTimeout: -time.Second},
		"negative interval": {LoadInterval: -time.Second},
		"bad format":        {Formats: []string{"rtf"}},
		"bad chapters":      {Chapters: "x"},
		"missing css":       {CSSFile: filepath.Join(t.TempDir(), "none.css")},
		"css is a dir":      {CSSFile: t.TempDir()},
		"proxy w/o scheme":  {ProxyServer: "127.0.0.1:8888"},
		"proxy w/o host":    {ProxyServer: "http://"},
		"profile conflict":  {UseDefaultProfile: true, UserDataDir: "/tmp/x"},
	}
	for name, cfg := range bad {
		err := cfg.Validate()
		var usage *UsageError
		if !errors.As(err, &usage) {
			t.Errorf("%s: err = %v, want a usage error", name, err)
		}
	}
	good := Config{CSSFile: css, ProxyServer: "socks5://127.0.0.1:1080", Chapters: "1-3", Formats: []string{"epub", "pdf"}}
	if err := good.Validate(); err != nil {
		t.Errorf("a valid configuration was rejected: %v", err)
	}
}

func TestNewReturnsUsageErrors(t *testing.T) {
	_, err := New(Config{Formats: []string{"rtf"}}, nil)
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewReadsTheSavedCookies(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cookie.txt"), []byte("wr_vid=12; wr_skey=abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	e, err := New(Config{CacheDir: dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if e.cookies.VID() != "12" {
		t.Fatalf("vid = %q", e.cookies.VID())
	}
	if e.Config().OutputDir != "output" {
		t.Fatalf("Config() = %+v", e.Config())
	}
}
