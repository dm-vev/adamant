package entity_test

import (
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// heldViewer is a viewer that records what mobs hold in their main hand each
// time it is shown, as the packet of a client would.
type heldViewer struct {
	world.NopViewer
	mu    sync.Mutex
	shown []heldShown
}

type heldShown struct {
	entity string
	main   string
}

func (v *heldViewer) ViewEntityItems(e world.Entity) {
	c, ok := e.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return
	}
	main, _ := c.HeldItems()
	name := ""
	if !main.Empty() {
		name, _ = main.Item().EncodeItem()
	}
	v.mu.Lock()
	v.shown = append(v.shown, heldShown{e.H().Type().EncodeEntity(), name})
	v.mu.Unlock()
}

// last returns what the last items shown of an entity type held.
func (v *heldViewer) last(entity string) (string, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	for i := len(v.shown) - 1; i >= 0; i-- {
		if v.shown[i].entity == entity {
			return v.shown[i].main, true
		}
	}
	return "", false
}

// TestMobsShowViewersTheirHeldItems checks that viewers are shown what a
// copper golem carries and an allay holds, as BDS does with MobEquipment: when
// they see the mob, and each time it changes.
func TestMobsShowViewersTheirHeldItems(t *testing.T) {
	t.Parallel()
	w := newDeathTestWorld(t)
	viewer := &heldViewer{}
	loader := world.NewLoader(2, w, viewer)
	t.Cleanup(func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }) })

	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{0.5, 64, 0.5}
		loader.Move(tx, pos)
		loader.Load(tx, 9)
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		player := addDeathTestPlayer(tx)

		golem, err := entity.NewFromStructureNBT(map[string]any{
			"id":          "minecraft:copper_golem",
			"CarriedItem": item.WriteNBT(item.NewStack(item.Apple{}, 1)),
		}, world.EntitySpawnOpts{Position: pos, SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		g := tx.AddEntity(golem).(*entity.CopperGolem)
		if got, ok := viewer.last("minecraft:copper_golem"); !ok || got != "minecraft:apple" {
			t.Errorf("a copper golem carrying an apple is shown holding %q (shown=%t), want minecraft:apple", got, ok)
		}
		// An empty hand takes the item the golem carries.
		g.Interact(tx, player, &item.UseContext{})
		if got, _ := viewer.last("minecraft:copper_golem"); got != "" {
			t.Errorf("a copper golem that was relieved of its item is shown holding %q, want nothing", got)
		}

		allay, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:allay"}, world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{2, 1, 0}), SpawnFinalized: true})
		if err != nil {
			t.Fatal(err)
		}
		a := tx.AddEntity(allay).(*entity.Allay)
		a.SetHeldItems(item.NewStack(item.Diamond{}, 1), item.Stack{})
		if got, _ := viewer.last("minecraft:allay"); got != "minecraft:diamond" {
			t.Errorf("an allay given a diamond is shown holding %q, want minecraft:diamond", got)
		}
		a.SetHeldItems(item.Stack{}, item.Stack{})
		if got, _ := viewer.last("minecraft:allay"); got != "" {
			t.Errorf("an allay that lost its diamond is shown holding %q, want nothing", got)
		}
	})
}
