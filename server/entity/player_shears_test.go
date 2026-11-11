package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// soundNameRecorder records the names of the custom sounds a world plays.
type soundNameRecorder struct {
	world.NopHandler
	names []string
}

func (r *soundNameRecorder) HandleSound(_ *world.Context, s world.Sound, _ mgl64.Vec3) {
	if c, ok := s.(sound.Custom); ok {
		r.names = append(r.names, c.Name)
	}
}

// playerWithShears adds a survival player holding shears next to the origin.
func playerWithShears(tx *world.Tx) interface {
	UseItemOnEntity(world.Entity) bool
	HeldItems() (item.Stack, item.Stack)
} {
	p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, .5})
	p.SetHeldItems(item.NewStack(item.Shears{}, 1), item.Stack{})
	return p
}

// TestPlayerShearsWhatJavaShears checks the mobInteract of each Shearable in
// 26.3: a player with shears shears a mooshroom, a snow golem, a copper golem,
// a sulfur cube and a sheep, each with its own sound and drops, and the shears
// wear down by one. A mob that is not ready is left alone and the shears keep
// their durability.
func TestPlayerShearsWhatJavaShears(t *testing.T) {
	t.Parallel()
	at := world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}
	for _, c := range []struct {
		name  string
		spawn func() *world.EntityHandle
		sound string
		drop  string
		// done reports whether the mob was sheared.
		done func(tx *world.Tx, e world.Entity) bool
	}{
		{"sheep", func() *world.EntityHandle { return entity.NewSheep(at) }, "entity.sheep.shear", "minecraft:white_wool",
			func(_ *world.Tx, e world.Entity) bool { return e.(*entity.Sheep).Sheared() }},
		{"mooshroom", func() *world.EntityHandle { return entity.NewMooshroom(at) }, "entity.mooshroom.shear", "minecraft:red_mushroom",
			func(tx *world.Tx, _ world.Entity) bool {
				return len(entitiesOfType(tx, entity.MooshroomType)) == 0 && len(entitiesOfType(tx, entity.CowType)) == 1
			}},
		{"snow golem", func() *world.EntityHandle { return entity.NewSnowGolem(at) }, "entity.snow_golem.shear", "minecraft:carved_pumpkin",
			func(_ *world.Tx, e world.Entity) bool { return !e.(*entity.SnowGolem).HasPumpkin() }},
		{"copper golem", func() *world.EntityHandle {
			return at.New(entity.CopperGolemType, entity.CopperGolemBehaviourConfig{NextWeatheringTick: -1, Antenna: item.NewStack(block.Flower{Type: block.Poppy()}, 1)})
		}, "entity.copper_golem.shear", "minecraft:poppy",
			func(_ *world.Tx, e world.Entity) bool { return e.(*entity.CopperGolem).Antenna().Empty() }},
		{"sulfur cube", func() *world.EntityHandle {
			return at.New(entity.SulfurCubeType, entity.SulfurCubeBehaviourConfig{Size: 2, BodyItem: item.NewStack(block.Stone{}, 1)})
		}, "entity.sulfur_cube.eject", "minecraft:stone",
			func(_ *world.Tx, e world.Entity) bool { return e.(*entity.SulfurCube).BodyItem().Empty() }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			r := &soundNameRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				p := playerWithShears(tx)
				mob := tx.AddEntity(c.spawn())
				if !p.UseItemOnEntity(mob) {
					t.Fatal("the player could not use the shears on the mob")
				}
				if !c.done(tx, mob) {
					t.Fatal("the mob was not sheared")
				}
				if held, _ := p.HeldItems(); !worn(held) {
					t.Errorf("shears are %v, want them worn down by one", held)
				}
				if len(r.names) != 1 || r.names[0] != c.sound {
					t.Errorf("played %v, want only %s", r.names, c.sound)
				}
				var dropped []string
				for _, e := range entitiesOfType(tx, entity.ItemType) {
					name, _ := e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Item().EncodeItem()
					dropped = append(dropped, name)
				}
				if len(dropped) == 0 || dropped[0] != c.drop {
					t.Errorf("dropped %v, want %s", dropped, c.drop)
				}
			})
		})
	}

	t.Run("things that are not ready", func(t *testing.T) {
		t.Parallel()
		for name, spawn := range map[string]func() *world.EntityHandle{
			"copper golem without an antenna": func() *world.EntityHandle { return entity.NewCopperGolem(at) },
			"sulfur cube without a block":     func() *world.EntityHandle { return entity.NewSulfurCube(at) },
			"mooshroom calf": func() *world.EntityHandle {
				return at.New(entity.MooshroomType, entity.MooshroomConfig{Age: -24000})
			},
			"lamb": func() *world.EntityHandle { return at.New(entity.SheepType, entity.SheepConfig{AgeTicks: -24000}) },
		} {
			w, _ := dispenserWorld(t)
			r := &soundNameRecorder{}
			w.Handle(r)
			<-w.Exec(func(tx *world.Tx) {
				p := playerWithShears(tx)
				p.UseItemOnEntity(tx.AddEntity(spawn()))
				held, _ := p.HeldItems()
				if held.Durability() != held.MaxDurability() || len(r.names) != 0 || len(entitiesOfType(tx, entity.ItemType)) != 0 {
					t.Errorf("%s: shears are %v, played %v and %d items dropped, want nothing done",
						name, held, r.names, len(entitiesOfType(tx, entity.ItemType)))
				}
			})
		}
	})
}

// TestPlayerShearsCutLeads checks Entity.interact: a player with shears cuts the
// lead of a mob before anything else, the lead drops, shears snip and wear down
// by one, and the sheep on the lead keeps its wool.
func TestPlayerShearsCutLeads(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	r := &soundNameRecorder{}
	w.Handle(r)
	<-w.Exec(func(tx *world.Tx) {
		p := playerWithShears(tx)
		sheep := tx.AddEntity(entity.NewSheep(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 64, .5}}))
		leash := sheep.(interface{ LeashEnt() *entity.Ent }).LeashEnt()
		holder := addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, .5})
		leash.AttachLeash(holder, tx)
		if !p.UseItemOnEntity(sheep) {
			t.Fatal("the player could not use the shears on the sheep")
		}
		if leash.HolderLeashID() != 0 || sheep.(*entity.Sheep).Sheared() {
			t.Error("want the lead cut and the wool kept")
		}
		if held, _ := p.HeldItems(); !worn(held) {
			t.Errorf("shears are %v, want them worn down by one", held)
		}
		if len(r.names) != 1 || r.names[0] != "item.shears.snip" {
			t.Errorf("played %v, want only item.shears.snip", r.names)
		}
		items := entitiesOfType(tx, entity.ItemType)
		if len(items) != 1 {
			t.Fatalf("%d items dropped, want the lead", len(items))
		}
		if _, ok := items[0].(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Item().(item.Lead); !ok {
			t.Error("the item dropped is not a lead")
		}
	})
}
