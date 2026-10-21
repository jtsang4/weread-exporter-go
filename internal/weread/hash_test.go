package weread

import (
	"encoding/json"
	"os"
	"testing"
)

// The golden vectors were produced by the original Python implementation
// (utils.wr_hash), see testdata/wr_hash_golden.json.
func TestHashMatchesPythonGoldenVectors(t *testing.T) {
	data, err := os.ReadFile("../../testdata/wr_hash_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		In  string `json:"in"`
		Out string `json:"out"`
	}
	if err := json.Unmarshal(data, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 50 {
		t.Fatalf("golden file only has %d vectors", len(vectors))
	}
	for _, v := range vectors {
		got, err := Hash(v.In)
		if err != nil {
			t.Errorf("Hash(%q): %v", v.In, err)
			continue
		}
		if got != v.Out {
			t.Errorf("Hash(%q) = %q, want %q", v.In, got, v.Out)
		}
	}
}

// These ids were confirmed against the live site: the reader URLs built from
// them load the right book / chapter.
func TestHashKnownSiteIDs(t *testing.T) {
	for in, want := range map[string]string{
		"34336681": "08232ac0720befa90825d88",
		"42557145": "f343248072895ed9f34f408",
		"41":       "34132fc02293416a75f431d",
	} {
		if got := MustHash(in); got != want {
			t.Errorf("Hash(%q) = %q, want %q", in, got, want)
		}
	}
	if got := ChapterHash(41); got != "34132fc02293416a75f431d" {
		t.Errorf("ChapterHash(41) = %q", got)
	}
}

func TestHashRejectsNonNumericInput(t *testing.T) {
	for _, in := range []string{"abc", "12a4", "-5", "1.5", " 7"} {
		if got, err := Hash(in); err == nil {
			t.Errorf("Hash(%q) = %q, want an error", in, got)
		}
	}
}

func TestMustHashPanicsOnBadInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustHash(\"x\") did not panic")
		}
	}()
	MustHash("x")
}
