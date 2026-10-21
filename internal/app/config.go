// Package app ties the pieces together: it resolves a book reference, captures
// the chapters in a browser, post-processes the captured Markdown and writes
// the requested output formats.
package app

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Output formats.
const (
	FormatEPUB     = "epub"
	FormatPDF      = "pdf"
	FormatMOBI     = "mobi"
	FormatTXT      = "txt"
	FormatMarkdown = "md"
)

// formatOrder is the order the formats are produced in: MOBI is converted from
// the EPUB, which therefore comes first.
var formatOrder = []string{FormatEPUB, FormatPDF, FormatMOBI, FormatTXT, FormatMarkdown}

// Formats lists the supported output formats.
func Formats() []string { return slices.Clone(formatOrder) }

// Defaults for Config.
const (
	DefaultCacheDir     = "cache"
	DefaultOutputDir    = "output"
	DefaultLoadTimeout  = 60 * time.Second
	DefaultLoadInterval = 30 * time.Second
)

// Config is everything an export run needs. The zero value is not valid: call
// Validate (New does) to apply the defaults.
type Config struct {
	// BookRef identifies what to export: a hashed or numeric book id, the URL of
	// a book or reader page, or a book list id (it contains "_").
	BookRef string
	// Formats are the output formats; empty means EPUB. MOBI implies EPUB.
	Formats []string
	// Chapters selects chapters to export, e.g. "1-3,7,10-"; empty means all.
	Chapters string

	// CacheDir holds the cookies and, per book, the captured chapters.
	CacheDir string
	// OutputDir receives the exported files.
	OutputDir string
	// CookieFile stores the login cookies; default <CacheDir>/cookie.txt.
	CookieFile string
	// CSSFile is appended to the built-in style sheet of EPUB and PDF.
	CSSFile string

	// Browser settings.
	ChromePath        string
	Headless          bool
	NoSandbox         bool
	UserDataDir       string
	UseDefaultProfile bool
	MockUserAgent     bool
	ProxyServer       string
	ForceLogin        bool
	LoadTimeout       time.Duration // limit for loading one chapter page
	LoadInterval      time.Duration // pause between two captured chapters

	// Overwrite regenerates output files that already exist.
	Overwrite bool
	// KeepReaderMode leaves the account's reader mode alone; the export then
	// fails if the account uses the paged reader.
	KeepReaderMode bool
	// MOBIConverter is the program that converts EPUB to MOBI; empty searches
	// for calibre's ebook-convert and kindlegen.
	MOBIConverter string
	// AssumeYes answers confirmation prompts with "yes".
	AssumeYes bool

	// BaseURL replaces https://weread.qq.com (tests point it at a fake site).
	BaseURL string
	// DebugBrowser forwards the page's console output to the log.
	DebugBrowser bool
	// Verbose is informational for the caller: it selects the log level.
	Verbose bool
}

// UsageError reports invalid input from the user (exit status 2).
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

func usagef(format string, args ...any) error {
	return &UsageError{Msg: fmt.Sprintf(format, args...)}
}

// applyDefaults fills in the unset fields.
func (c *Config) applyDefaults() {
	if c.CacheDir == "" {
		c.CacheDir = DefaultCacheDir
	}
	if c.OutputDir == "" {
		c.OutputDir = DefaultOutputDir
	}
	if c.CookieFile == "" {
		c.CookieFile = filepath.Join(c.CacheDir, "cookie.txt")
	}
	if c.LoadTimeout == 0 {
		c.LoadTimeout = DefaultLoadTimeout
	}
}

// NormalizeFormats lower-cases and de-duplicates the formats, applies the
// default and adds EPUB when MOBI is requested. The result follows the
// production order.
func NormalizeFormats(in []string) ([]string, error) {
	want := map[string]bool{}
	for _, f := range in {
		for _, name := range strings.Split(f, ",") {
			name = strings.ToLower(strings.TrimSpace(name))
			if name == "" {
				continue
			}
			if !slices.Contains(formatOrder, name) {
				return nil, usagef("unsupported output format %q (supported: %s)", name, strings.Join(formatOrder, ", "))
			}
			want[name] = true
		}
	}
	if len(want) == 0 {
		want[FormatEPUB] = true
	}
	if want[FormatMOBI] {
		want[FormatEPUB] = true
	}
	var out []string
	for _, f := range formatOrder {
		if want[f] {
			out = append(out, f)
		}
	}
	return out, nil
}

// Validate applies the defaults and checks the settings.
func (c *Config) Validate() error {
	c.applyDefaults()

	formats, err := NormalizeFormats(c.Formats)
	if err != nil {
		return err
	}
	c.Formats = formats

	if c.LoadTimeout < 0 {
		return usagef("--load-timeout must not be negative")
	}
	if c.LoadInterval < 0 {
		return usagef("--load-interval must not be negative")
	}
	if c.Chapters != "" {
		if _, err := ParseChapterSpec(c.Chapters); err != nil {
			return err
		}
	}
	if c.CSSFile != "" {
		if info, err := os.Stat(c.CSSFile); err != nil || info.IsDir() {
			return usagef("CSS file %s does not exist", c.CSSFile)
		}
	}
	if c.ProxyServer != "" {
		u, err := url.Parse(c.ProxyServer)
		if err != nil || u.Host == "" || u.Scheme == "" {
			return usagef("invalid proxy server %q: use a URL such as http://127.0.0.1:8888", c.ProxyServer)
		}
	}
	if c.UseDefaultProfile && c.UserDataDir != "" {
		return usagef("--use-default-profile and --user-data-dir cannot be combined")
	}
	return nil
}

// ChapterRange is a range of chapters, 1-based and inclusive. To == 0 means
// "up to the last chapter".
type ChapterRange struct{ From, To int }

// ChapterSpec is a parsed chapter selection such as "1-3,7,10-".
type ChapterSpec []ChapterRange

// ParseChapterSpec parses a comma separated list of chapter numbers ("7"),
// ranges ("2-5"), open ranges ("10-" and "-3"). Chapters are numbered from 1
// in the order of the book's table of contents (see `weread-exporter info`).
func ParseChapterSpec(s string) (ChapterSpec, error) {
	var spec ChapterSpec
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, usagef("invalid chapter selection %q: empty item", s)
		}
		from, to, isRange := strings.Cut(part, "-")
		rng := ChapterRange{}
		var err error
		switch {
		case !isRange:
			rng.From, err = parseChapterNumber(part)
			rng.To = rng.From
		case strings.TrimSpace(from) == "" && strings.TrimSpace(to) == "":
			return nil, usagef("invalid chapter selection %q: %q is not a range", s, part)
		case strings.TrimSpace(from) == "":
			rng.From = 1
			rng.To, err = parseChapterNumber(to)
		case strings.TrimSpace(to) == "":
			rng.From, err = parseChapterNumber(from)
		default:
			if rng.From, err = parseChapterNumber(from); err == nil {
				rng.To, err = parseChapterNumber(to)
			}
		}
		if err != nil {
			return nil, usagef("invalid chapter selection %q: %v", s, err)
		}
		if rng.To != 0 && rng.To < rng.From {
			return nil, usagef("invalid chapter selection %q: range %d-%d is backwards", s, rng.From, rng.To)
		}
		spec = append(spec, rng)
	}
	return spec, nil
}

func parseChapterNumber(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%q is not a chapter number (chapters start at 1)", s)
	}
	return n, nil
}

// Resolve returns the 0-based positions the selection covers in a book with
// total chapters, ascending and without duplicates. Ranges that run past the
// last chapter are cut off; a selection that starts after it is an error.
func (cs ChapterSpec) Resolve(total int) ([]int, error) {
	if total <= 0 {
		return nil, errors.New("the book has no chapters")
	}
	picked := make([]bool, total)
	for _, r := range cs {
		if r.From > total {
			return nil, usagef("chapter %d does not exist: the book has %d chapters", r.From, total)
		}
		to := r.To
		if to == 0 || to > total {
			to = total
		}
		for n := r.From; n <= to; n++ {
			picked[n-1] = true
		}
	}
	var out []int
	for i, ok := range picked {
		if ok {
			out = append(out, i)
		}
	}
	return out, nil
}

// String renders the selection in the syntax ParseChapterSpec reads.
func (cs ChapterSpec) String() string {
	parts := make([]string, len(cs))
	for i, r := range cs {
		switch r.To {
		case r.From:
			parts[i] = strconv.Itoa(r.From)
		case 0:
			parts[i] = strconv.Itoa(r.From) + "-"
		default:
			parts[i] = strconv.Itoa(r.From) + "-" + strconv.Itoa(r.To)
		}
	}
	return strings.Join(parts, ",")
}
