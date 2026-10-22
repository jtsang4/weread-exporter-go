package weread

import (
	"path/filepath"
	"strings"
	"testing"
)

// FuzzParseRef checks what the -b/--book-id parser lets through: ids end up in
// file system paths and URLs, so whatever it accepts must be a plain
// alphanumeric token. Run it with
//
//	go test ./internal/weread -fuzz FuzzParseRef -fuzztime 30s
func FuzzParseRef(f *testing.F) {
	for _, seed := range []string{
		"", " ", "34336681", "08232ac0720befa90825d88", "352003193_7T4LYH9ib", "123456789012", "1234567890123",
		"https://weread.qq.com/web/bookDetail/08232ac0720befa90825d88",
		"https://weread.qq.com/web/reader/08232ac0720befa90825d88k1b6320202b61b6b50d5a9b0",
		"weread.qq.com/misc/booklist/352003193_7T4LYH9ib",
		"https://weread.qq.com/book-detail?v=08232ac0720befa90825d88",
		"https://evil.example/web/bookDetail/08232ac0720befa90825d88", "https://weread.qq.com.evil.example/web/bookDetail/x",
		"../../etc/passwd", "a_b/../c", "x://weread.qq.com/web/bookDetail/..%2f..%2fx", "\x00", "ａｂｃｄｅｆｇｈ",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		ref, err := ParseRef(in)
		if err != nil {
			return
		}
		if ref.Kind != RefBook && ref.Kind != RefBookList {
			t.Fatalf("%q: unknown kind %d", in, ref.Kind)
		}
		if ref.ID == "" || ref.ID == "." || ref.ID == ".." || filepath.Base(ref.ID) != ref.ID || strings.ContainsAny(ref.ID, `/\.:%?# `) {
			t.Fatalf("%q: the id %q is not safe in a path or URL", in, ref.ID)
		}
		switch ref.Kind {
		case RefBook:
			if !hashedIDPattern.MatchString(ref.ID) {
				t.Fatalf("%q: %q is not a hashed book id", in, ref.ID)
			}
		case RefBookList:
			if !strings.Contains(ref.ID, "_") {
				t.Fatalf("%q: %q is not a book list id", in, ref.ID)
			}
		}
	})
}
