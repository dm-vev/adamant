package nether

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestNetherTemplateStartsReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_template_starts.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Graphs []struct {
			Seed      int64    `json:"seed"`
			Structure string   `json:"structure"`
			Region    [2]int32 `json:"region_input"`
			Chunk     [2]int32 `json:"chunk"`
			Valid     bool     `json:"valid"`
			Pieces    []struct {
				Box [6]int32 `json:"box"`
				Tag struct {
					Template string `json:"Template"`
					X        int32  `json:"TPX"`
					Y        int32  `json:"TPY"`
					Z        int32  `json:"TPZ"`
					Rot      string `json:"Rot"`
					Rotation string `json:"Rotation"`
					Mirror   string `json:"Mirror"`
				} `json:"tag"`
			} `json:"pieces"`
		} `json:"graphs"`
	}
	if err = json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*Nether{}
	for _, row := range source.Graphs {
		t.Run(fmt.Sprintf("%d/%s/%d,%d", row.Seed, row.Structure, row.Chunk[0], row.Chunk[1]), func(t *testing.T) {
			g := generators[row.Seed]
			if g == nil {
				g = NewNether(row.Seed)
				generators[row.Seed] = g
			}
			var box structure.Box
			var origin [3]int32
			var rotation structure.Rotation
			var mirror structure.Mirror
			var name string
			valid := false
			if row.Structure == "minecraft:nether_fossil" {
				start, err := g.fossilStart(row.Chunk[0], row.Chunk[1])
				if err != nil {
					t.Fatal(err)
				}
				if start != nil {
					valid = true
					box, origin, rotation, name = start.box, start.origin, start.rotation, start.name
				}
			} else {
				start, err := g.portalStart(row.Chunk[0], row.Chunk[1])
				if err != nil {
					t.Fatal(err)
				}
				if start != nil {
					valid = true
					box, origin, rotation, mirror, name = start.box, start.origin, start.rotation, start.mirror, start.templateName
				}
				if row.Region != row.Chunk {
					outside, err := g.portalStart(row.Region[0], row.Region[1])
					if err != nil || outside != nil {
						t.Fatalf("noncandidate generation returned %v, %v", outside, err)
					}
				}
			}
			if valid != row.Valid {
				t.Fatalf("valid=%v source=%v", valid, row.Valid)
			}
			location, found, err := g.FindNearestMapFeature(row.Structure, cube.Pos{int(row.Region[0] * 16), 0, int(row.Region[1] * 16)}, 0, nil)
			if err != nil || found != row.Valid || found && (location.ID != row.Structure || location.StartChunk != world.ChunkPos(row.Chunk)) {
				t.Fatalf("locate=%+v found=%v error=%v source chunk=%v valid=%v", location, found, err, row.Chunk, row.Valid)
			}
			if !valid {
				return
			}
			want := row.Pieces[0]
			actualBox := [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
			if actualBox != want.Box || origin != [3]int32{want.Tag.X, want.Tag.Y, want.Tag.Z} || name != strings.TrimPrefix(want.Tag.Template, "minecraft:") {
				t.Fatalf("geometry box=%v/%v origin=%v/(%d,%d,%d) template=%s/%s", actualBox, want.Box, origin, want.Tag.X, want.Tag.Y, want.Tag.Z, name, want.Tag.Template)
			}
			wantRotation := want.Tag.Rot
			if wantRotation == "" {
				wantRotation = want.Tag.Rotation
			}
			if actual := []string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}[rotation]; actual != wantRotation {
				t.Fatalf("rotation=%s source=%s", actual, wantRotation)
			}
			if want.Tag.Mirror != "" {
				if actual := []string{"NONE", "LEFT_RIGHT", "FRONT_BACK"}[mirror]; actual != want.Tag.Mirror {
					t.Fatalf("mirror=%s source=%s", actual, want.Tag.Mirror)
				}
			}
			for _, point := range []cube.Pos{{int(want.Box[0]), int(want.Box[1]), int(want.Box[2])}, {int(want.Box[3]), int(want.Box[4]), int(want.Box[5])}} {
				if !netherLocateContains(g, row.Structure, world.ChunkPos(row.Chunk), point) {
					t.Fatalf("source bounding box excludes %v", point)
				}
			}
			outside := cube.Pos{int(want.Box[0] - 1), int(want.Box[1]), int(want.Box[2])}
			if netherLocateContains(g, row.Structure, world.ChunkPos(row.Chunk), outside) {
				t.Fatalf("source bounding box includes exterior %v", outside)
			}
		})
	}
}
