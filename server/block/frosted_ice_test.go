package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// TestFrostedIceMeltsInStages checks frosted ice ages a stage at a time
// and melts into water once it is three stages old, and that ice next to
// it with fewer than two frosted neighbours melts with it.
func TestFrostedIceMeltsInStages(t *testing.T) {
	w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlock(pos, FrostedIce{}, nil)
		for age := 0; age < 3; age++ {
			if (FrostedIce{Age: age}).slightlyMelt(pos, tx) {
				t.Fatalf("ice of age %d melted, want it to age", age)
			}
			if got := tx.Block(pos); got != (FrostedIce{Age: age + 1}) {
				t.Fatalf("ice of age %d became %#v", age, got)
			}
		}
		lone := pos.Side(cube.FaceEast)
		tx.SetBlock(lone, FrostedIce{}, nil)
		if !(FrostedIce{Age: 3}).slightlyMelt(pos, tx) {
			t.Fatal("ice of age 3 did not melt")
		}
		if got := tx.Block(pos); got != (Water{Depth: 8, Still: true}) {
			t.Fatalf("melted ice became %#v, want still water", got)
		}
		FrostedIce{}.NeighbourUpdateTick(lone, pos, tx)
		if got := tx.Block(lone); got != (Water{Depth: 8, Still: true}) {
			t.Fatalf("ice left without frosted neighbours became %#v, want it melted", got)
		}
	})
}

func TestFrostedIceInterfacesAndEncoding(t *testing.T) {
	var ice any = FrostedIce{Age: 2}
	if _, ok := ice.(world.RandomTicker); !ok {
		t.Fatal("FrostedIce does not implement world.RandomTicker")
	}
	if _, ok := ice.(world.ScheduledTicker); !ok {
		t.Fatal("FrostedIce does not implement world.ScheduledTicker")
	}
	name, states := ice.(FrostedIce).EncodeBlock()
	if name != "minecraft:frosted_ice" || states["age"] != int32(2) {
		t.Fatalf("EncodeBlock() = %q, %#v", name, states)
	}
}

func TestFrostedIceNeighbourLimit(t *testing.T) {
	w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Provider: world.NopProvider{}}.New()
	defer w.Close()

	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		for i, face := range cube.Faces()[:4] {
			tx.SetBlock(pos.Side(face), FrostedIce{}, nil)
			if got := fewerFrostedIceNeighbours(pos, tx, 4); got != (i < 3) {
				t.Fatalf("fewerFrostedIceNeighbours() after %d neighbours = %t", i+1, got)
			}
		}
	})
}
