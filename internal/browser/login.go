package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrLoginNeedsWindow is returned when a login is needed in a headless browser:
// the QR code has to be scanned from a visible window.
var ErrLoginNeedsWindow = errors.New("browser: signing in needs a visible browser window; " +
	"run `weread-exporter login` once (without --headless) to save a login")

// AvatarState says what the page shows in the avatar slot of the nav bar.
type AvatarState string

const (
	AvatarNone    AvatarState = "none"    // no avatar at all (anonymous visitor, or not rendered yet)
	AvatarDefault AvatarState = "default" // the placeholder avatar (login not finished)
	AvatarUser    AvatarState = "user"    // the signed-in user's own avatar
)

const avatarStateExpr = `(() => {
  const img = document.querySelector('img.wr_avatar_img');
  if (!img) return "none";
  return (img.getAttribute('src') || '').endsWith('Default.svg') ? "default" : "user";
})()`

// loginButtonSelectors are the login buttons of the site's pages (home pages
// and the reader).
var loginButtonSelectors = []string{
	"button.navBar_link_Login",
	"div.readerTopBar_right button.actionItem",
}

// loggedInExpr is true once the nav bar shows the user's avatar menu.
const loggedInExpr = `!!document.querySelector('div.menu_container img.wr_avatar_img')`

// AvatarState reports what the current page shows as the user's avatar.
func (s *Session) AvatarState(ctx context.Context) (AvatarState, error) {
	state, err := s.evalString(ctx, avatarStateExpr)
	return AvatarState(state), err
}

// WaitForAvatar waits until the page shows the signed-in user's avatar and
// reports whether it did within timeout.
func (s *Session) WaitForAvatar(ctx context.Context, timeout time.Duration) (bool, error) {
	return s.waitFor(ctx, `(() => {
  const img = document.querySelector('img.wr_avatar_img');
  return !!img && !(img.getAttribute('src') || '').endsWith('Default.svg');
})()`, timeout, 500*time.Millisecond)
}

// Login signs in through the site's QR-code login and returns the browser's
// cookies afterwards. A page of the site must be open. The user scans the code
// in the browser window; Login waits up to Options.LoginTimeout for that.
func (s *Session) Login(ctx context.Context) (map[string]string, error) {
	if s.opts.Headless && !s.opts.allowHeadlessLogin {
		return nil, ErrLoginNeedsWindow
	}

	clicked := false
	for _, selector := range loginButtonSelectors {
		quoted, err := json.Marshal(selector)
		if err != nil {
			return nil, err
		}
		text, err := s.evalString(ctx, fmt.Sprintf(`(() => { const e = document.querySelector(%s); return e ? e.innerText : ""; })()`, quoted))
		if err != nil {
			return nil, fmt.Errorf("browser: look for the login button: %w", err)
		}
		if !strings.Contains(text, "登录") {
			continue
		}
		if err := s.click(ctx, selector); err != nil {
			return nil, fmt.Errorf("browser: click the login button: %w", err)
		}
		clicked = true
		break
	}
	if !clicked {
		return nil, ErrNoLoginButton
	}

	s.log.Info("waiting for you to sign in: scan the QR code in the browser window with WeChat",
		"timeout", s.opts.LoginTimeout.String())
	ok, err := s.waitFor(ctx, loggedInExpr, s.opts.LoginTimeout, 2*time.Second)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLoginTimeout
	}
	s.log.Info("signed in")
	// Let the site finish setting its session cookies.
	if err := sleep(ctx, time.Second); err != nil {
		return nil, err
	}
	return s.Cookies(ctx)
}
