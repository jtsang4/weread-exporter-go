package browser

import (
	"errors"
	"fmt"
)

var (
	// ErrLoginRequired means the reader asks the visitor to sign in before it
	// shows the chapter.
	ErrLoginRequired = errors.New("browser: login required")

	// ErrHorizontalReader means the account is in the two-column paged reader
	// mode, which the capture hook cannot read. Switch the account to the
	// vertical reader (weread.Client.SetReaderMode) and try again.
	ErrHorizontalReader = errors.New("browser: the reader is in horizontal (paged) mode")

	// ErrHeadlessRefused means the user declined to continue in a browser that
	// looks headless.
	ErrHeadlessRefused = errors.New("browser: stopped because the browser looks headless")

	// ErrLoginTimeout means nobody completed the QR-code login in time.
	ErrLoginTimeout = errors.New("browser: timed out waiting for the login to complete")

	// ErrNoLoginButton means the page offers no way to log in (usually because
	// the visitor is already signed in).
	ErrNoLoginButton = errors.New("browser: the page has no login button")
)

// ChapterUnavailableError means the reader shows something other than the
// chapter text and a "next" button, for example a purchase prompt. Retrying
// cannot help.
type ChapterUnavailableError struct {
	// Footer is the text of the reader's footer button.
	Footer string
}

func (e *ChapterUnavailableError) Error() string {
	return fmt.Sprintf("browser: the chapter cannot be read (reader footer button %q); it is probably not free and not owned by this account", e.Footer)
}

// UnexpectedPageError means the browser did not end up on the reader page it
// was sent to (for example it was redirected), or the capture hook is missing.
type UnexpectedPageError struct {
	URL    string
	Title  string
	Reason string
}

func (e *UnexpectedPageError) Error() string {
	return fmt.Sprintf("browser: unexpected page %q (title %q): %s", e.URL, e.Title, e.Reason)
}

// StepTimeoutError reports that a wait inside the page ran out of time.
type StepTimeoutError struct {
	Step string
}

func (e *StepTimeoutError) Error() string { return "browser: timed out waiting for " + e.Step }

// Retryable reports whether trying the same chapter again may succeed.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	var unavailable *ChapterUnavailableError
	switch {
	case errors.Is(err, ErrLoginRequired), errors.Is(err, ErrHorizontalReader), errors.Is(err, ErrHeadlessRefused):
		return false
	case errors.As(err, &unavailable):
		return false
	}
	return true
}
