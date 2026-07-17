package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestFrostedIceDecay(t *testing.T) {
	for age := 0; age < 3; age++ {
		next, melted := decayFrostedIce(FrostedIce{Age: age})
		if melted || next != (FrostedIce{Age: age + 1}) {
			t.Fatalf("age %d decayed to %#v, melted %t", age, next, melted)
		}
	}
	next, melted := decayFrostedIce(FrostedIce{Age: 3})
	if !melted || next != (Water{Depth: 8, Still: true}) {
		t.Fatalf("age 3 decayed to %#v, melted %t", next, melted)
	}
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
