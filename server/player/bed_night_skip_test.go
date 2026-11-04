package player

import (
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// nativeOnlyBlock returns the block of a native state that has no Java
// block state, failing the test if it has one.
func nativeOnlyBlock(t *testing.T, name string, properties map[string]any) world.Block {
	t.Helper()
	b, ok := world.BlockByName(name, properties)
	if !ok {
		t.Fatalf("no native block %s%v", name, properties)
	}
	if _, err := world.CanonicalStateOf(b); err == nil {
		t.Fatalf("%s%v has a Java block state", name, properties)
	}
	return b
}

// A night skipped wakes a player next to blocks without a Java block state,
// as leaf litter of more segments than Java has, or a carpet holding water, which Java cannot
// waterlog. The stand-up search reads them by their collision shape, as it
// reads any block, instead of panicking on the missing state: the owner's
// server crashed waking from a bed next to one. The player stands up where
// Java stands it up next to the same block.
func TestNightSkipWakesBesideBlocksWithoutJavaState(t *testing.T) {
	for _, tc := range []struct {
		name string
		// place puts the blocks at the first stand-up candidate.
		place func(tx *world.Tx, pos cube.Pos)
		// passable is whether the player stands up in the candidate.
		passable bool
		floor    float64
	}{
		{name: "leaf litter of five segments", passable: true, place: func(tx *world.Tx, pos cube.Pos) {
			tx.SetBlock(pos, nativeOnlyBlock(t, "minecraft:leaf_litter", map[string]any{"growth": int32(5), "minecraft:cardinal_direction": "east"}), nil)
		}},
		{name: "wall hanging sign facing down", place: func(tx *world.Tx, pos cube.Pos) {
			tx.SetBlock(pos, nativeOnlyBlock(t, "minecraft:oak_hanging_sign", map[string]any{"attached_bit": uint8(0), "facing_direction": int32(0), "ground_sign_direction": int32(0), "hanging": uint8(0)}), nil)
		}},
		{name: "ceiling skull below", passable: true, place: func(tx *world.Tx, pos cube.Pos) {
			tx.SetBlock(pos.Side(cube.FaceDown), nativeOnlyBlock(t, "minecraft:skeleton_skull", map[string]any{"facing_direction": int32(0)}), nil)
		}},
		{name: "carpet holding water", passable: true, floor: 0.0625, place: func(tx *world.Tx, pos cube.Pos) {
			tx.SetBlock(pos, block.Carpet{}, nil)
			tx.SetLiquid(pos, block.Water{Still: true, Depth: 8})
			if _, err := tx.BlockStateID(pos); err == nil {
				t.Fatal("a carpet holding water has a Java block state")
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var faults []world.Fault
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry, Fault: func(f world.Fault) {
				faults = append(faults, f)
			}}.New()
			loader := world.NewLoader(2, w, clienttest.New())
			t.Cleanup(func() {
				<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
				_ = w.Close()
			})
			w.SetTime(18000)
			foot, head := cube.Pos{0, 65, 0}, cube.Pos{0, 65, -1}
			c := clienttest.New()
			handle := world.EntitySpawnOpts{Position: foot.Vec3Middle(), ID: uuid.New()}.New(Type, Config{Name: "Alex", Position: foot.Vec3Middle(), GameMode: world.GameModeSurvival, Client: c})
			var first mgl64.Vec3
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, foot.Vec3Middle())
				loader.Load(tx, 25)
				for x := -4; x <= 4; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(foot, block.Bed{Facing: cube.North}, nil)
				tx.SetBlock(head, block.Bed{Facing: cube.North, Head: true}, nil)
				var ok bool
				if first, ok = bedStandUpPosition(tx, head, cube.North, 0); !ok {
					t.Fatal("no stand-up position around a free bed")
				}
				tc.place(tx, cube.PosFromVec3(first))
				p := tx.AddEntity(handle).(*Player)
				p.Sleep(head)
				if _, sleeping := p.Sleeping(); !sleeping {
					t.Fatal("the player did not sleep")
				}
			})
			for range 200 {
				advance(t, w)
				awake := false
				<-w.Exec(func(tx *world.Tx) {
					e, ok := handle.Entity(tx)
					if !ok {
						t.Fatal("the player left the world")
					}
					_, sleeping := e.(*Player).Sleeping()
					awake = !sleeping
				})
				if awake {
					break
				}
			}
			if len(faults) != 0 {
				t.Fatalf("faults = %v, want none", brief(faults))
			}
			if w.Time()%world.TimeFull >= 18000 {
				t.Fatalf("time = %d: the night was not skipped", w.Time())
			}
			<-w.Exec(func(tx *world.Tx) {
				e, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("the player left the world")
				}
				p := e.(*Player)
				if _, sleeping := p.Sleeping(); sleeping {
					t.Fatal("the player is still asleep")
				}
				got := p.Position()
				if tc.passable {
					if want := first.Add(mgl64.Vec3{0, tc.floor}); got != want {
						t.Fatalf("stood up at %v, want %v", got, want)
					}
				} else if got == first || got == (head.Vec3Middle().Add(mgl64.Vec3{0, 1.1})) {
					t.Fatalf("stood up at %v, in the block or on the bed", got)
				}
			})
		})
	}
}
