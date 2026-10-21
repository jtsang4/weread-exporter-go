// Package version holds the build information of the binary.
//
// Release builds get the values injected by the linker (see .goreleaser.yaml and
// the Makefile):
//
//	-X github.com/jtsang4/weread-exporter-go/internal/version.Version=v1.2.3
//
// Builds made with plain `go build` or `go install module@version` fall back to
// the information the Go toolchain embeds in the binary.
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
)

// Set by the linker; the defaults describe a development build.
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
	BuiltBy = ""
)

// Info is the resolved build information.
type Info struct {
	Version   string
	Commit    string
	Date      string
	BuiltBy   string
	GoVersion string
	Platform  string // GOOS/GOARCH
	Modified  bool   // the working tree had uncommitted changes
}

// Get resolves the build information.
func Get() Info {
	return resolve(Info{Version: Version, Commit: Commit, Date: Date, BuiltBy: BuiltBy}, readBuildInfo)
}

func readBuildInfo() (*debug.BuildInfo, bool) { return debug.ReadBuildInfo() }

func resolve(in Info, read func() (*debug.BuildInfo, bool)) Info {
	in.GoVersion = runtime.Version()
	in.Platform = runtime.GOOS + "/" + runtime.GOARCH

	bi, ok := read()
	if !ok {
		return in
	}
	if in.Version == "" || in.Version == "dev" {
		// `go install module@v1.2.3` records the module version; a local build says "(devel)".
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			in.Version = v
		}
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if in.Commit == "" {
				in.Commit = s.Value
			}
		case "vcs.time":
			if in.Date == "" {
				in.Date = s.Value
			}
		case "vcs.modified":
			in.Modified = s.Value == "true"
		}
	}
	return in
}

// Short is the version number alone ("1.2.3", without the leading "v").
func (i Info) Short() string { return strings.TrimPrefix(i.Version, "v") }

// String is the multi-line text printed by `weread-exporter version`.
func (i Info) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "weread-exporter %s\n", i.Version)
	if i.Commit != "" {
		commit := i.Commit
		if len(commit) > 12 {
			commit = commit[:12]
		}
		if i.Modified {
			commit += " (modified)"
		}
		fmt.Fprintf(&b, "  commit:   %s\n", commit)
	}
	if i.Date != "" {
		fmt.Fprintf(&b, "  built:    %s\n", i.Date)
	}
	if i.BuiltBy != "" {
		fmt.Fprintf(&b, "  built by: %s\n", i.BuiltBy)
	}
	fmt.Fprintf(&b, "  go:       %s\n", i.GoVersion)
	fmt.Fprintf(&b, "  platform: %s\n", i.Platform)
	return b.String()
}

// UserAgent identifies the tool in plain HTTP requests made on behalf of the
// user, if a site ever asks.
func (i Info) UserAgent() string { return "weread-exporter/" + i.Short() }
