package weread

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// RefKind says what a user-supplied book reference points at.
type RefKind int

const (
	// RefBook is a single book; Ref.ID is its hashed web id.
	RefBook RefKind = iota + 1
	// RefBookList is a WeRead book list; Ref.ID is the list id (contains "_").
	RefBookList
)

// Ref is a normalised reference to a book or a book list.
type Ref struct {
	Kind RefKind
	ID   string
}

// hashedIDPattern limits ids to a conservative alphabet: ids end up in file
// system paths (the cache directory) and URLs.
var hashedIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{8,64}$`)

// bookListIDPattern matches ids such as "352003193_7T4LYH9ib".
var bookListIDPattern = regexp.MustCompile(`^[0-9A-Za-z]+_[0-9A-Za-z_-]+$`)

// ParseRef understands everything the -b/--book-id flag accepts:
//
//   - a hashed book id: 08232ac0720befa90825d88
//   - a numeric book id (converted with Hash): 34336681
//   - a book list id (contains "_"): 352003193_7T4LYH9ib
//   - the URL of a book detail page, a reader page (with or without the
//     chapter part), a book-detail deep link or a book list page.
func ParseRef(input string) (Ref, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return Ref{}, fmt.Errorf("weread: empty book reference")
	}

	if strings.Contains(s, "://") || strings.HasPrefix(s, "weread.qq.com/") {
		return parseRefURL(s)
	}
	return parseRefID(s)
}

func parseRefID(s string) (Ref, error) {
	switch {
	case strings.Contains(s, "_"):
		if !bookListIDPattern.MatchString(s) {
			return Ref{}, fmt.Errorf("weread: %q is not a valid book list id", s)
		}
		return Ref{Kind: RefBookList, ID: s}, nil
	case isDigits(s) && len(s) <= 12:
		h, err := Hash(s)
		if err != nil {
			return Ref{}, err
		}
		return Ref{Kind: RefBook, ID: h}, nil
	case hashedIDPattern.MatchString(s):
		return Ref{Kind: RefBook, ID: s}, nil
	default:
		return Ref{}, fmt.Errorf("weread: %q is not a valid book id", s)
	}
}

func parseRefURL(raw string) (Ref, error) {
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Ref{}, fmt.Errorf("weread: invalid url %q: %w", raw, err)
	}
	if host := strings.ToLower(u.Hostname()); host != "weread.qq.com" && !strings.HasSuffix(host, ".weread.qq.com") {
		return Ref{}, fmt.Errorf("weread: %q is not a weread.qq.com url", raw)
	}

	path := strings.Trim(u.Path, "/")
	segments := strings.Split(path, "/")
	switch {
	case len(segments) >= 3 && segments[0] == "web" && segments[1] == "bookDetail":
		return parseRefID(segments[2])
	case len(segments) >= 3 && segments[0] == "web" && segments[1] == "reader":
		// <bookHash>k<chapterHash>: neither hash contains the letter "k".
		id, _, _ := strings.Cut(segments[2], "k")
		return parseRefID(id)
	case len(segments) >= 3 && segments[0] == "misc" && segments[1] == "booklist":
		return parseRefID(segments[2])
	case len(segments) >= 1 && segments[0] == "book-detail":
		if v := u.Query().Get("v"); v != "" {
			return parseRefID(v)
		}
	}
	return Ref{}, fmt.Errorf("weread: cannot find a book id in url %q", raw)
}
