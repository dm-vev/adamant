package world

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestLightningSearchDoesNotLoadAnUnloadedIndexedRod(t *testing.T) {
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	w.StartRaining(time.Hour)
	<-w.Exec(func(tx *Tx) {
		// The persisted POI index may outlive its loaded terrain. Native block
		// enumeration only visits available chunks, and must not resurrect it.
		pos := cube.Pos{1, 100, 1}
		w.pois[pos] = POI{Kind: Other, Type: "minecraft:lightning_rod"}
		if loaded, _ := tx.ChunkState(ChunkPos{}); loaded {
			t.Fatal("fixture unexpectedly loaded terrain")
		}
		if got, ok := tx.findLightningRod(cube.Pos{0, 100, 0}); ok {
			t.Errorf("selected unloaded rod %v", got)
		}
		if loaded, _ := tx.ChunkState(ChunkPos{}); loaded {
			t.Fatal("rod query loaded indexed terrain")
		}
	})
}

func TestLightningCylinderAndOrderingMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_lightning_search.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Cases    []struct {
			Center     cube.Pos
			Candidates []struct {
				Pos      cube.Pos
				Loaded   *bool
				Material int
			}
			Order [][4]int `json:"candidate_order"`
		}
		SortCases []struct {
			Distances []uint32
			Ideal     int
			Order     []int
		} `json:"sort_cases"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Cases) != 670 || len(native.SortCases) != 120 {
		t.Fatal("incomplete native cylinder/sort capture")
	}
	for i, c := range native.Cases {
		t.Run(fmt.Sprintf("cylinder%d", i), func(t *testing.T) {
			var positions []cube.Pos
			for _, v := range c.Candidates {
				if v.Material < 8 && (v.Loaded == nil || *v.Loaded) {
					positions = append(positions, v.Pos)
				}
			}
			got := orderLightningRods(c.Center, positions)
			if len(got) != len(c.Order) {
				t.Fatalf("candidate count %d, native %d", len(got), len(c.Order))
			}
			for j, want := range c.Order {
				if got[j].pos != (cube.Pos{want[0], want[1], want[2]}) || got[j].distance != uint32(want[3]) {
					t.Errorf("candidate %d=%+v, native=%v", j, got[j], want)
				}
			}
		})
	}
	for i, c := range native.SortCases {
		t.Run(fmt.Sprintf("sort%d/count%d/ideal%d", i, len(c.Distances), c.Ideal), func(t *testing.T) {
			values := make([]lightningRodCandidate, len(c.Distances))
			for j, distance := range c.Distances {
				values[j] = lightningRodCandidate{pos: cube.Pos{j, 0, 0}, distance: distance}
			}
			sortLightningRods(values, c.Ideal)
			if len(c.Order) != len(values) {
				t.Fatal("missing native sort output")
			}
			for j, want := range c.Order {
				if values[j].pos[0] != want {
					t.Fatalf("entry %d=%d, native=%d", j, values[j].pos[0], want)
				}
			}
		})
	}
}
