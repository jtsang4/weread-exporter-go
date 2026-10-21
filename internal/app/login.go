package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/jtsang4/weread-exporter-go/internal/weread"
)

// LoginResult tells what the login command found or did.
type LoginResult struct {
	User *weread.UserInfo
	// AlreadySignedIn is true when the saved login was still valid and no
	// browser was opened.
	AlreadySignedIn bool
	// CookieFile is where the login is stored.
	CookieFile string
}

// Login makes sure there is a valid saved login. A login that works is kept
// (unless Config.ForceLogin is set); otherwise a browser window opens and the
// user scans the QR code with the WeChat app.
func (e *Exporter) Login(ctx context.Context) (*LoginResult, error) {
	if e.cfg.ForceLogin {
		e.log.Info("--force-login: ignoring the saved login")
		e.cookies.Clear()
	} else {
		info, err := e.checkLogin(ctx, false)
		if err != nil {
			return nil, fmt.Errorf("check the saved login: %w", err)
		}
		if info != nil {
			return &LoginResult{User: info, AlreadySignedIn: true, CookieFile: e.cookies.Path()}, nil
		}
	}
	if e.cfg.Headless && !e.hasPersistentProfile() {
		return nil, usagef("signing in needs a browser window that shows the QR code: run `login` without --headless")
	}

	sess, err := e.launch(ctx, e.browserOptions())
	if err != nil {
		return nil, err
	}
	defer func() { _ = sess.Close() }()
	if err := sess.Navigate(ctx, e.api.BookDetailURL(e.probeBook()), e.cfg.LoadTimeout); err != nil {
		return nil, err
	}
	e.log.Info("scan the QR code in the browser window with the WeChat app to sign in")
	if err := e.signInWithBrowser(ctx, sess); err != nil {
		return nil, err
	}
	info, err := e.checkLogin(ctx, true)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("%w: the new login was rejected", ErrLoginNeeded)
	}
	return &LoginResult{User: info, CookieFile: e.cookies.Path()}, nil
}

// Logout deletes the saved login. A reader-mode switch an earlier run left
// behind is undone first, while the login can still do that. It reports
// whether there was a login file.
func (e *Exporter) Logout(ctx context.Context) (bool, error) {
	e.recoverReaderMode(ctx)
	path := e.cookies.Path()
	e.cookies.Clear()
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("remove the saved login: %w", err)
	}
	return true, nil
}
