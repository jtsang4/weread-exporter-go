package weread

import (
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestParseCookieText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"json object", `{"wr_vid":"123","wr_skey":"abc"}`, map[string]string{"wr_vid": "123", "wr_skey": "abc"}},
		{"json with numbers and bools", `{"a":1,"b":true,"c":"x","d":null,"e":[1]}`, map[string]string{"a": "1", "b": "true", "c": "x"}},
		{"cookie header", "wr_vid=123; wr_skey=abc; flag", map[string]string{"wr_vid": "123", "wr_skey": "abc"}},
		{"value containing equals sign", "token=a=b==", map[string]string{"token": "a=b=="}},
		{"whitespace only", " \n\t ", map[string]string{}},
		{"empty", "", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseCookieText(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLoadCookiesMissingFileIsEmpty(t *testing.T) {
	c, err := LoadCookies(filepath.Join(t.TempDir(), "nope.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Len() != 0 {
		t.Fatalf("Len = %d", c.Len())
	}
}

func TestCookiesSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "cookie.txt")
	c := NewCookies(path)
	c.Set("wr_vid", "42")
	c.Set("wr_skey", "s3cret")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("cookie file mode = %v, want 0600", perm)
		}
	}

	loaded, err := LoadCookies(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded.Map(), c.Map()) {
		t.Fatalf("round trip mismatch: %v vs %v", loaded.Map(), c.Map())
	}
	if loaded.VID() != "42" || loaded.Path() != path {
		t.Fatalf("VID=%q Path=%q", loaded.VID(), loaded.Path())
	}
}

func TestCookiesSaveWithoutPathIsNoop(t *testing.T) {
	c := NewCookies("")
	c.Set("a", "b")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestCookiesHeaderIsSortedAndNamesCarryNoValues(t *testing.T) {
	c := NewCookies("")
	c.Set("b", "2")
	c.Set("a", "1")
	if got := c.Header(); got != "a=1; b=2" {
		t.Fatalf("Header = %q", got)
	}
	if got := c.Names(); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("Names = %v", got)
	}
}

func TestCookiesMapReturnsACopy(t *testing.T) {
	c := NewCookies("")
	c.Set("a", "1")
	m := c.Map()
	m["a"] = "tampered"
	if c.Get("a") != "1" {
		t.Fatal("Map leaked internal state")
	}
}

func TestCookiesReplaceAndClear(t *testing.T) {
	c := NewCookies("")
	c.Set("old", "1")
	in := map[string]string{"new": "2"}
	c.Replace(in)
	in["new"] = "changed"
	if c.Get("old") != "" || c.Get("new") != "2" {
		t.Fatalf("Replace: %v", c.Map())
	}
	c.Clear()
	if c.Len() != 0 {
		t.Fatal("Clear left cookies behind")
	}
}

func TestMergeSetCookie(t *testing.T) {
	c := NewCookies("")
	c.Set("wr_skey", "old")
	c.Set("wr_vid", "1")
	h := http.Header{}
	h.Add("Set-Cookie", "wr_skey=new; Path=/; HttpOnly; Max-Age=3600")
	h.Add("Set-Cookie", "wr_fp=999; Path=/")
	h.Add("Set-Cookie", "garbage-without-equals")
	updated := c.MergeSetCookie(h)
	if !reflect.DeepEqual(updated, []string{"wr_skey", "wr_fp"}) {
		t.Fatalf("updated = %v", updated)
	}
	if c.Get("wr_skey") != "new" || c.Get("wr_fp") != "999" || c.Get("wr_vid") != "1" {
		t.Fatalf("cookies = %v", c.Map())
	}
}

func TestFilterPersistentDropsSiteUIState(t *testing.T) {
	in := map[string]string{"wr_vid": "1", "wr_theme": "white", "wr_useHorizonReader": "1", "wr_skey": "x"}
	got := FilterPersistent(in)
	want := map[string]string{"wr_vid": "1", "wr_skey": "x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if len(in) != 4 {
		t.Fatal("FilterPersistent modified its input")
	}
}
