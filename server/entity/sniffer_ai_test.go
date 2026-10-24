package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// snifferTestWorld returns a world with a field of grass and a sniffer on
// it.
func snifferTestWorld(t *testing.T) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	var sniffer *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -24; x <= 24; x++ {
			for z := -24; z <= 24; z++ {
				tx.SetBlock(cube.Pos{x, 62, z}, block.Dirt{}, nil)
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
			}
		}
		sniffer = tx.AddEntity(NewSniffer(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}})).H()
	})
	return w, sniffer
}

func snifferOf(tx *world.Tx, h *world.EntityHandle) *Sniffer {
	e, _ := h.Entity(tx)
	return e.(*Sniffer)
}

// TestSniffersDigUpSeeds checks Sniffing, Searching, Digging and
// FinishedDigging: a sniffer on grass sniffs out a block, walks to it, digs
// up a seed, remembers the block and rests from sniffing.
func TestSniffersDigUpSeeds(t *testing.T) {
	w, h := snifferTestWorld(t)
	seen := map[snifferState]bool{}
	for range 12000 {
		stepTicks(t, w, 1)
		dug := false
		<-w.Exec(func(tx *world.Tx) {
			seen[snifferOf(tx, h).behaviour().state] = true
			for e := range tx.Entities() {
				if i, ok := e.(*Ent); ok && i.H().Type() == ItemType {
					dug = true
				}
			}
		})
		if dug {
			for _, st := range []snifferState{snifferSniffing, snifferSearching, snifferDigging} {
				if !seen[st] {
					t.Fatalf("sniffer dug up a seed without passing state %v", st)
				}
			}
			// The seed comes up 120 ticks into the dig, which lasts up to 180
			// ticks before the sniffer rises for 40.
			stepTicks(t, w, 120)
			<-w.Exec(func(tx *world.Tx) {
				b := snifferOf(tx, h).behaviour()
				if len(b.explored()) != 1 || !b.br.has(memSniffCooldown) {
					t.Fatalf("sniffer done digging remembers %d blocks, cooldown %v, want 1 and a cooldown", len(b.explored()), b.br.has(memSniffCooldown))
				}
			})
			return
		}
	}
	t.Fatal("sniffer on grass dug up nothing in ten minutes")
}

// TestHurtSniffersStopDigging checks the AnimalPanic of SnifferAi: a sniffer
// hurt while it digs stops and runs.
func TestHurtSniffersStopDigging(t *testing.T) {
	w, h := snifferTestWorld(t)
	stepTicks(t, w, 2)
	<-w.Exec(func(tx *world.Tx) {
		s := snifferOf(tx, h)
		b := s.behaviour()
		b.br.set(memSnifferDigging, true)
		b.transitionTo(b.ai.Mob(s.Ent, s), snifferDigging)
		s.Hurt(1, AttackDamageSource{})
	})
	stepTicks(t, w, 25)
	<-w.Exec(func(tx *world.Tx) {
		s := snifferOf(tx, h)
		if s.Digging() || s.behaviour().br.has(memSnifferDigging) || !s.Panicking() {
			t.Fatalf("hurt sniffer digging %v, panicking %v, want it running", s.Digging(), s.Panicking())
		}
	})
}

// TestSniffersSaveExploredBlocks checks that the blocks a sniffer dug up
// stay remembered once it is saved and loaded.
func TestSniffersSaveExploredBlocks(t *testing.T) {
	data := &world.EntityData{}
	SnifferConfig{Explored: []cube.Pos{{1, 2, 3}, {4, 5, 6}}}.Apply(data)
	loaded := &world.EntityData{}
	SnifferType.DecodeNBT(SnifferType.EncodeNBT(data), loaded)
	if got := loaded.Data.(*SnifferBehaviour).explored(); len(got) != 2 || got[0] != (cube.Pos{1, 2, 3}) || got[1] != (cube.Pos{4, 5, 6}) {
		t.Fatalf("loaded sniffer remembers %v", got)
	}
}
