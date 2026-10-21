package book

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatFilename(t *testing.T) {
	cases := map[string]string{
		"Python编程：从入门到实践（第2版）": "Python编程：从入门到实践（第2版）", // full-width punctuation is fine
		"a/b":                     "a%2fb",
		`a\b`:                     "a%5cb",
		"C++: the book":           "C++%3a the book",
		"What? Why*":              "What%3f Why%2a",
		`say "hi" <b>|x`:          "say %22hi%22 %3cb%3e%7cx",
		"tab\there":               "tab%09here",
		"trailing dots...":        "trailing dots",
		"  leading and trailing ": "leading and trailing",
		"CON":                     "_CON",
		"nul.txt":                 "_nul.txt",
		"com1":                    "_com1",
		"console":                 "console",
		"":                        "book",
		"...":                     "book",
	}
	for in, want := range cases {
		if got := FormatFilename(in); got != want {
			t.Errorf("FormatFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatFilenameTruncatesOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("书", 200) // 600 bytes
	got := FormatFilename(long)
	if len(got) > maxFilenameBytes {
		t.Fatalf("len = %d", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatal("truncation split a multi-byte character")
	}
	if got == "" || !strings.HasPrefix(long, got) {
		t.Fatal("truncated name must be a prefix of the title")
	}
}
