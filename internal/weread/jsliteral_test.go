package weread

import (
	"encoding/json"
	"testing"
)

func TestJSScannerReadString(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{`"plain"`, "plain"},
		{`'single'`, "single"},
		{`"esc \" \\ \/ \n \t"`, "esc \" \\ / \n \t"},
		{`"\u4e2d\u6587"`, "中文"},
		{`"\ud83d\ude00"`, "😀"},
		{`"\u{1F600}"`, "😀"},
		{`"\x41"`, "A"},
		{`"line \` + "\n" + `continued"`, "line continued"},
		{`"héllo — ünïcode"`, "héllo — ünïcode"},
		{`""`, ""},
	}
	for _, tc := range cases {
		sc := &jsScanner{s: tc.in + ",rest"}
		got, err := sc.readString()
		if err != nil {
			t.Errorf("readString(%s): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("readString(%s) = %q, want %q", tc.in, got, tc.want)
		}
		if sc.s[sc.pos:] != ",rest" {
			t.Errorf("readString(%s) left the scanner at %q", tc.in, sc.s[sc.pos:])
		}
	}
}

func TestJSScannerReadStringErrors(t *testing.T) {
	for _, in := range []string{`"unterminated`, `"bad \u12"`, `"bad \uZZZZ"`, `"dangling \`, `notastring`, `"\u{zz}"`} {
		sc := &jsScanner{s: in}
		if got, err := sc.readString(); err == nil {
			t.Errorf("readString(%s) = %q, want an error", in, got)
		}
	}
}

func TestJSScannerSkipValue(t *testing.T) {
	cases := map[string]string{
		`a,b`:                     ",b",
		`void 0,x`:                ",x",
		`"a,b",x`:                 ",x",
		`'it\'s, ok',x`:           ",x",
		`{a:1,b:[1,2,{c:"}"}]},x`: ",x",
		`f(1,2,(3,4)),x`:          ",x",
		"`tpl, ${a}`,x":           ",x",
		`-1}`:                     "}",
		`[1,2,3]]`:                "]",
		`!0)`:                     ")",
		`{"k":"v"}`:               "",
		`q`:                       "",
	}
	for in, rest := range cases {
		sc := &jsScanner{s: in}
		if err := sc.skipValue(); err != nil {
			t.Errorf("skipValue(%q): %v", in, err)
			continue
		}
		if got := sc.s[sc.pos:]; got != rest {
			t.Errorf("skipValue(%q) stopped at %q, want %q", in, got, rest)
		}
	}
}

func TestJSScannerSkipValueUnbalanced(t *testing.T) {
	for _, in := range []string{`{a:1`, `[1,2`, `"open`} {
		sc := &jsScanner{s: in}
		if err := sc.skipValue(); err == nil {
			t.Errorf("skipValue(%q) should fail", in)
		}
	}
}

func TestJSScannerObjectFields(t *testing.T) {
	sc := &jsScanner{s: `{a:1,"b":"two",'c':{x:[1,2]},3:void 0,d:"end"}tail`}
	got := map[string]string{}
	err := sc.objectFields(func(key string) error {
		if key == "b" || key == "d" {
			s, err := sc.readString()
			got[key] = s
			return err
		}
		return errSkipValue
	})
	if err != nil {
		t.Fatal(err)
	}
	if got["b"] != "two" || got["d"] != "end" || len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if sc.s[sc.pos:] != "tail" {
		t.Fatalf("scanner at %q", sc.s[sc.pos:])
	}
}

func TestJSScannerObjectFieldsErrors(t *testing.T) {
	for _, in := range []string{`[1]`, `{a 1}`, `{:1}`, `{a:1`} {
		sc := &jsScanner{s: in}
		if err := sc.objectFields(func(string) error { return errSkipValue }); err == nil {
			t.Errorf("objectFields(%q) should fail", in)
		}
	}
}

func TestJSScannerSkipBalanced(t *testing.T) {
	sc := &jsScanner{s: ` {a:"}",b:{c:1}}(rest)`}
	if err := sc.skipBalanced(); err != nil {
		t.Fatal(err)
	}
	if sc.s[sc.pos:] != "(rest)" {
		t.Fatalf("scanner at %q", sc.s[sc.pos:])
	}
	if err := (&jsScanner{s: "x"}).skipBalanced(); err == nil {
		t.Fatal("skipBalanced must demand a bracket")
	}
}

func TestIsIdentifier(t *testing.T) {
	for in, want := range map[string]bool{"a": true, "_x1": true, "$": true, "中文": true, "": false, "1a": false, "a-b": false, `"s"`: false} {
		if got := isIdentifier(in); got != want {
			t.Errorf("isIdentifier(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFlexInt(t *testing.T) {
	var out struct {
		A, B, C, D, E, F, G flexInt
	}
	in := `{"A":5,"B":"6","C":null,"D":true,"E":false,"F":7.0,"G":""}`
	if err := json.Unmarshal([]byte(in), &out); err != nil {
		t.Fatal(err)
	}
	if out.A != 5 || out.B != 6 || out.C != 0 || out.D != 1 || out.E != 0 || out.F != 7 || out.G != 0 {
		t.Fatalf("out = %+v", out)
	}
	if err := json.Unmarshal([]byte(`{"A":"x"}`), &out); err == nil {
		t.Fatal("non-numeric string must fail")
	}
}

func TestExtractInitialStateStopsAtValueEnd(t *testing.T) {
	raw, err := extractInitialState([]byte(`x window.__INITIAL_STATE__ =  {"a":"};","b":[1]};window.other={"z":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"a":"};","b":[1]}` {
		t.Fatalf("raw = %s", raw)
	}
	if _, err := extractInitialState([]byte("nothing")); err == nil {
		t.Fatal("missing marker must fail")
	}
	if _, err := extractInitialState([]byte("window.__INITIAL_STATE__")); err == nil {
		t.Fatal("missing assignment must fail")
	}
}
