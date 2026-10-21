package version

import (
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
)

func buildInfo(mainVersion string, settings map[string]string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		bi := &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
		for k, v := range settings {
			bi.Settings = append(bi.Settings, debug.BuildSetting{Key: k, Value: v})
		}
		return bi, true
	}
}

func TestResolveKeepsInjectedValues(t *testing.T) {
	got := resolve(Info{Version: "v1.2.3", Commit: "abc", Date: "2026-01-02T03:04:05Z", BuiltBy: "goreleaser"},
		buildInfo("v9.9.9", map[string]string{"vcs.revision": "other", "vcs.time": "never"}))
	if got.Version != "v1.2.3" || got.Commit != "abc" || got.Date != "2026-01-02T03:04:05Z" || got.BuiltBy != "goreleaser" {
		t.Errorf("injected values must win: %+v", got)
	}
	if got.GoVersion != runtime.Version() || got.Platform != runtime.GOOS+"/"+runtime.GOARCH {
		t.Errorf("runtime info: %+v", got)
	}
}

func TestResolveFallsBackToBuildInfo(t *testing.T) {
	got := resolve(Info{Version: "dev"}, buildInfo("v0.4.0", map[string]string{
		"vcs.revision": "0123456789abcdef0123", "vcs.time": "2026-03-04T00:00:00Z", "vcs.modified": "true",
	}))
	if got.Version != "v0.4.0" || got.Commit != "0123456789abcdef0123" || got.Date != "2026-03-04T00:00:00Z" || !got.Modified {
		t.Errorf("fallback: %+v", got)
	}
}

func TestResolveDevelBuild(t *testing.T) {
	got := resolve(Info{Version: "dev"}, buildInfo("(devel)", nil))
	if got.Version != "dev" {
		t.Errorf("a (devel) module version must not replace %q, got %q", "dev", got.Version)
	}
	got = resolve(Info{Version: "dev"}, func() (*debug.BuildInfo, bool) { return nil, false })
	if got.Version != "dev" || got.Platform == "" {
		t.Errorf("no build info: %+v", got)
	}
}

func TestStringAndShort(t *testing.T) {
	info := Info{Version: "v1.2.3", Commit: "0123456789abcdef", Date: "today", BuiltBy: "ci", GoVersion: "go1.26", Platform: "linux/arm64", Modified: true}
	s := info.String()
	for _, want := range []string{"weread-exporter v1.2.3", "0123456789ab (modified)", "built:    today", "built by: ci", "go:       go1.26", "platform: linux/arm64"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "0123456789abc") {
		t.Errorf("the commit should be abbreviated to 12 characters:\n%s", s)
	}
	if info.Short() != "1.2.3" || (Info{Version: "dev"}).Short() != "dev" {
		t.Errorf("Short() = %q", info.Short())
	}
	if ua := info.UserAgent(); ua != "weread-exporter/1.2.3" {
		t.Errorf("UserAgent() = %q", ua)
	}
	if minimal := (Info{Version: "dev", GoVersion: "go1", Platform: "x/y"}).String(); strings.Contains(minimal, "commit") || strings.Contains(minimal, "built") {
		t.Errorf("empty fields should be omitted:\n%s", minimal)
	}
}

func TestGetReturnsSomething(t *testing.T) {
	if got := Get(); got.Version == "" || got.GoVersion == "" {
		t.Errorf("Get() = %+v", got)
	}
}
