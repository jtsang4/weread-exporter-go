package weread

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// flexInt decodes an integer that the site sometimes serialises as a string
// ("1") and sometimes as a number (1); null and booleans are tolerated too.
type flexInt int64

// UnmarshalJSON implements json.Unmarshaler.
func (f *flexInt) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	switch s {
	case "", "null", "false":
		*f = 0
		return nil
	case "true":
		*f = 1
		return nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		*f = flexInt(n)
		return nil
	}
	if x, err := strconv.ParseFloat(s, 64); err == nil {
		*f = flexInt(x)
		return nil
	}
	return fmt.Errorf("weread: cannot parse %q as an integer", data)
}

// Int returns the value as int.
func (f flexInt) Int() int { return int(f) }

// extractAssignedJSON finds `<marker> = {json}` in script text and returns the
// raw JSON value. Decoding with json.Decoder (instead of searching for "};")
// copes with braces inside strings.
func extractAssignedJSON(html []byte, marker string) (json.RawMessage, error) {
	pos := bytes.Index(html, []byte(marker))
	if pos < 0 {
		return nil, fmt.Errorf("weread: %s not found in page", marker)
	}
	rest := html[pos+len(marker):]
	eq := bytes.IndexByte(rest, '=')
	if eq < 0 {
		return nil, fmt.Errorf("weread: no assignment after %s", marker)
	}
	rest = bytes.TrimLeft(rest[eq+1:], " \t\r\n")
	var raw json.RawMessage
	if err := json.NewDecoder(bytes.NewReader(rest)).Decode(&raw); err != nil {
		return nil, fmt.Errorf("weread: decode %s: %w", marker, err)
	}
	return raw, nil
}
