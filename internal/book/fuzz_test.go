package book

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzFormatFilename checks that no title, however odd, turns into a name that
// a file system rejects or that reaches outside the output directory. Run it
// with
//
//	go test ./internal/book -fuzz FuzzFormatFilename -fuzztime 30s
func FuzzFormatFilename(f *testing.F) {
	for _, seed := range []string{
		"", " ", ".", "..", "../../etc/passwd", `a\b`, "C:\\Windows", "a/b", "CON", "con.txt", "Nul ", "COM1.", "LPT9",
		"title.", "title . ", "\x00\x1f\x7f", "a*b?c\"d<e>f|g", "Python编程：从入门到实践（第2版）", strings.Repeat("长", 300),
		strings.Repeat("a", 199) + "长", strings.Repeat("a", 200) + ".", "\xff\xfe", "%2f",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, title string) {
		name := FormatFilename(title)
		switch {
		case name == "":
			t.Fatalf("empty name for %q", title)
		case len(name) > maxFilenameBytes+1: // +1: the "_" in front of a reserved device name
			t.Fatalf("%q is %d bytes long (title %q)", name, len(name), title)
		case !utf8.ValidString(name):
			t.Fatalf("%q is not valid UTF-8 (title %q)", name, title)
		case strings.ContainsAny(name, `/\:*?"<>|`):
			t.Fatalf("%q has a character that file systems reject (title %q)", name, title)
		case strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") || strings.HasPrefix(name, " "):
			t.Fatalf("%q starts or ends with a space or ends with a dot (title %q)", name, title)
		case name == "." || name == "..":
			t.Fatalf("%q is a directory name (title %q)", name, title)
		}
		for _, r := range name {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("%q has a control character (title %q)", name, title)
			}
		}
		base, _, _ := strings.Cut(name, ".")
		if _, reserved := reservedWindowsNames[strings.ToUpper(base)]; reserved {
			t.Fatalf("%q is a reserved device name on Windows (title %q)", name, title)
		}
	})
}
