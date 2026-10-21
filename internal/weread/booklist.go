package weread

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// BookListEntry is one book of a WeRead book list.
type BookListEntry struct {
	// BookID is the numeric book id as it appears in the page.
	BookID string
	// ID is the hashed id used in reader URLs and as the cache directory name.
	ID string
	// Title is the book title (informational; may be empty).
	Title string
}

var bookEntitiesPattern = regexp.MustCompile(`["']?bookEntities["']?\s*:`)

// BookListURL is the public page of a book list.
func (c *Client) BookListURL(listID string) string {
	return c.baseURL + "/misc/booklist/" + listID
}

// BookList downloads a book list page and returns its books in list order.
func (c *Client) BookList(ctx context.Context, listID string) ([]BookListEntry, error) {
	resp, err := c.Get(ctx, c.BookListURL(listID), nil)
	if err != nil {
		return nil, err
	}
	return ParseBookList(resp.Body)
}

// ParseBookList extracts the books from the Nuxt payload (window.__NUXT__) of a
// book list page:
//
//	window.__NUXT__=(function(a,b,...){return {...,data:[{bookEntities:{"912071":{bookId:q,...,title:"…"},...}}]}}("…",…))
//
// The payload is JavaScript, not JSON: values may be variables that the minifier
// hoisted into the argument list of the wrapping function, so titles that are
// not plain literals are resolved through that argument list.
func ParseBookList(html []byte) ([]BookListEntry, error) {
	text := string(html)
	start := strings.Index(text, "window.__NUXT__")
	if start < 0 {
		return nil, fmt.Errorf("weread: unexpected book list page (no window.__NUXT__): %s", excerpt(text))
	}
	payload := text[start:]

	loc := bookEntitiesPattern.FindStringIndex(payload)
	if loc == nil {
		return nil, fmt.Errorf("weread: book list page has no bookEntities")
	}
	vars := nuxtVariables(payload)

	sc := &jsScanner{s: payload, pos: loc[1]}
	var entries []BookListEntry
	err := sc.objectFields(func(bookKey string) error {
		sc.skipSpace()
		if sc.peek() != '{' {
			return errSkipValue
		}
		var title string
		if err := sc.objectFields(func(key string) error {
			if key != "title" {
				return errSkipValue
			}
			sc.skipSpace()
			if c := sc.peek(); c == '"' || c == '\'' {
				t, err := sc.readString()
				title = t
				return err
			}
			begin := sc.pos
			for !sc.eof() && isIdentByte(sc.s[sc.pos]) {
				sc.pos++
			}
			if ident := sc.s[begin:sc.pos]; isIdentifier(ident) {
				title = vars[ident]
				return nil
			}
			return errSkipValue
		}); err != nil {
			return err
		}

		if !isDigits(bookKey) {
			return nil // not a book entry
		}
		hashed, err := Hash(bookKey)
		if err != nil {
			return err
		}
		entries = append(entries, BookListEntry{BookID: bookKey, ID: hashed, Title: title})
		return nil
	})
	if err != nil {
		return entries, fmt.Errorf("weread: parse book list: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("weread: book list contains no books")
	}
	return entries, nil
}

// nuxtVariables maps the parameter names of the function wrapping the Nuxt
// payload to the string literals passed as its arguments. Best effort: on any
// parse problem the (possibly partial) map built so far is returned.
func nuxtVariables(payload string) map[string]string {
	vars := map[string]string{}

	eq := strings.IndexByte(payload, '=')
	if eq < 0 {
		return vars
	}
	sc := &jsScanner{s: payload, pos: eq + 1}
	sc.skipSpace()
	if sc.peek() == '(' {
		sc.pos++
	}
	sc.skipSpace()
	if !strings.HasPrefix(sc.s[sc.pos:], "function") {
		return vars
	}
	sc.pos += len("function")
	if err := sc.expect('('); err != nil {
		return vars
	}
	closeParen := strings.IndexByte(sc.s[sc.pos:], ')')
	if closeParen < 0 {
		return vars
	}
	var params []string
	for _, p := range strings.Split(sc.s[sc.pos:sc.pos+closeParen], ",") {
		params = append(params, strings.TrimSpace(p))
	}
	sc.pos += closeParen + 1

	// Skip the function body, then read the call arguments.
	if err := sc.skipBalanced(); err != nil {
		return vars
	}
	if err := sc.expect('('); err != nil {
		return vars
	}
	for i := 0; i < len(params); i++ {
		sc.skipSpace()
		if sc.eof() || sc.peek() == ')' {
			break
		}
		if c := sc.peek(); c == '"' || c == '\'' {
			s, err := sc.readString()
			if err != nil {
				return vars
			}
			vars[params[i]] = s
		} else if err := sc.skipValue(); err != nil {
			return vars
		}
		sc.skipSpace()
		if sc.peek() == ',' {
			sc.pos++
		}
	}
	return vars
}

func excerpt(s string) string {
	const limit = 160
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}
