package browser

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeFinder(goos string, env map[string]string, onPath map[string]string, files ...string) chromeFinder {
	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}
	return chromeFinder{
		goos:   goos,
		getenv: func(k string) string { return env[k] },
		lookPath: func(name string) (string, error) {
			if p, ok := onPath[name]; ok {
				return p, nil
			}
			return "", errors.New("not found")
		},
		isFile: func(p string) bool { return have[p] },
	}
}

func TestFindChromeExplicit(t *testing.T) {
	f := fakeFinder("linux", nil, map[string]string{"my-chrome": "/opt/bin/my-chrome"}, "/custom/chrome")

	if got, err := f.find("my-chrome"); err != nil || got != "/opt/bin/my-chrome" {
		t.Errorf("name on PATH: %q, %v", got, err)
	}
	if got, err := f.find("/custom/chrome"); err != nil || got != "/custom/chrome" {
		t.Errorf("explicit file: %q, %v", got, err)
	}
	_, err := f.find("/nope/chrome")
	var nf *ChromeNotFoundError
	if !errors.As(err, &nf) || nf.Path != "/nope/chrome" || !strings.Contains(err.Error(), "/nope/chrome") {
		t.Errorf("missing explicit path: %v", err)
	}
}

func TestFindChromeEnvironment(t *testing.T) {
	f := fakeFinder("linux", map[string]string{ChromeEnv: "/env/chrome"}, nil, "/env/chrome", "/usr/bin/google-chrome")
	got, err := f.find("")
	if err != nil || got != "/env/chrome" {
		t.Fatalf("$%s should win over the automatic search: %q, %v", ChromeEnv, got, err)
	}
	// An explicit argument beats the environment.
	f = fakeFinder("linux", map[string]string{ChromeEnv: "/env/chrome"}, nil, "/env/chrome", "/arg/chrome")
	if got, _ := f.find("/arg/chrome"); got != "/arg/chrome" {
		t.Fatalf("explicit argument = %q", got)
	}
}

func TestFindChromeSearch(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		host   string // the OS the case can run on ("" = any)
		env    map[string]string
		onPath map[string]string
		files  []string
		want   string
	}{
		{"linux PATH", "linux", "", nil, map[string]string{"chromium": "/usr/local/bin/chromium"}, nil, "/usr/local/bin/chromium"},
		{"linux prefers google-chrome", "linux", "", nil,
			map[string]string{"chromium": "/c", "google-chrome": "/g"}, nil, "/g"},
		{"linux fixed location", "linux", "", nil, nil, []string{"/snap/bin/chromium"}, "/snap/bin/chromium"},
		// The app bundles below are joined with the host's separator, so they
		// only find their file on macOS.
		{"mac app bundle", "darwin", "darwin", map[string]string{"HOME": "/Users/me"}, nil,
			[]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"},
		{"mac user applications", "darwin", "darwin", map[string]string{"HOME": "/Users/me"}, nil,
			[]string{"/Users/me/Applications/Chromium.app/Contents/MacOS/Chromium"},
			"/Users/me/Applications/Chromium.app/Contents/MacOS/Chromium"},
		{"mac falls back to Edge", "darwin", "darwin", map[string]string{"HOME": "/Users/me"}, nil,
			[]string{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"},
		{"windows program files", "windows", "", map[string]string{"ProgramFiles": `C:\Program Files`}, nil,
			[]string{filepath.Join(`C:\Program Files`, "Google", "Chrome", "Application", "chrome.exe")},
			filepath.Join(`C:\Program Files`, "Google", "Chrome", "Application", "chrome.exe")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.host != "" && tc.host != runtime.GOOS {
				t.Skipf("the %s candidates are built for the %s host", tc.goos, tc.host)
			}
			got, err := fakeFinder(tc.goos, tc.env, tc.onPath, tc.files...).find("")
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestFindChromeNothingInstalled(t *testing.T) {
	_, err := fakeFinder("linux", nil, nil).find("")
	var nf *ChromeNotFoundError
	if !errors.As(err, &nf) || nf.Path != "" || len(nf.Tried) == 0 {
		t.Fatalf("err = %v", err)
	}
	msg := err.Error()
	for _, want := range []string{"--chrome-path", ChromeEnv, "google-chrome"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q lacks %q", msg, want)
		}
	}
}

func TestFindChromeUsesTheRealFileSystem(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "fake-chrome")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindChrome(exe)
	if err != nil || got != exe {
		t.Fatalf("FindChrome(%q) = %q, %v", exe, got, err)
	}
	if _, err := FindChrome(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("a missing explicit path must be an error")
	}
}

func TestParseMajorVersion(t *testing.T) {
	tests := map[string]int{
		"Google Chrome 155.0.8059.86 \n":             155,
		"Chromium 120.0.6099.109 snap\n":             120,
		"Microsoft Edge 118.0.2088.46 \n":            118,
		"Google Chrome for Testing 136.0.7103.49 \n": 136,
	}
	for in, want := range tests {
		if got, err := parseMajorVersion(in); err != nil || got != want {
			t.Errorf("parseMajorVersion(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "no digits here", "version 12"} {
		if _, err := parseMajorVersion(bad); err == nil {
			t.Errorf("parseMajorVersion(%q) should fail", bad)
		}
	}
}

func TestMajorVersionFromInstallDir(t *testing.T) {
	tests := map[string]int{
		// filepath.Join keeps the case portable: the separators follow the host.
		filepath.Join(`C:\Program Files`, "Google", "Chrome", "Application", "155.0.8059.86", "chrome.exe"): 155,
		filepath.Join("/opt/google/chrome", "120.0.6099.109", "chrome"):                                     120,
	}
	for path, want := range tests {
		if got, ok := majorVersionFromInstallDir(path); !ok || got != want {
			t.Errorf("majorVersionFromInstallDir(%q) = %d, %v; want %d", path, got, ok, want)
		}
	}
	for _, path := range []string{
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"chrome.exe",
		"",
	} {
		if got, ok := majorVersionFromInstallDir(path); ok {
			t.Errorf("majorVersionFromInstallDir(%q) = %d, should have failed", path, got)
		}
	}
}

func TestDefaultProfileDir(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		goos string
		env  map[string]string
		want string
	}{
		{"darwin", map[string]string{"HOME": "/Users/me"}, filepath.Join("/Users/me", "Library", "Application Support", "Google", "Chrome")},
		{"linux", map[string]string{"HOME": "/home/me"}, filepath.Join("/home/me", ".config", "google-chrome")},
		{"linux", map[string]string{"HOME": "/home/me", "XDG_CONFIG_HOME": "/xdg"}, filepath.Join("/xdg", "google-chrome")},
		{"windows", map[string]string{"LOCALAPPDATA": `C:\L`}, filepath.Join(`C:\L`, "Google", "Chrome", "User Data")},
		{"linux", nil, ""},
	}
	for _, tc := range tests {
		if got := defaultProfileDir(tc.goos, env(tc.env)); got != tc.want {
			t.Errorf("defaultProfileDir(%s, %v) = %q, want %q", tc.goos, tc.env, got, tc.want)
		}
	}
}
