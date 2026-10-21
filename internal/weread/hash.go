// Package weread talks to WeRead (微信读书) over plain HTTP: book metadata,
// book lists, user/cookie handling and the account's reader-mode setting.
// Everything that needs a real browser lives in package browser.
package weread

import (
	"crypto/md5" //nolint:gosec // md5 is part of WeRead's id scheme, not a security control
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

// Hash computes the "web id" WeRead uses in /web/reader and /web/bookDetail
// URLs from a numeric book id or chapter uid, e.g. Hash("42557145") ==
// "f343248072895ed9f34f408".
//
// It is a faithful port of the wr_hash function of the original Python tool
// (itself a port of the site's obfuscated JavaScript): the id is split into
// 9-digit chunks, every chunk is rendered as hex with a two-digit length prefix,
// and the result is wrapped by fragments of the md5 digest.
func Hash(s string) (string, error) {
	digest := md5Hex(s)
	result := digest[:3] + "32" + digest[len(digest)-2:]

	var chunks []string
	for i := 0; i < len(s); i += 9 {
		end := min(i+9, len(s))
		n, err := strconv.ParseUint(s[i:end], 10, 64)
		if err != nil {
			return "", fmt.Errorf("weread: hash input %q is not a decimal number: %w", s, err)
		}
		chunks = append(chunks, strconv.FormatUint(n, 16))
	}

	for i, chunk := range chunks {
		result += fmt.Sprintf("%02x", len(chunk)) + chunk
		if i < len(chunks)-1 {
			result += "g"
		}
	}

	if len(result) < 20 {
		result += digest[:20-len(result)]
	}
	result += md5Hex(result)[:3]
	return result, nil
}

// MustHash is Hash for inputs that are known to be numeric (formatted integers).
func MustHash(s string) string {
	h, err := Hash(s)
	if err != nil {
		panic(err)
	}
	return h
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s)) //nolint:gosec // see import comment
	return hex.EncodeToString(sum[:])
}

// ChapterHash returns the web id of a chapter uid.
func ChapterHash(uid int64) string {
	return MustHash(strconv.FormatInt(uid, 10))
}

// isDigits reports whether s is a non-empty string of ASCII digits.
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) < 0
}
