package overworld

import (
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
)

func TestLocateProjectsRandomSpreadRegions(t *testing.T) {
	t.Parallel()
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

func TestOverworldSpawnStructuresFilterAndParity(t *testing.T) {
	t.Parallel()
	g := NewOverworld(0)
	if len(g.modernStructures.spawnSets) != len(spawnOverrideStructures) {
		t.Fatalf("spawnSets len = %d, want %d", len(g.modernStructures.spawnSets), len(spawnOverrideStructures))
	}
	for _, set := range g.modernStructures.spawnSets {
		if !setContainsWanted(set, spawnOverrideStructures) {
			t.Fatalf("spawnSets includes non-override set %q", set.name)
		}
	}

	// Verify known trial_chambers start at chunk {14, 5} (seed 0) is returned
	// by SpawnStructures, and that region-projected lookup matches brute-force
	// [-8, 8] scanning across positive and negative chunk coordinates.
	trialAreas := g.SpawnStructures(world.ChunkPos{14, 5})
	foundTrial := false
	for _, a := range trialAreas {
		if !spawnOverrideStructures[a.ID] {
			t.Fatalf("SpawnStructures returned non-override structure %q", a.ID)
		}
		if a.ID == "minecraft:trial_chambers" {
			foundTrial = true
		}
	}
	if !foundTrial {
		t.Fatalf("expected minecraft:trial_chambers at chunk {14, 5}, got %+v", trialAreas)
	}

	bruteForce := func(pos world.ChunkPos) []world.StructureArea {
		s := g.modernStructures
		var areas []world.StructureArea
		for _, set := range s.sets {
			if !setContainsWanted(set, spawnOverrideStructures) {
				continue
			}
			for dz := int32(-8); dz <= 8; dz++ {
				for dx := int32(-8); dx <= 8; dx++ {
					cand, ok := s.candidate(set, pos[0]+dx, pos[1]+dz)
					if !ok {
						continue
					}
					start, ok := s.makeStart(set, cand[0], cand[1])
					if !ok || !spawnOverrideStructures[start.name] || !intersectsChunk(start.box, pos) {
						continue
					}
					areas = append(areas, g.structureArea(start))
				}
			}
		}
		return areas
	}

	for _, pos := range []world.ChunkPos{{0, 0}, {14, 5}, {-33, 3}, {-16, -16}, {32, -24}} {
		got := g.SpawnStructures(pos)
		want := bruteForce(pos)
		if len(got) != len(want) {
			t.Fatalf("SpawnStructures(%v) len=%d, want %d", pos, len(got), len(want))
		}
		for i := range got {
			if got[i].ID != want[i].ID || got[i].Box != want[i].Box || len(got[i].Pieces) != len(want[i].Pieces) {
				t.Fatalf("SpawnStructures(%v)[%d] = %+v, want %+v", pos, i, got[i], want[i])
			}
		}
	}
}
