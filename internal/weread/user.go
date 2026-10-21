package weread

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// Error codes returned by the WeRead web API.
const (
	errCodeUserNotFound   = -2010
	errCodeSessionExpired = -2012
)

// ErrInvalidUser means the cookies do not identify a usable account: they are
// missing, belong to no user, or the session expired and could not be renewed.
var ErrInvalidUser = errors.New("weread: invalid user")

// UserInfo is the public profile returned by /web/user.
type UserInfo struct {
	VID    int64  `json:"userVid"`
	Name   string `json:"name"`
	Avatar string `json:"avatar"`

	ErrCode int    `json:"errCode"`
	ErrMsg  string `json:"errMsg"`
}

// UserInfo validates the cookies by loading the profile of the logged-in user.
//
// When the session key has expired (errCode -2012) it is renewed with the
// long-lived wr_rt cookie (see RefreshCookies), the cookies are persisted and
// the request is retried once. Errors wrapping ErrInvalidUser mean the user has
// to log in again.
func (c *Client) UserInfo(ctx context.Context, cookies *Cookies) (*UserInfo, error) {
	vid := cookies.VID()
	if !isDigits(vid) {
		return nil, fmt.Errorf("%w: no wr_vid cookie", ErrInvalidUser)
	}

	info, err := c.fetchUser(ctx, cookies, vid)
	if err != nil {
		return nil, err
	}
	if info.ErrCode == errCodeSessionExpired {
		c.log.Info("session expired, renewing it")
		if err := c.RefreshCookies(ctx, cookies); err != nil {
			return nil, err
		}
		if info, err = c.fetchUser(ctx, cookies, vid); err != nil {
			return nil, err
		}
	}

	switch info.ErrCode {
	case 0:
		return info, nil
	case errCodeUserNotFound:
		return nil, fmt.Errorf("%w: user %s not found", ErrInvalidUser, vid)
	case errCodeSessionExpired:
		return nil, fmt.Errorf("%w: session expired and refreshing the cookies did not help", ErrInvalidUser)
	default:
		return nil, fmt.Errorf("weread: get user info failed: errCode=%d errMsg=%q", info.ErrCode, info.ErrMsg)
	}
}

func (c *Client) fetchUser(ctx context.Context, cookies *Cookies, vid string) (*UserInfo, error) {
	resp, err := c.Get(ctx, c.baseURL+"/web/user?userVid="+vid, c.cookieHeaders(cookies))
	if err != nil {
		return nil, err
	}
	var info UserInfo
	if err := json.Unmarshal(resp.Body, &info); err != nil {
		return nil, fmt.Errorf("weread: parse user info: %w", err)
	}
	return &info, nil
}

// renewalBody is what the site's own scripts send to renew a session.
const renewalBody = `{"rq":"%2Fweb%2Fbook%2Fread","ql":true}`

// RefreshCookies renews the session key the way the site's own scripts do: the
// long-lived wr_rt cookie entitles the client to a new wr_skey, which
// POST /web/login/renewal hands out in Set-Cookie headers. (Merely visiting the
// home page yields a wr_skey too, but an anonymous one.) The renewed set is saved.
// Errors wrapping ErrInvalidUser mean the refresh token is no longer accepted.
func (c *Client) RefreshCookies(ctx context.Context, cookies *Cookies) error {
	endpoint := c.baseURL + "/web/login/renewal"
	header := c.cookieHeaders(cookies)
	header["Referer"] = c.baseURL + "/"
	header["Origin"] = c.baseURL
	header["Content-Type"] = "application/json;charset=UTF-8"

	resp, err := c.Do(ctx, Request{Method: "POST", URL: endpoint, Header: header, Body: []byte(renewalBody)})
	if err != nil {
		return err
	}
	if !resp.OK() {
		return &StatusError{URL: endpoint, Status: resp.Status}
	}
	var out struct {
		Succ    int    `json:"succ"`
		ErrCode int    `json:"errCode"`
		ErrMsg  string `json:"errMsg"`
	}
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return fmt.Errorf("weread: parse renewal response: %w", err)
	}
	if out.Succ != 1 {
		return fmt.Errorf("%w: the session could not be renewed (errCode %d %q)", ErrInvalidUser, out.ErrCode, out.ErrMsg)
	}
	updated := cookies.MergeSetCookie(resp.Header)
	if len(updated) == 0 {
		c.log.Warn("cookie renewal returned no Set-Cookie headers")
		return nil
	}
	c.log.Info("cookies renewed", "updated", updated)
	return cookies.Save()
}

func (c *Client) cookieHeaders(cookies *Cookies) map[string]string {
	return map[string]string{
		"Referer": c.baseURL,
		"Cookie":  cookies.Header(),
	}
}
