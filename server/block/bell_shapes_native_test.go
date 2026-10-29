package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestBellShapesMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/client_1_26_51_bell_shapes.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Cases    []struct {
			Attachment, Direction int
			Box                   [6]float64
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Cases) != 16 {
		t.Fatal("incomplete executed native bell shapes")
	}
	world.DefaultBlockRegistry.Finalize()
	attachments := [4]string{"standing", "hanging", "side", "multiple"}
	for _, c := range native.Cases {
		b, ok := world.DefaultBlockRegistry.BlockByName("minecraft:bell", map[string]any{"attachment": attachments[c.Attachment], "direction": int32(c.Direction), "toggle_bit": uint8(0)})
		if !ok {
			t.Fatal("native bell shape permutation missing")
		}
		boxes := b.Model().BBox([3]int{}, nil)
		if len(boxes) != 1 {
			t.Fatal("native bell shape is a single AABB")
		}
		lo, hi := boxes[0].Min(), boxes[0].Max()
		got := [6]float64{lo[0], lo[1], lo[2], hi[0], hi[1], hi[2]}
		if got != c.Box {
			t.Fatalf("attachment %d direction %d shape=%v, native=%v", c.Attachment, c.Direction, got, c.Box)
		}
	}
}
