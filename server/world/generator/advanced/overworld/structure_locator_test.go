package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLocateProjectsRandomSpreadRegions(t *testing.T) {
	g := NewOverworld(0)
	set := configuredStartSet(t, g.modernStructures, "trial_chambers")
	// Candidates observed from Java 26.3 createStructures at these region inputs.
	cases := []struct {
		name, selector string
		origin         cube.Pos
		candidate      world.ChunkPos
	}{
		{"origin", "trial_chambers", cube.Pos{0, 64, 0}, world.ChunkPos{14, 5}},
		{"negative", "minecraft:trial_chambers", cube.Pos{-34 * 16, 64, 17 * 16}, world.ChunkPos{-33, 3}},
		{"distant_tag", "#minecraft:on_buried_trial_chambers_maps", cube.Pos{1024 * 16, 64, -1024 * 16}, world.ChunkPos{1020, -1039}},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			got, found, err := g.FindNearestMapFeature(row.selector, row.origin, 0, nil)
			want := world.StructureLocation{ID: "minecraft:trial_chambers", StartChunk: row.candidate, Position: cube.Pos{int(row.candidate[0]) * 16, 0, int(row.candidate[1]) * 16}}
			if err != nil || !found || got != want {
				t.Fatalf("region locate=(%+v,%v,%v), want %+v", got, found, err, want)
			}
			_, found, err = g.FindNearestMapFeature(row.selector, row.origin, 0, func(location world.StructureLocation) bool { return location.StartChunk == row.candidate })
			if err != nil || found {
				t.Fatalf("referenced start was not excluded: found=%v err=%v", found, err)
			}
			// Locating projects the region, but generation must not duplicate its start
			// for each other chunk visited by Prepare.
			if _, allowed := g.modernStructures.candidate(set, int32(row.origin[0]>>4), int32(row.origin[2]>>4)); allowed {
				t.Fatal("generation accepted a non-candidate chunk")
			}
			if candidate, allowed := g.modernStructures.candidate(set, row.candidate[0], row.candidate[1]); !allowed || candidate != [2]int32(row.candidate) {
				t.Fatalf("generation rejected source candidate %v", row.candidate)
			}
		})
	}
}
