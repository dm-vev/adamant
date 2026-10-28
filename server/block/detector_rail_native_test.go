package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestDetectorRailQueryMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_detector_query.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Position cube.Pos
			Kind     uint32
			Bounds   [6]float64
			Excluded uint64
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 324 {
		t.Fatal("incomplete executed-client rail query capture")
	}
	for _, c := range native.Scenarios {
		if c.Kind != 0x80000 || c.Excluded != 0 {
			t.Fatal("unexpected native query filter")
		}
		box := detectorRailDetectionBox(c.Position)
		lo, hi := box.Min(), box.Max()
		if got := [6]float64{lo[0], lo[1], lo[2], hi[0], hi[1], hi[2]}; got != c.Bounds {
			t.Errorf("at %v: bounds=%v, client=%v", c.Position, got, c.Bounds)
		}
	}
}
