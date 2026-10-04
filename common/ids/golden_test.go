package ids_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/publicdomainrelay/graph-clm-kcp-spec/common/ids"
)

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
