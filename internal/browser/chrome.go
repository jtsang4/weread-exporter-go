// Package browser drives Chrome through the DevTools protocol: it starts a
// browser, logs in, hooks the reader page and captures chapters as Markdown, and
// prints HTML to PDF.
package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ChromeEnv is the environment variable that points to the browser executable
// (the --chrome-path flag takes precedence).
const ChromeEnv = "CHROME_PATH"

// ChromeNotFoundError is returned when no Chrome/Chromium/Edge executable can be located.
type ChromeNotFoundError struct {
	// Path is the explicitly requested path ("" when the search was automatic).
	Path string
	// Tried lists the names and locations that were checked.
	Tried []string
}

func (e *ChromeNotFoundError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("browser: Chrome executable %q does not exist or is not executable", e.Path)
	}
	return "browser: Google Chrome (or Chromium / Microsoft Edge) was not found; install it, " +
		"put it on PATH, or pass --chrome-path / set " + ChromeEnv +
		" (looked for: " + strings.Join(e.Tried, ", ") + ")"
}

// chromeFinder abstracts the file system so the search can be tested on any OS.
type chromeFinder struct {
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	isFile   func(string) bool
}

func systemFinder() chromeFinder {
	return chromeFinder{
		goos:     runtime.GOOS,
		getenv:   os.Getenv,
		lookPath: exec.LookPath,
		isFile: func(p string) bool {
			st, err := os.Stat(p)
			return err == nil && !st.IsDir()
		},
	}
}

// FindChrome returns the browser executable to use: explicit (when not empty),
// then $CHROME_PATH, then the usual names on PATH and installation directories.
func FindChrome(explicit string) (string, error) {
	return systemFinder().find(explicit)
}

func (f chromeFinder) find(explicit string) (string, error) {
	if explicit == "" {
		explicit = f.getenv(ChromeEnv)
	}
	if explicit != "" {
		if p, err := f.lookPath(explicit); err == nil {
			return p, nil
		}
		if f.isFile(explicit) {
			return explicit, nil
		}
		return "", &ChromeNotFoundError{Path: explicit}
	}

	names, paths := f.candidates()
	tried := make([]string, 0, len(names)+len(paths))
	for _, name := range names {
		tried = append(tried, name)
		if p, err := f.lookPath(name); err == nil {
			return p, nil
		}
	}
	for _, p := range paths {
		if p == "" {
			continue
		}
		tried = append(tried, p)
		if f.isFile(p) {
			return p, nil
		}
	}
	return "", &ChromeNotFoundError{Tried: tried}
}

// candidates returns executable names to look up on PATH and absolute install locations.
func (f chromeFinder) candidates() (names, paths []string) {
	switch f.goos {
	case "darwin":
		home := f.getenv("HOME")
		apps := []string{
			"Google Chrome.app/Contents/MacOS/Google Chrome",
			"Chromium.app/Contents/MacOS/Chromium",
			"Google Chrome Beta.app/Contents/MacOS/Google Chrome Beta",
			"Google Chrome Canary.app/Contents/MacOS/Google Chrome Canary",
			"Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
		names = []string{"google-chrome", "google-chrome-stable", "chromium", "chrome"}
		dirs := []string{"/Applications"}
		if home != "" {
			dirs = append(dirs, filepath.Join(home, "Applications"))
		}
		for _, dir := range dirs {
			for _, app := range apps {
				paths = append(paths, filepath.Join(dir, app))
			}
		}
	case "windows":
		names = []string{"chrome.exe", "chrome", "msedge.exe", "msedge"}
		for _, root := range []string{
			f.getenv("ProgramFiles"), f.getenv("ProgramFiles(x86)"), f.getenv("LOCALAPPDATA"),
		} {
			if root == "" {
				continue
			}
			paths = append(paths,
				filepath.Join(root, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(root, "Chromium", "Application", "chrome.exe"),
				filepath.Join(root, "Microsoft", "Edge", "Application", "msedge.exe"),
			)
		}
	default:
		names = []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome",
			"microsoft-edge", "microsoft-edge-stable",
		}
		paths = []string{
			"/usr/bin/google-chrome", "/opt/google/chrome/chrome", "/snap/bin/chromium",
			"/usr/bin/chromium", "/usr/bin/chromium-browser", "/usr/bin/microsoft-edge",
		}
	}
	return names, paths
}

var versionPattern = regexp.MustCompile(`(\d+)\.\d+\.\d+\.\d+`)

// ChromeMajorVersion returns the major version of the browser at path (for
// example 155), by running "<path> --version". While another Chrome is running,
// Windows prints "Opening in existing browser session." instead, so the
// versioned directory next to the executable is the fallback.
func ChromeMajorVersion(ctx context.Context, path string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, runErr := exec.CommandContext(ctx, path, "--version").Output()
	v, parseErr := parseMajorVersion(string(out))
	if parseErr == nil {
		return v, nil
	}
	if dirV, ok := majorVersionFromInstallDir(path); ok {
		return dirV, nil
	}
	if runErr != nil {
		return 0, fmt.Errorf("browser: run %s --version: %w", path, runErr)
	}
	return 0, parseErr
}

// majorVersionFromInstallDir reads the major version from the versioned
// directory Chrome installs next to its executable. On Linux the executable
// sits inside it (".../120.0.6099.109/chrome"); on Windows chrome.exe is one
// level above it (".../Application/chrome.exe" and ".../Application/155.0.8059.86/").
func majorVersionFromInstallDir(path string) (int, bool) {
	dir := filepath.Dir(path)
	if v, ok := majorVersionOfName(filepath.Base(dir)); ok {
		return v, true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, false
	}
	best := 0
	for _, e := range entries {
		if v, ok := majorVersionOfName(e.Name()); ok && v > best {
			best = v
		}
	}
	return best, best > 0
}

// majorVersionOfName returns the major version in a name such as "155.0.8059.86".
func majorVersionOfName(name string) (int, bool) {
	m := versionPattern.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	v, err := strconv.Atoi(m[1])
	return v, err == nil
}

func parseMajorVersion(output string) (int, error) {
	m := versionPattern.FindStringSubmatch(output)
	if m == nil {
		return 0, errors.New("browser: cannot find a version number in " + strconv.Quote(strings.TrimSpace(output)))
	}
	return strconv.Atoi(m[1])
}

// defaultProfileDir returns the platform's default Chrome user data directory
// ("" when it cannot be determined).
func defaultProfileDir(goos string, getenv func(string) string) string {
	switch goos {
	case "darwin":
		if home := getenv("HOME"); home != "" {
			return filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
		}
	case "windows":
		if local := getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "Google", "Chrome", "User Data")
		}
	default:
		if cfg := getenv("XDG_CONFIG_HOME"); cfg != "" {
			return filepath.Join(cfg, "google-chrome")
		}
		if home := getenv("HOME"); home != "" {
			return filepath.Join(home, ".config", "google-chrome")
		}
	}
	return ""
}
