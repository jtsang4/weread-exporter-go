package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/pflag"

	"github.com/jtsang4/weread-exporter-go/internal/app"
)

// durationValue is a flag value for durations. Like the original tool it reads
// a bare number as seconds ("--load-interval 30"); it also takes Go durations
// ("500ms", "2m").
type durationValue struct{ d *time.Duration }

func newDurationValue(p *time.Duration, def time.Duration) *durationValue {
	*p = def
	return &durationValue{d: p}
}

func (v *durationValue) Set(s string) error {
	s = strings.TrimSpace(s)
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		if n < 0 {
			return fmt.Errorf("%q must not be negative", s)
		}
		*v.d = time.Duration(n * float64(time.Second))
		return nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("%q is neither a number of seconds nor a duration such as 90s or 2m", s)
	}
	if d < 0 {
		return fmt.Errorf("%q must not be negative", s)
	}
	*v.d = d
	return nil
}

func (v *durationValue) String() string {
	if v == nil || v.d == nil {
		return "0s"
	}
	if *v.d%time.Second == 0 {
		return strconv.FormatInt(int64(*v.d/time.Second), 10) + "s"
	}
	return v.d.String()
}

func (v *durationValue) Type() string { return "duration" }

// commonFlags are understood by every command.
type commonFlags struct {
	cacheDir    string
	cookieFile  string
	proxyServer string
	baseURL     string
	verbose     bool
	logFormat   string
}

func (f *commonFlags) register(fs *pflag.FlagSet) {
	fs.StringVar(&f.cacheDir, "cache-dir", app.DefaultCacheDir, "directory for the login cookies and the captured chapters of every book")
	fs.StringVar(&f.cookieFile, "cookie-file", "", "file that stores the login cookies (default <cache-dir>/cookie.txt)")
	fs.StringVar(&f.proxyServer, "proxy-server", "", "proxy for the browser and the HTTP requests, e.g. http://127.0.0.1:8888")
	fs.BoolVarP(&f.verbose, "verbose", "v", false, "log debugging details")
	fs.StringVar(&f.logFormat, "log-format", logText, "format of the log on the standard error: text or json")
	fs.StringVar(&f.baseURL, "base-url", "", "")
	_ = fs.MarkHidden("base-url") // for tests that run against a fake site
}

// browserFlags configure Chrome; the commands that open it understand them.
type browserFlags struct {
	headless          bool
	noSandbox         bool
	chromePath        string
	userDataDir       string
	useDefaultProfile bool
	mockUserAgent     bool
	forceLogin        bool
	debugBrowser      bool
	assumeYes         bool
	loadTimeout       time.Duration
}

func (f *browserFlags) register(fs *pflag.FlagSet) {
	fs.BoolVar(&f.headless, "headless", false, "run Chrome without a window (needs a login saved earlier with the login command)")
	fs.BoolVar(&f.noSandbox, "no-sandbox", false, "start Chrome without its sandbox (needed in some containers and when running as root)")
	fs.StringVar(&f.chromePath, "chrome-path", "", "Chrome, Chromium or Edge executable (default: search for one; $CHROME_PATH works too)")
	fs.StringVar(&f.userDataDir, "user-data-dir", "", "keep Chrome's profile (cookies, login) in this directory between runs")
	fs.BoolVar(&f.useDefaultProfile, "use-default-profile", false, "use your everyday Chrome profile (close Chrome first; not possible with Chrome 136 and later)")
	fs.BoolVar(&f.mockUserAgent, "mock-user-agent", false, "present a regular desktop Chrome user agent instead of the real one")
	fs.BoolVar(&f.forceLogin, "force-login", false, "ignore the saved login and sign in again")
	fs.BoolVar(&f.debugBrowser, "debug-browser", false, "forward the console output of the web pages to the log")
	fs.BoolVarP(&f.assumeYes, "yes", "y", false, "answer questions with yes (for example to carry on with a browser that looks automated)")
	fs.Var(newDurationValue(&f.loadTimeout, app.DefaultLoadTimeout), "load-timeout", "how long one chapter page may take to load, in seconds or as a duration")
}

// exportFlags select what is exported and where it goes.
type exportFlags struct {
	bookID         string
	formats        []string
	chapters       string
	outputDir      string
	cssFile        string
	loadInterval   time.Duration
	overwrite      bool
	keepReaderMode bool
	mobiConverter  string
}

// registerBookFlag adds -b/--book-id, shared by the export and info commands.
func registerBookFlag(fs *pflag.FlagSet, dst *string, verb string) {
	fs.StringVarP(dst, "book-id", "b", "", "book to "+verb+": hashed id, numeric id, book list id (contains \"_\") or a book or reader URL")
}

func (f *exportFlags) register(fs *pflag.FlagSet) {
	registerBookFlag(fs, &f.bookID, "export")
	fs.StringSliceVarP(&f.formats, "output-format", "o", nil, "output format, repeat it for several: epub, pdf, mobi, txt, md (default epub)")
	fs.StringVar(&f.chapters, "chapters", "", "export only these chapters, e.g. 1-3,7,10- (numbers as listed by the info command)")
	fs.StringVar(&f.outputDir, "output-dir", app.DefaultOutputDir, "directory for the exported files")
	fs.StringVar(&f.cssFile, "css-file", "", "CSS file appended to the built-in style sheet of EPUB and PDF")
	fs.Var(newDurationValue(&f.loadInterval, app.DefaultLoadInterval), "load-interval", "pause between two chapters captured from the site, in seconds or as a duration")
	fs.BoolVar(&f.overwrite, "overwrite", false, "replace output files that exist already")
	fs.BoolVar(&f.keepReaderMode, "keep-reader-mode", false, "never switch the account's reader mode (fails if the account uses the paged reader)")
	fs.StringVar(&f.mobiConverter, "mobi-converter", "", "program that converts EPUB to MOBI (default: calibre's ebook-convert, then kindlegen)")
}

// config assembles the exporter configuration; the flag groups that a command
// does not have are nil.
func buildConfig(c *commonFlags, b *browserFlags, x *exportFlags) app.Config {
	cfg := app.Config{
		CacheDir:    c.cacheDir,
		CookieFile:  c.cookieFile,
		ProxyServer: c.proxyServer,
		BaseURL:     c.baseURL,
		Verbose:     c.verbose,
	}
	if b != nil {
		cfg.Headless = b.headless
		cfg.NoSandbox = b.noSandbox
		cfg.ChromePath = b.chromePath
		cfg.UserDataDir = b.userDataDir
		cfg.UseDefaultProfile = b.useDefaultProfile
		cfg.MockUserAgent = b.mockUserAgent
		cfg.ForceLogin = b.forceLogin
		cfg.DebugBrowser = b.debugBrowser
		cfg.AssumeYes = b.assumeYes
		cfg.LoadTimeout = b.loadTimeout
	}
	if x != nil {
		cfg.BookRef = x.bookID
		cfg.Formats = x.formats
		cfg.Chapters = x.chapters
		cfg.OutputDir = x.outputDir
		cfg.CSSFile = x.cssFile
		cfg.LoadInterval = x.loadInterval
		cfg.Overwrite = x.overwrite
		cfg.KeepReaderMode = x.keepReaderMode
		cfg.MOBIConverter = x.mobiConverter
	}
	return cfg
}
