package overworld

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func TestMansionGraphsMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_mansion_graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Seed     string                 `json:"seed"`
		Chunk    [2]int32               `json:"chunk"`
		Rotation javastructure.Rotation `json:"rotation"`
		Pieces   []struct {
			Name     string                 `json:"name"`
			Position [3]int32               `json:"position"`
			Rotation javastructure.Rotation `json:"rotation"`
			Mirror   javastructure.Mirror   `json:"mirror"`
			BBox     [6]int32               `json:"bbox"`
		} `json:"pieces"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		random := largeFeatureRandom(seed, fixture.Chunk[0], fixture.Chunk[1])
		rotation := javastructure.Rotation(random.NextInt(4))
		if rotation != fixture.Rotation {
			t.Fatalf("seed %s rotation=%d, Source=%d", fixture.Seed, rotation, fixture.Rotation)
		}
		plan := newMansionPlan(&random)
		placer := newMansionPlacer([3]int32{-13, 70, 19}, rotation, seed, &random)
		if err = placer.create(plan); err != nil {
			t.Fatalf("seed %s: %v", fixture.Seed, err)
		}
		if len(placer.pieces) != len(fixture.Pieces) {
			t.Errorf("seed %s pieces=%d, Source=%d", fixture.Seed, len(placer.pieces), len(fixture.Pieces))
		}
		for i, want := range fixture.Pieces[:min(len(placer.pieces), len(fixture.Pieces))] {
			got := placer.pieces[i].(modernMansionPiece)
			name := ""
			for n, template := range placer.templates {
				if template == got.template {
					name = n
					break
				}
			}
			box := got.Bounds()
			bounds := [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
			if name != want.Name || got.origin != want.Position || got.rotation != want.Rotation || got.mirror != want.Mirror || bounds != want.BBox {
				t.Errorf("seed %s piece %d=%s at %v rotation=%d mirror=%d bbox=%v, Source=%s at %v rotation=%d mirror=%d bbox=%v", fixture.Seed, i, name, got.origin, got.rotation, got.mirror, bounds, want.Name, want.Position, want.Rotation, want.Mirror, want.BBox)
				break
			}
		}
	}
}
