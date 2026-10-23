package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestMansionEntranceBoundaryMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_mansion_boundary.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Seed     string             `json:"seed"`
		Rotation structure.Rotation `json:"rotation"`
		BaseGrid [11][11]int        `json:"baseGrid"`
		Pieces   []struct {
			Name     string             `json:"name"`
			Position [3]int32           `json:"position"`
			Rotation structure.Rotation `json:"rotation"`
			BBox     [6]int32           `json:"bbox"`
		} `json:"pieces"`
		FinalPosition [3]int32           `json:"finalPosition"`
		FinalRotation structure.Rotation `json:"finalRotation"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("seed_%s/rotation_%d", fixture.Seed, fixture.Rotation), func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			random := largeFeatureRandom(seed, 83, 30)
			random.NextInt(4)
			plan := newMansionPlan(&random)
			// A missing entrance column makes the prescribed traversal endpoint unreachable.
			// Fail before traversing, so that regression reports the graph error rather than hanging.
			for y, row := range fixture.BaseGrid {
				for x, want := range row {
					if got := plan.base.get(x, y); got != want {
						t.Fatalf("base grid (%d,%d)=%d, Source=%d", x, y, got, want)
					}
				}
			}
			placer := newMansionPlacer([3]int32{-13, 70, 19}, fixture.Rotation, seed, &random)
			placement := mansionPlacementData{pos: placer.origin, rotation: fixture.Rotation, wall: "wall_flat"}
			if err = placer.outerWalls(&placement, plan.base, mansionSouth, plan.sx, plan.sy, plan.sx, plan.sy-1); err != nil {
				t.Fatal(err)
			}
			if len(placer.pieces) != len(fixture.Pieces) {
				t.Fatalf("boundary pieces=%d, Source=%d", len(placer.pieces), len(fixture.Pieces))
			}
			for i, want := range fixture.Pieces {
				got := placer.pieces[i].(modernMansionPiece)
				box := got.Bounds()
				bounds := [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
				name := ""
				for n, template := range placer.templates {
					if template == got.template {
						name = n
						break
					}
				}
				if name != want.Name || got.origin != want.Position || got.rotation != want.Rotation || bounds != want.BBox {
					t.Errorf("boundary piece%d=%s at%v rotation%d bbox%v, Source=%+v", i, name, got.origin, got.rotation, bounds, want)
				}
			}
			if placement.pos != fixture.FinalPosition || placement.rotation != fixture.FinalRotation {
				t.Errorf("boundary endpoint=%v rotation%d, Source=%v rotation%d", placement.pos, placement.rotation, fixture.FinalPosition, fixture.FinalRotation)
			}
		})
	}
}
