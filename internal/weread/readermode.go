package weread

import (
	"context"
	"encoding/json"
	"fmt"
)

// ReaderMode is the account setting that decides how the web reader lays out a
// chapter. The exporter needs the vertical, scrolling reader ("上下滚动阅读",
// internally "LegacyReader"): its single-column canvas is what the capture hook
// understands. New accounts default to the two-column paged reader, so the
// exporter switches the account to vertical for the duration of an export and
// restores the previous value afterwards.
type ReaderMode int

const (
	// ReaderModeVertical is the scrolling single-column reader (useHorizonReader=0).
	ReaderModeVertical ReaderMode = 0
	// ReaderModeHorizontal is the paged two-column reader (useHorizonReader=1).
	ReaderModeHorizontal ReaderMode = 1
)

// String implements fmt.Stringer.
func (m ReaderMode) String() string {
	switch m {
	case ReaderModeVertical:
		return "vertical"
	case ReaderModeHorizontal:
		return "horizontal"
	default:
		return fmt.Sprintf("ReaderMode(%d)", int(m))
	}
}

// ReaderMode reports the reader mode the server renders for the given cookies.
// Without a valid login the site always answers with its default (horizontal).
func (c *Client) ReaderMode(ctx context.Context, cookies *Cookies, bookID string) (ReaderMode, error) {
	resp, err := c.Get(ctx, c.ReaderURL(bookID, 0), map[string]string{"Cookie": cookies.Header()})
	if err != nil {
		return 0, err
	}
	return ParseReaderMode(resp.Body)
}

// ParseReaderMode reads config.useHorizonReader from the server-rendered
// window.__INITIAL_STATE__ of a reader page.
func ParseReaderMode(html []byte) (ReaderMode, error) {
	raw, err := extractAssignedJSON(html, "window.__INITIAL_STATE__")
	if err != nil {
		return 0, err
	}
	var state struct {
		Config struct {
			UseHorizonReader *flexInt `json:"useHorizonReader"`
		} `json:"config"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return 0, fmt.Errorf("weread: parse reader state: %w", err)
	}
	if state.Config.UseHorizonReader == nil {
		return 0, fmt.Errorf("weread: reader page does not expose config.useHorizonReader")
	}
	if *state.Config.UseHorizonReader != 0 {
		return ReaderModeHorizontal, nil
	}
	return ReaderModeVertical, nil
}

// SetReaderMode changes the account setting (the same call the site's own
// "上下滚动阅读 / 双栏阅读" toolbar button makes). It requires a valid login.
func (c *Client) SetReaderMode(ctx context.Context, cookies *Cookies, mode ReaderMode) error {
	body, err := json.Marshal(map[string]int{"useHorizonReader": int(mode)})
	if err != nil {
		return err
	}
	resp, err := c.Do(ctx, Request{
		Method: "POST",
		URL:    c.baseURL + "/web/user_config/modify",
		Header: map[string]string{
			"Cookie":       cookies.Header(),
			"Referer":      c.baseURL + "/",
			"Content-Type": "application/json;charset=UTF-8",
		},
		Body: body,
	})
	if err != nil {
		return err
	}
	if !resp.OK() {
		return &StatusError{URL: c.baseURL + "/web/user_config/modify", Status: resp.Status}
	}

	var out struct {
		ErrCode int     `json:"errCode"`
		ErrMsg  string  `json:"errMsg"`
		Value   flexInt `json:"useHorizonReader"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return fmt.Errorf("weread: parse reader mode response: %w", err)
	}
	switch {
	case out.ErrCode == errCodeSessionExpired || out.ErrCode == errCodeUserNotFound:
		return fmt.Errorf("%w: changing the reader mode needs a logged-in session (errCode %d)", ErrInvalidUser, out.ErrCode)
	case out.ErrCode != 0:
		return fmt.Errorf("weread: set reader mode failed: errCode=%d errMsg=%q", out.ErrCode, out.ErrMsg)
	case ReaderMode(out.Value.Int()) != mode:
		return fmt.Errorf("weread: server answered useHorizonReader=%d, wanted %d", out.Value.Int(), int(mode))
	}
	return nil
}
