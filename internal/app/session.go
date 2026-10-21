package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jtsang4/weread-exporter-go/internal/browser"
	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

const (
	// revalidateEvery is how often the login is checked (and renewed) during a
	// long export: the session key lasts about an hour.
	revalidateEvery = 20 * time.Minute
	// profileAvatarWait is how long the page gets to show the avatar of a
	// browser profile that is signed in already.
	profileAvatarWait = 3 * time.Second
)

// loginHint is appended to errors that need the user to sign in.
const loginHint = "run `weread-exporter login` to sign in with the QR code"

// checkLogin validates the saved login. It returns nil without an error when
// there is no usable login (the cookies are then dropped from memory); an error
// means the check itself failed (network). A session key that has expired is
// renewed on the way and the renewed cookies are saved.
func (e *Exporter) checkLogin(ctx context.Context, quiet bool) (*weread.UserInfo, error) {
	if e.cookies.VID() == "" {
		return nil, nil
	}
	info, err := e.api.UserInfo(ctx, e.cookies)
	switch {
	case err == nil:
		e.lastValidated = e.now()
		if quiet {
			e.log.Debug("the login is still valid", "user", info.Name)
		} else {
			e.log.Info("signed in", "user", info.Name)
		}
		return info, nil
	case errors.Is(err, weread.ErrInvalidUser):
		e.log.Warn("the saved login is no longer valid", "reason", err)
		e.cookies.Clear()
		return nil, nil
	default:
		return nil, err
	}
}

// hasPersistentProfile reports whether the browser keeps its state between runs.
func (e *Exporter) hasPersistentProfile() bool {
	return e.cfg.UserDataDir != "" || e.cfg.UseDefaultProfile
}

// startSession launches the browser, signs in (with the saved cookies, the
// browser profile or the QR code) and makes sure the account uses the vertical
// reader. The caller closes the session.
func (e *Exporter) startSession(ctx context.Context, bookID string) (browserSession, error) {
	signedIn := false
	if e.cfg.ForceLogin && !e.forcedLogin {
		e.log.Info("--force-login: ignoring the saved login")
		e.cookies.Clear()
	} else {
		info, err := e.checkLogin(ctx, false)
		if err != nil {
			return nil, fmt.Errorf("check the saved login: %w", err)
		}
		signedIn = info != nil
	}
	if !signedIn && e.cfg.Headless && !e.hasPersistentProfile() {
		return nil, fmt.Errorf("%w: there is no valid saved login and a headless browser cannot show the QR code; %s first (or run without --headless)",
			ErrLoginNeeded, loginHint)
	}

	if signedIn && e.cfg.KeepReaderMode {
		// Nothing may be switched, so a paged account is a failure that needs no browser.
		if err := e.ensureVertical(ctx, bookID); err != nil {
			return nil, err
		}
	}

	sess, err := e.launch(ctx, e.browserOptions())
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			_ = sess.Close()
		}
	}()

	if signedIn {
		if err := sess.InjectCookies(ctx, e.cookies.Map()); err != nil {
			return nil, err
		}
	}
	if err := sess.Navigate(ctx, e.api.BookDetailURL(bookID), e.cfg.LoadTimeout); err != nil {
		return nil, err
	}

	if signedIn {
		shown, err := sess.WaitForAvatar(ctx, 30*time.Second)
		if err != nil {
			return nil, err
		}
		if !shown {
			e.log.Warn("the page does not show the signed-in avatar although the login is valid; carrying on")
		}
	} else if err := e.signInWithBrowser(ctx, sess); err != nil {
		return nil, err
	}
	e.forcedLogin = true

	if err := e.ensureVertical(ctx, bookID); err != nil {
		return nil, err
	}
	ok = true
	return sess, nil
}

// signInWithBrowser gets a login out of the open browser: a profile that is
// signed in already hands over its cookies, otherwise the user scans the QR code.
func (e *Exporter) signInWithBrowser(ctx context.Context, sess browserSession) error {
	if e.hasPersistentProfile() {
		shown, err := sess.WaitForAvatar(ctx, profileAvatarWait)
		if err != nil {
			return err
		}
		if shown {
			return e.adoptBrowserLogin(ctx, sess)
		}
	}
	cookies, err := sess.Login(ctx)
	switch {
	case errors.Is(err, browser.ErrLoginNeedsWindow):
		return fmt.Errorf("%w: a headless browser cannot show the QR code; %s first (or run without --headless)", ErrLoginNeeded, loginHint)
	case errors.Is(err, browser.ErrNoLoginButton):
		return e.adoptBrowserLogin(ctx, sess)
	case err != nil:
		return err
	}
	return e.saveBrowserLogin(ctx, cookies)
}

// adoptBrowserLogin takes over the cookies of a browser that is signed in already.
func (e *Exporter) adoptBrowserLogin(ctx context.Context, sess browserSession) error {
	cookies, err := sess.Cookies(ctx)
	if err != nil {
		return err
	}
	if cookies["wr_vid"] == "" {
		return errors.New("the page shows no login button but the browser holds no WeRead login either")
	}
	e.log.Info("using the login of the browser profile")
	return e.saveBrowserLogin(ctx, cookies)
}

// saveBrowserLogin stores the cookies a browser login produced.
func (e *Exporter) saveBrowserLogin(ctx context.Context, cookies map[string]string) error {
	e.cookies.Replace(weread.FilterPersistent(cookies))
	if err := e.cookies.Save(); err != nil {
		return err
	}
	e.log.Info("login saved", "file", e.cookies.Path(), "cookies", e.cookies.Names())
	info, err := e.checkLogin(ctx, false)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("%w: the cookies of the new login were rejected", ErrLoginNeeded)
	}
	return nil
}

// refreshSession validates the session before the exporter changes account
// settings, renewing it if it expired meanwhile.
func (e *Exporter) refreshSession(ctx context.Context) error {
	info, err := e.checkLogin(ctx, true)
	if err != nil {
		return err
	}
	if info == nil {
		return fmt.Errorf("%w: the session expired and could not be renewed; %s", ErrLoginNeeded, loginHint)
	}
	return nil
}

// keepSessionFresh renews the login during long exports and hands renewed
// cookies to the browser.
func (e *Exporter) keepSessionFresh(ctx context.Context, sess browserSession) error {
	if e.now().Sub(e.lastValidated) < revalidateEvery {
		return nil
	}
	before := e.cookies.Header()
	info, err := e.checkLogin(ctx, true)
	switch {
	case err != nil:
		e.log.Warn("could not check the login; carrying on", "error", err)
		return nil
	case info == nil:
		return e.loginInBrowserAgain(ctx, sess)
	}
	if e.cookies.Header() != before {
		e.log.Info("the session was renewed")
		return sess.InjectCookies(ctx, e.cookies.Map())
	}
	return nil
}

// handleLoginRequired reacts to a reader page that asks for a login. A session
// that merely expired is renewed; otherwise the user signs in again.
func (e *Exporter) handleLoginRequired(ctx context.Context, sess browserSession) error {
	info, err := e.checkLogin(ctx, false)
	if err != nil {
		return fmt.Errorf("check the login: %w", err)
	}
	if info != nil {
		e.log.Info("the reader asked for a login although the session is valid; handing the cookies to the browser again")
		return sess.InjectCookies(ctx, e.cookies.Map())
	}
	return e.loginInBrowserAgain(ctx, sess)
}

// loginInBrowserAgain signs in through the open browser after the session was lost.
func (e *Exporter) loginInBrowserAgain(ctx context.Context, sess browserSession) error {
	e.log.Warn("the login was lost")
	if e.cfg.Headless && !e.hasPersistentProfile() {
		return fmt.Errorf("%w: the session expired and could not be renewed, and a headless browser cannot show the QR code; %s first",
			ErrLoginNeeded, loginHint)
	}
	return e.signInWithBrowser(ctx, sess)
}
