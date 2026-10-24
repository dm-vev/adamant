package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func addPanda(tx *world.Tx, pos mgl64.Vec3, conf entity.PandaConfig) *world.EntityHandle {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.PandaType, conf)).H()
}

func pandaOf(tx *world.Tx, h *world.EntityHandle) *entity.Panda {
	e, _ := h.Entity(tx)
	return e.(*entity.Panda)
}

// waitForPanda advances the world until cond holds for the panda or ticks
// run out, and reports if it held.
func waitForPanda(w *world.World, h *world.EntityHandle, ticks int, cond func(*entity.Panda) bool) bool {
	for range ticks {
		w.AdvanceTick()
		held := false
		<-w.Exec(func(tx *world.Tx) { held = cond(pandaOf(tx, h)) })
		if held {
			return true
		}
	}
	return false
}

// TestPandasSitToEatBamboo checks PandaSitGoal, Panda.pickUpItem and
// handleEating: a panda picks up bamboo off the ground, sits down with it
// and in the end eats it. A panda that gets up before it eats drops the
// bamboo and waits up to 160 seconds to pick it up again, and it finishes
// only a third of the bites it starts, so eating may take it many minutes.
func TestPandasSitToEatBamboo(t *testing.T) {
	w := wolfTestWorld(t)
	var panda *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		panda = addPanda(tx, mgl64.Vec3{.5, 64, .5}, entity.PandaConfig{})
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, .5}}, item.NewStack(block.Bamboo{}, 1)))
	})
	if !waitForPanda(w, panda, 600, func(p *entity.Panda) bool {
		held, _ := p.HeldItems()
		return p.Sitting() && !held.Empty()
	}) {
		t.Fatal("panda did not pick up bamboo nearby and sit down with it in half a minute")
	}
	for range 36000 {
		w.AdvanceTick()
		eaten := false
		<-w.Exec(func(tx *world.Tx) {
			if held, _ := pandaOf(tx, panda).HeldItems(); !held.Empty() {
				return
			}
			for e := range tx.Entities() {
				if e.H().Type() == entity.ItemType {
					return
				}
			}
			eaten = true
		})
		if eaten {
			return
		}
	}
	t.Fatal("panda did not eat its bamboo in half an hour")
}

// TestPlayfulPandasRoll checks PandaRollGoal and Panda.handleRoll: a
// playful panda now and then rolls over.
func TestPlayfulPandasRoll(t *testing.T) {
	w := wolfTestWorld(t)
	var panda *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		panda = addPanda(tx, mgl64.Vec3{.5, 64, .5}, entity.PandaConfig{Variant: 3, Gene: 3, HiddenGene: 3})
	})
	if !waitForPanda(w, panda, 600, (*entity.Panda).Rolling) {
		t.Fatal("playful panda did not roll in half a minute")
	}
	if !waitForPanda(w, panda, 40, func(p *entity.Panda) bool { return !p.Rolling() }) {
		t.Fatal("panda did not stop rolling after 32 ticks")
	}
}

// TestLazyPandasLieOnTheirBacks checks PandaLieOnBackGoal: a lazy panda now
// and then lies on its back.
func TestLazyPandasLieOnTheirBacks(t *testing.T) {
	w := wolfTestWorld(t)
	var panda *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		panda = addPanda(tx, mgl64.Vec3{.5, 64, .5}, entity.PandaConfig{Variant: 1, Gene: 1, HiddenGene: 1})
	})
	if !waitForPanda(w, panda, 4800, (*entity.Panda).LyingOnBack) {
		t.Fatal("lazy panda did not lie on its back in four minutes")
	}
}

// TestPandasBiteBackOnce checks PandaHurtByTargetGoal and
// Panda.doHurtTarget: a panda that is not aggressive bites what hurt it once
// and then forgets it.
func TestPandasBiteBackOnce(t *testing.T) {
	w := wolfTestWorld(t)
	var panda, player *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		player = p.H()
		panda = addPanda(tx, mgl64.Vec3{.5, 64, .5}, entity.PandaConfig{})
		pandaOf(tx, panda).Hurt(1, entity.AttackDamageSource{Attacker: p})
	})
	bites := 0
	last := 20.0
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			p, _ := player.Entity(tx)
			if h := p.(entity.Living).Health(); h < last {
				bites++
				last = h
			}
		})
	}
	if bites != 1 {
		t.Fatalf("panda bit its attacker %d times, want once", bites)
	}
}

// TestWeakCubsSneeze checks PandaSneezeGoal: a weak cub now and then
// sneezes.
func TestWeakCubsSneeze(t *testing.T) {
	w := wolfTestWorld(t)
	var panda *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		panda = addPanda(tx, mgl64.Vec3{.5, 64, .5}, entity.PandaConfig{Age: -24000, Variant: 5, Gene: 5, HiddenGene: 5})
	})
	if !waitForPanda(w, panda, 2400, (*entity.Panda).Sneezing) {
		t.Fatal("weak cub did not sneeze in two minutes")
	}
}
