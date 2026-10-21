package browser

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// ThemeCookie and ThemeDark pin the site theme. The capture hook tells body text
// from highlights by its colour, and that colour is the dark theme's.
const (
	ThemeCookie = "wr_theme"
	ThemeDark   = "dark"
)

// InjectCookies installs the login cookies in the browser, as cookies of the
// site's host. The theme is pinned to dark and display preferences stored in
// cookies (see weread.FilterPersistent) are not copied.
func (s *Session) InjectCookies(ctx context.Context, cookies map[string]string) error {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()

	secure := strings.HasPrefix(s.baseURL, "https://")
	set := func(name, value string) error {
		_, err := chromedp.Call(c, network.SetCookie, network.SetCookieParams{
			Name:   name,
			Value:  value,
			URL:    s.baseURL,
			Secure: &secure,
		})
		if err != nil {
			return fmt.Errorf("browser: set cookie %s: %w", name, err)
		}
		return nil
	}

	persistent := weread.FilterPersistent(cookies)
	names := make([]string, 0, len(persistent))
	for name := range persistent {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if err := set(name, persistent[name]); err != nil {
			return err
		}
	}
	if err := set(ThemeCookie, ThemeDark); err != nil {
		return err
	}

	// Chrome silently drops cookies it considers invalid; say so.
	got, err := s.Cookies(ctx)
	if err != nil {
		return err
	}
	for _, name := range names {
		if _, ok := got[name]; !ok {
			s.log.Warn("the browser did not accept a cookie", "name", name)
		}
	}
	return nil
}

// Cookies returns the cookies the browser would send to the site.
func (s *Session) Cookies(ctx context.Context) (map[string]string, error) {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	res, err := chromedp.Call(c, network.GetCookies, network.GetCookiesParams{URLs: []string{s.baseURL}})
	if err != nil {
		return nil, fmt.Errorf("browser: read cookies: %w", err)
	}
	out := make(map[string]string, len(res.Cookies))
	for _, ck := range res.Cookies {
		out[ck.Name] = ck.Value
	}
	return out, nil
}

// ClearCookies deletes every cookie in the browser.
func (s *Session) ClearCookies(ctx context.Context) error {
	c, cancel := s.opTimeout(ctx, s.opts.StepTimeout)
	defer cancel()
	if _, err := chromedp.Call(c, network.ClearBrowserCookies, cdp.Empty{}); err != nil {
		return fmt.Errorf("browser: clear cookies: %w", err)
	}
	return nil
}
