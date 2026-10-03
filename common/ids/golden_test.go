package ids_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

// The ids both sides compute. pi-hydradb-clm and clm/core carry the same
// FNV-1a function in TypeScript, and their own test reads this file: a change
// to one that the other does not make shows up as a failure on both sides.
const goldenPath = "../../testdata/ids.json"

func TestStableMatchesTheSharedGoldenFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Clean(goldenPath))
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]int64{}
	if err := json.Unmarshal(data, &expected); err != nil {
		t.Fatal(err)
	}
	if len(expected) == 0 {
		t.Fatal("the golden file is empty")
	}
	for key, want := range expected {
		if got := ids.Stable(key); got != want {
			t.Errorf("Stable(%q) = %d, want %d", key, got, want)
		}
	}
}
