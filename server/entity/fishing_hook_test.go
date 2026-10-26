package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestFishingHookCatchesAsJava follows FishingHook: a hook bobbing in open
// water gets a bite within the 600 ticks of luring and 80 of approach, and
// retrieving it then lands a catch and wears the rod down by one.
func TestFishingHookCatchesAsJava(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 60, z}, block.Stone{}, nil)
				for y := 61; y <= 63; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Water{Still: true, Depth: 8}, nil)
				}
			}
		}
		p := addSurvivalPlayer(tx, mgl64.Vec3{6.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.FishingRod{}, 1), item.Stack{})
		hookHandle := world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 63.9, .5}}.New(entity.FishingHookType, entity.FishingHookConfig{Owner: p.H(), Rod: item.NewStack(item.FishingRod{}, 1)})
		hook := tx.AddEntity(hookHandle).(*entity.Ent)
		behaviour := hook.Behaviour().(*entity.FishingHookBehaviour)
		bit := false
		for range 800 {
			hook.Tick(tx, 0)
			if behaviour.Biting() {
				bit = true
				break
			}
		}
		if !bit {
			t.Fatal("no bite within 800 ticks in open water")
		}
		dmg, err := behaviour.Retrieve(hook, tx)
		if err != nil || dmg != 1 {
			t.Fatalf("retrieving a bite: durability %v, err %v; want 1", dmg, err)
		}
		caught := false
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:item" {
				caught = true
			}
		}
		if !caught {
			t.Fatal("no catch landed")
		}
	})
}
