package app

import (
	"reflect"
	"testing"
)

// FuzzParseChapterSpec checks the --chapters parser: it never panics, what it
// resolves is a non-empty, strictly ascending list of positions inside the
// book, and a selection printed with String reads back as the same selection.
// Run it with
//
//	go test ./internal/app -fuzz FuzzParseChapterSpec -fuzztime 30s
func FuzzParseChapterSpec(f *testing.F) {
	for _, seed := range []string{
		"1", "1-3,7,10-", "-3", "5-", "2-2", " 4 , 6 - 8 ", "", ",", "-", "1--2", "3-1", "0", "-0", "+5", "007",
		"99999999999999999999", "1-99999999999", "1,1,1", "a-b", "１-３", "1-3-5",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, in string) {
		spec, err := ParseChapterSpec(in)
		if err != nil {
			return
		}
		for _, total := range []int{1, 3, 50} {
			positions, err := spec.Resolve(total)
			if err != nil {
				continue
			}
			if len(positions) == 0 {
				t.Fatalf("%q selects nothing in a book of %d chapters", in, total)
			}
			prev := -1
			for _, p := range positions {
				if p <= prev || p >= total {
					t.Fatalf("%q resolves to %v in a book of %d chapters", in, positions, total)
				}
				prev = p
			}
		}
		again, err := ParseChapterSpec(spec.String())
		if err != nil || !reflect.DeepEqual(again, spec) {
			t.Fatalf("%q prints as %q, which reads back as %v (%v), want %v", in, spec.String(), again, err, spec)
		}
	})
}
