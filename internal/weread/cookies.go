package weread

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jtsang4/weread-exporter-go/internal/fsutil"
)

// Cookies is the set of WeRead cookies of the logged-in user. It is safe for
// concurrent use and persisted as a flat JSON object {"name": "value"}, the
// same format the original Python tool used (cache/cookie.txt).
type Cookies struct {
	mu     sync.RWMutex
	values map[string]string
	path   string
}

// NewCookies returns an empty cookie set that saves to path ("" disables saving).
func NewCookies(path string) *Cookies {
	return &Cookies{values: map[string]string{}, path: path}
}

// LoadCookies reads path. A missing file yields an empty set. Both the JSON
// object format and a raw "a=b; c=d" Cookie header are accepted.
func LoadCookies(path string) (*Cookies, error) {
	c := NewCookies(path)
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, fmt.Errorf("weread: read cookie file: %w", err)
	}
	c.values = ParseCookieText(string(data))
	return c, nil
}

// ParseCookieText parses a cookie file body: JSON object first, then a
// Cookie-header style "name=value; name2=value2" list.
func ParseCookieText(text string) map[string]string {
	out := map[string]string{}
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return out
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(trimmed), &obj); err == nil {
		for k, v := range obj {
			switch t := v.(type) {
			case string:
				out[k] = t
			case float64, bool:
				out[k] = fmt.Sprint(t)
			}
		}
		return out
	}
	for _, part := range strings.Split(trimmed, ";") {
		part = strings.TrimSpace(part)
		name, value, ok := strings.Cut(part, "=")
		if !ok || name == "" {
			continue
		}
		out[name] = value
	}
	return out
}

// Get returns the value of one cookie.
func (c *Cookies) Get(name string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.values[name]
}

// Set stores one cookie (in memory only; call Save to persist).
func (c *Cookies) Set(name, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[name] = value
}

// Replace swaps the whole cookie set.
func (c *Cookies) Replace(values map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = make(map[string]string, len(values))
	for k, v := range values {
		c.values[k] = v
	}
}

// Clear drops every cookie.
func (c *Cookies) Clear() { c.Replace(nil) }

// Len reports how many cookies are stored.
func (c *Cookies) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.values)
}

// Map returns a copy of the cookies.
func (c *Cookies) Map() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]string, len(c.values))
	for k, v := range c.values {
		out[k] = v
	}
	return out
}

// Names returns the sorted cookie names (safe to log: no values).
func (c *Cookies) Names() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.values))
	for k := range c.values {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Header renders the cookies as a Cookie request header value.
func (c *Cookies) Header() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.values))
	for k := range c.values {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, k := range names {
		parts = append(parts, k+"="+c.values[k])
	}
	return strings.Join(parts, "; ")
}

// VID returns the numeric user id cookie (wr_vid).
func (c *Cookies) VID() string { return c.Get("wr_vid") }

// MergeSetCookie applies the Set-Cookie headers of a response and returns the
// names that were updated. Invalid entries are skipped.
func (c *Cookies) MergeSetCookie(h http.Header) []string {
	var updated []string
	for _, line := range h.Values("Set-Cookie") {
		first, _, _ := strings.Cut(line, ";")
		name, value, ok := strings.Cut(strings.TrimSpace(first), "=")
		if !ok || name == "" {
			continue
		}
		c.Set(name, value)
		updated = append(updated, name)
	}
	return updated
}

// Save writes the cookies atomically with mode 0600 (they are credentials).
func (c *Cookies) Save() error {
	if c.path == "" {
		return nil
	}
	data, err := json.Marshal(c.Map())
	if err != nil {
		return err
	}
	data = append(bytes.TrimSpace(data), '\n')
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("weread: create cookie dir: %w", err)
	}
	if err := fsutil.WriteFileAtomic(c.path, data, 0o600); err != nil {
		return fmt.Errorf("weread: save cookies: %w", err)
	}
	return nil
}

// Path returns the file the cookies are persisted to.
func (c *Cookies) Path() string { return c.path }

// ephemeralCookies are set by the site UI at runtime and must not be persisted:
// the exporter pins the theme itself and the reader-mode cookie has no effect
// any more (the mode is an account setting now, see ReaderMode).
var ephemeralCookies = map[string]struct{}{
	"wr_theme":            {},
	"wr_useHorizonReader": {},
}

// FilterPersistent removes cookies that should not be written to disk.
func FilterPersistent(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		if _, skip := ephemeralCookies[k]; skip {
			continue
		}
		out[k] = v
	}
	return out
}
