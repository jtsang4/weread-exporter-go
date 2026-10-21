package weread

import "testing"

func TestParseRef(t *testing.T) {
	const hashed = "08232ac0720befa90825d88" // numeric id 34336681
	cases := []struct {
		name string
		in   string
		want Ref
	}{
		{"numeric id", "34336681", Ref{RefBook, hashed}},
		{"numeric id with spaces", "  34336681\n", Ref{RefBook, hashed}},
		{"hashed id", hashed, Ref{RefBook, hashed}},
		{"book list id", "352003193_7T4LYH9ib", Ref{RefBookList, "352003193_7T4LYH9ib"}},
		{"book detail url", "https://weread.qq.com/web/bookDetail/" + hashed, Ref{RefBook, hashed}},
		{"book detail url with query and fragment", "https://weread.qq.com/web/bookDetail/" + hashed + "?from=x#top", Ref{RefBook, hashed}},
		{"book detail url without scheme", "weread.qq.com/web/bookDetail/" + hashed, Ref{RefBook, hashed}},
		{"reader url", "https://weread.qq.com/web/reader/" + hashed, Ref{RefBook, hashed}},
		{"reader url with chapter", "https://weread.qq.com/web/reader/" + hashed + "k34132fc02293416a75f431d", Ref{RefBook, hashed}},
		{"book list url", "https://weread.qq.com/misc/booklist/352003193_7T4LYH9ib", Ref{RefBookList, "352003193_7T4LYH9ib"}},
		{"book-detail deep link", "https://weread.qq.com/book-detail?v=" + hashed, Ref{RefBook, hashed}},
		{"http url", "http://weread.qq.com/web/bookDetail/" + hashed, Ref{RefBook, hashed}},
		{"numeric id in url", "https://weread.qq.com/web/bookDetail/34336681", Ref{RefBook, hashed}},
		{"very long numeric string is taken as an opaque id", "1234567890123", Ref{RefBook, "1234567890123"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRef(tc.in)
			if err != nil {
				t.Fatalf("ParseRef(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseRef(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRefRejectsGarbage(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"abc",                       // too short for a hashed id
		"../../etc/passwd",          // must never reach the file system
		"book/id/with/slashes12345", //
		"bad id with spaces",        //
		"a_b/c",                     // not a valid list id
		"https://example.com/web/bookDetail/08232ac0720befa90825d88",
		"https://weread.qq.com/",
		"https://weread.qq.com/web/bookDetail/",
		"https://weread.qq.com/something/else",
		"https://weread.qq.com.evil.example/web/bookDetail/08232ac0720befa90825d88",
		"https://evilweread.qq.com/web/bookDetail/08232ac0720befa90825d88",
	} {
		if got, err := ParseRef(in); err == nil {
			t.Errorf("ParseRef(%q) = %+v, want an error", in, got)
		}
	}
}
