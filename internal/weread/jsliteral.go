package weread

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// jsScanner walks over a fragment of minified JavaScript (the Nuxt payload of
// a book list page). It does not evaluate anything: it understands just enough
// of the syntax - string literals, balanced brackets, object literals - to pick
// individual fields out of an object literal without being fooled by commas,
// braces or quotes that appear inside strings.
type jsScanner struct {
	s   string
	pos int
}

func (sc *jsScanner) eof() bool { return sc.pos >= len(sc.s) }

func (sc *jsScanner) peek() byte {
	if sc.eof() {
		return 0
	}
	return sc.s[sc.pos]
}

func (sc *jsScanner) skipSpace() {
	for !sc.eof() {
		switch sc.s[sc.pos] {
		case ' ', '\t', '\r', '\n':
			sc.pos++
		default:
			return
		}
	}
}

func (sc *jsScanner) expect(b byte) error {
	sc.skipSpace()
	if sc.peek() != b {
		return fmt.Errorf("weread: js literal: expected %q at offset %d, found %q", b, sc.pos, sc.near())
	}
	sc.pos++
	return nil
}

func (sc *jsScanner) near() string {
	end := min(sc.pos+20, len(sc.s))
	if sc.pos >= end {
		return "<eof>"
	}
	return sc.s[sc.pos:end]
}

func isIdentByte(b byte) bool {
	return b == '_' || b == '$' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b >= 0x80
}

// isIdentifier reports whether s is a plain JavaScript identifier.
func isIdentifier(s string) bool {
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

// readKey reads an object key: a quoted string or a bare identifier/number.
func (sc *jsScanner) readKey() (string, error) {
	sc.skipSpace()
	switch c := sc.peek(); {
	case c == '"' || c == '\'':
		return sc.readString()
	case isIdentByte(c):
		start := sc.pos
		for !sc.eof() && isIdentByte(sc.s[sc.pos]) {
			sc.pos++
		}
		return sc.s[start:sc.pos], nil
	default:
		return "", fmt.Errorf("weread: js literal: bad object key at offset %d near %q", sc.pos, sc.near())
	}
}

// readString decodes a single- or double-quoted string literal.
func (sc *jsScanner) readString() (string, error) {
	quote := sc.peek()
	if quote != '"' && quote != '\'' {
		return "", fmt.Errorf("weread: js literal: expected string at offset %d near %q", sc.pos, sc.near())
	}
	sc.pos++
	var b strings.Builder
	for !sc.eof() {
		c := sc.s[sc.pos]
		switch c {
		case quote:
			sc.pos++
			return b.String(), nil
		case '\\':
			sc.pos++
			if err := sc.readEscape(&b); err != nil {
				return "", err
			}
		default:
			r, size := utf8.DecodeRuneInString(sc.s[sc.pos:])
			if r == utf8.RuneError && size == 1 {
				b.WriteByte(c)
			} else {
				b.WriteRune(r)
			}
			sc.pos += size
		}
	}
	return "", fmt.Errorf("weread: js literal: unterminated string")
}

func (sc *jsScanner) readEscape(b *strings.Builder) error {
	if sc.eof() {
		return fmt.Errorf("weread: js literal: dangling backslash")
	}
	c := sc.s[sc.pos]
	sc.pos++
	switch c {
	case 'n':
		b.WriteByte('\n')
	case 'r':
		b.WriteByte('\r')
	case 't':
		b.WriteByte('\t')
	case 'b':
		b.WriteByte('\b')
	case 'f':
		b.WriteByte('\f')
	case 'v':
		b.WriteByte('\v')
	case '0':
		b.WriteByte(0)
	case '\r':
		if sc.peek() == '\n' {
			sc.pos++
		}
	case '\n':
		// line continuation
	case 'x':
		r, err := sc.readHex(2)
		if err != nil {
			return err
		}
		b.WriteRune(r)
	case 'u':
		r, err := sc.readUnicodeEscape()
		if err != nil {
			return err
		}
		b.WriteRune(r)
	default:
		// \" \' \\ \/ and any other escaped character stand for themselves.
		r, size := utf8.DecodeRuneInString(sc.s[sc.pos-1:])
		b.WriteRune(r)
		sc.pos += size - 1
	}
	return nil
}

// hexValue is the value of the hexadecimal digit c, or -1 if c is not one.
func hexValue(c byte) rune {
	switch {
	case c >= '0' && c <= '9':
		return rune(c - '0')
	case c >= 'a' && c <= 'f':
		return rune(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return rune(c-'A') + 10
	}
	return -1
}

// readHex reads n hexadecimal digits (at most four: the value must fit a rune).
func (sc *jsScanner) readHex(n int) (rune, error) {
	if sc.pos+n > len(sc.s) {
		return 0, errors.New("weread: js literal: truncated hex escape")
	}
	var v rune
	for _, c := range []byte(sc.s[sc.pos : sc.pos+n]) {
		d := hexValue(c)
		if d < 0 {
			return 0, fmt.Errorf("weread: js literal: bad hex escape %q", sc.s[sc.pos:sc.pos+n])
		}
		v = v<<4 | d
	}
	sc.pos += n
	return v, nil
}

func (sc *jsScanner) readUnicodeEscape() (rune, error) {
	if sc.peek() == '{' {
		end := strings.IndexByte(sc.s[sc.pos:], '}')
		if end < 0 {
			return 0, errors.New(`weread: js literal: unterminated \u{...}`)
		}
		v, err := strconv.ParseUint(sc.s[sc.pos+1:sc.pos+end], 16, 32)
		if err != nil || v > unicode.MaxRune {
			return 0, errors.New(`weread: js literal: bad \u{...} escape`)
		}
		sc.pos += end + 1
		return rune(v), nil
	}
	r, err := sc.readHex(4)
	if err != nil {
		return 0, err
	}
	if utf16.IsSurrogate(r) && strings.HasPrefix(sc.s[sc.pos:], `\u`) {
		save := sc.pos
		sc.pos += 2
		if lo, err := sc.readHex(4); err == nil {
			if pair := utf16.DecodeRune(r, lo); pair != utf8.RuneError {
				return pair, nil
			}
		}
		sc.pos = save
	}
	return r, nil
}

// skipString advances past a quoted string (or template literal) without decoding it.
func (sc *jsScanner) skipString() error {
	quote := sc.peek()
	sc.pos++
	for !sc.eof() {
		switch sc.s[sc.pos] {
		case '\\':
			sc.pos += 2
		case quote:
			sc.pos++
			return nil
		default:
			sc.pos++
		}
	}
	return fmt.Errorf("weread: js literal: unterminated string")
}

// skipValue advances past one expression: it stops in front of the first ',' or
// closing bracket that is not nested inside brackets or strings.
func (sc *jsScanner) skipValue() error {
	depth := 0
	for !sc.eof() {
		switch c := sc.s[sc.pos]; c {
		case '"', '\'', '`':
			if err := sc.skipString(); err != nil {
				return err
			}
			continue
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			if depth == 0 {
				return nil
			}
			depth--
		case ',':
			if depth == 0 {
				return nil
			}
		}
		sc.pos++
	}
	if depth != 0 {
		return fmt.Errorf("weread: js literal: unbalanced brackets")
	}
	return nil
}

// skipBalanced advances past exactly one bracketed group ({...}, [...] or (...))
// starting at the scanner position.
func (sc *jsScanner) skipBalanced() error {
	sc.skipSpace()
	if c := sc.peek(); c != '{' && c != '[' && c != '(' {
		return fmt.Errorf("weread: js literal: expected a bracket at offset %d near %q", sc.pos, sc.near())
	}
	depth := 0
	for !sc.eof() {
		switch sc.s[sc.pos] {
		case '"', '\'', '`':
			if err := sc.skipString(); err != nil {
				return err
			}
			continue
		case '{', '[', '(':
			depth++
		case '}', ']', ')':
			depth--
			if depth == 0 {
				sc.pos++
				return nil
			}
		}
		sc.pos++
	}
	return fmt.Errorf("weread: js literal: unbalanced brackets")
}

// objectFields calls fn for every top-level field of the object literal at the
// scanner position. fn must consume the field value (leaving the scanner right
// behind it); returning errSkipValue makes objectFields skip it instead.
func (sc *jsScanner) objectFields(fn func(key string) error) error {
	if err := sc.expect('{'); err != nil {
		return err
	}
	for {
		sc.skipSpace()
		if sc.peek() == '}' {
			sc.pos++
			return nil
		}
		key, err := sc.readKey()
		if err != nil {
			return err
		}
		if err := sc.expect(':'); err != nil {
			return err
		}
		sc.skipSpace()
		start := sc.pos
		if err := fn(key); err != nil {
			if !errors.Is(err, errSkipValue) {
				return err
			}
			sc.pos = start
			if err := sc.skipValue(); err != nil {
				return err
			}
		}
		sc.skipSpace()
		switch sc.peek() {
		case ',':
			sc.pos++
		case '}':
			sc.pos++
			return nil
		default:
			return fmt.Errorf("weread: js literal: expected ',' or '}' at offset %d near %q", sc.pos, sc.near())
		}
	}
}

// errSkipValue tells objectFields that fn is not interested in the value.
var errSkipValue = fmt.Errorf("skip value")
