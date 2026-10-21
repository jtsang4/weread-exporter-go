package book

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxFilenameBytes keeps "<title>.<ext>" below the 255-byte limit of common file systems.
const maxFilenameBytes = 200

// reservedWindowsNames cannot be used as file names on Windows, with or without extension.
var reservedWindowsNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {}, "COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {}, "LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// FormatFilename turns a book title into a safe file name.
//
// Like the original tool it percent-encodes the characters that are not allowed in
// file names ('/', '\\', ':' become %2f, %5c, %3a). It additionally encodes the
// characters Windows rejects ('*', '?', '"', '<', '>', '|') and control
// characters, strips trailing dots and spaces, avoids reserved device names and
// truncates very long titles, so that output written on one platform can be
// copied to another.
func FormatFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			fmt.Fprintf(&b, "%%%02x", r)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, "%%%02x", r)
		default:
			b.WriteRune(r)
		}
	}
	name := b.String()

	if len(name) > maxFilenameBytes {
		cut := maxFilenameBytes
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	name = strings.TrimRight(name, ". ")
	name = strings.TrimLeft(name, " ")

	base, _, _ := strings.Cut(name, ".")
	if _, reserved := reservedWindowsNames[strings.ToUpper(base)]; reserved {
		name = "_" + name
	}
	if name == "" {
		name = "book"
	}
	return name
}
