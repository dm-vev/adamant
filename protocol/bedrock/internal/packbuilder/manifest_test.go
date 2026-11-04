package packbuilder

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

// TestManifestNeedsTheOldestGame checks the manifest of the pack needs the
// version of the game it is built for, the oldest clients may join with.
func TestManifestNeedsTheOldestGame(t *testing.T) {
	for version, want := range map[string][3]int{"1.26.50": {1, 26, 50}, "1.26.60": {1, 26, 60}} {
		dir := t.TempDir()
		if err := buildManifest(dir, uuid.New(), uuid.New(), version); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Header struct {
				MinEngineVersion [3]int `json:"min_engine_version"`
			} `json:"header"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Header.MinEngineVersion != want {
			t.Errorf("%s: min_engine_version %v, want %v", version, m.Header.MinEngineVersion, want)
		}
	}
}
