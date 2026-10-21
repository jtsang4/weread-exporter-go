package markdown

import (
	"encoding/json"
	"os"
	"testing"
)

// The golden file is produced by the original Python code, see
// testdata/generators/normalize.py: the port has to match it byte for byte.
func TestNormalizeMatchesPythonGolden(t *testing.T) {
	data, err := os.ReadFile("../../testdata/normalize_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name string `json:"name"`
		In   string `json:"in"`
		Out  string `json:"out"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) < 20 {
		t.Fatalf("golden file only has %d cases", len(cases))
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := Normalize(tc.In); got != tc.Out {
				t.Fatalf("Normalize(%q)\n got  %q\n want %q", tc.In, got, tc.Out)
			}
		})
	}
}

func TestNormalizeIsStableOnItsOwnOutput(t *testing.T) {
	in := "# Title\n\nrow one \nrow two\n\n```\ncode\n\nmore\n```\n\n- a\n- b\n"
	once := Normalize(in)
	if twice := Normalize(once); twice != once {
		t.Fatalf("re-normalising changed the text:\nonce  %q\ntwice %q", once, twice)
	}
}
