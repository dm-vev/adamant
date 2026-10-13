package block_test

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

func TestStrawBedCollisionMatchesJava263Source(t *testing.T) {
	var fixture struct {
		Cases []struct {
			Facing string                          `json:"facing"`
			Part   string                          `json:"part"`
			Boxes  []struct{ Min, Max [3]float64 } `json:"boxes"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/java263_straw_bed_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Facing+"/"+tc.Part, func(t *testing.T) {
			direction := cube.North
			for _, d := range cube.Directions() {
				if d.String() == tc.Facing {
					direction = d
				}
			}
			var want []cube.BBox
			for _, b := range tc.Boxes {
				want = append(want, cube.Box(b.Min[0], b.Min[1], b.Min[2], b.Max[0], b.Max[1], b.Max[2]))
			}
			bed := block.StrawBed{Facing: direction, Head: tc.Part == "head"}
			if got := bed.Model().BBox(cube.Pos{}, nil); !reflect.DeepEqual(got, want) {
				t.Fatalf("collision=%v Source=%v", got, want)
			}
		})
	}
}

func TestStrawBedNativeStatesSurviveMCDBRestart(t *testing.T) {
	dir := t.TempDir()
	db, err := mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() {
		if w != nil {
			_ = w.Close()
		}
	})
	var states []block.StrawBed
	for _, direction := range cube.Directions() {
		for _, head := range [...]bool{false, true} {
			for _, occupied := range [...]bool{false, true} {
				states = append(states, block.StrawBed{Facing: direction, Head: head, Occupied: occupied})
			}
		}
	}
	job := w.Do(func(tx *world.Tx) {
		for index, state := range states {
			tx.SetBlock(cube.Pos{257 + index, 70, -257}, state, nil)
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
	w.Save()
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w = nil
	db, err = mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w = world.Config{Provider: db, Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	job = w.Do(func(tx *world.Tx) {
		for index, state := range states {
			pos := cube.Pos{257 + index, 70, -257}
			got, ok := tx.Block(pos).(block.StrawBed)
			if !ok || got != state {
				t.Errorf("native state at %v=%v want=%v", pos, tx.Block(pos), state)
			}
			if _, actor := tx.Block(pos).(world.NBTer); actor {
				t.Error("Source straw bed acquired a block actor")
			}
		}
		t.Log("actual World/MCDB reopened all16 native direction/part/occupancy states without block actors")
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestStrawBedNeighborRemovalDropsOnlyHead(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	foot, head := cube.Pos{257, 70, -257}, cube.Pos{257, 70, -256}
	job := w.Do(func(tx *world.Tx) {
		tx.SetBlock(foot, block.StrawBed{Facing: cube.South}, nil)
		tx.SetBlock(head, block.StrawBed{Facing: cube.South, Head: true}, nil)
		tx.SetBlock(foot, nil, nil)
		tx.Block(head).(block.StrawBed).NeighbourUpdateTick(head, foot, tx)
		if _, air := tx.Block(head).(block.Air); !air {
			t.Error("orphaned head survived")
		}
		count := 0
		for e := range tx.Entities() {
			if drop, ok := e.(*entity.Ent); ok {
				if behavior, ok := drop.Behaviour().(*entity.ItemBehaviour); ok {
					stack := behavior.Item()
					if _, ok := stack.Item().(block.StrawBed); ok {
						count += stack.Count()
					}
				}
			}
		}
		if count != 1 {
			t.Fatalf("broken foot dropped %d straw beds, Sourcehead loot=1", count)
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}
