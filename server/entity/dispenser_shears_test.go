package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// shearedBy dispenses shears towards what set builds in the cell in front, and
// returns the shears left in the dispenser and the items that dropped.
func shearedBy(tx *world.Tx, set func(front cube.Pos)) (shears item.Stack, dropped []item.Stack) {
	pos := dispenseOnce(tx, item.NewStack(item.Shears{}, 1), set)
	for _, e := range entitiesOfType(tx, entity.ItemType) {
		dropped = append(dropped, e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item())
	}
	return slotOf(tx, pos), dropped
}

// worn reports whether the shears were worn down by one.
func worn(s item.Stack) bool { return !s.Empty() && s.Durability() == s.MaxDurability()-1 }

// TestDispenserShearsWhatJavaShears checks ShearsDispenseItemBehavior.tryShearEntity:
// every entity that is Shearable and ready for shearing in 26.3 is sheared from
// a dispenser, with its drops, and the shears wear down by one; one that is not
// ready is left alone and the dispenser keeps the shears.
func TestDispenserShearsWhatJavaShears(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		spawn func(world.EntitySpawnOpts) *world.EntityHandle
		// done reports whether the mob was sheared.
		done func(tx *world.Tx, e world.Entity) bool
		// drop is what falls from it.
		drop string
	}{
		{"snow golem with a pumpkin", entity.NewSnowGolem,
			func(_ *world.Tx, e world.Entity) bool { return !e.(*entity.SnowGolem).HasPumpkin() }, "minecraft:carved_pumpkin"},
		{"copper golem with a poppy", func(o world.EntitySpawnOpts) *world.EntityHandle {
			return o.New(entity.CopperGolemType, entity.CopperGolemBehaviourConfig{NextWeatheringTick: -1, Antenna: item.NewStack(block.Flower{Type: block.Poppy()}, 1)})
		}, func(_ *world.Tx, e world.Entity) bool { return e.(*entity.CopperGolem).Antenna().Empty() }, "minecraft:poppy"},
		{"sulfur cube with a block", func(o world.EntitySpawnOpts) *world.EntityHandle {
			return o.New(entity.SulfurCubeType, entity.SulfurCubeBehaviourConfig{Size: 2, BodyItem: item.NewStack(block.Stone{}, 1)})
		}, func(_ *world.Tx, e world.Entity) bool { return e.(*entity.SulfurCube).BodyItem().Empty() }, "minecraft:stone"},
		{"mooshroom", entity.NewMooshroom, func(tx *world.Tx, _ world.Entity) bool {
			return len(entitiesOfType(tx, entity.MooshroomType)) == 0 && len(entitiesOfType(tx, entity.CowType)) == 1
		}, "minecraft:red_mushroom"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				var mob world.Entity
				shears, dropped := shearedBy(tx, func(front cube.Pos) {
					mob = tx.AddEntity(c.spawn(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
				})
				if !c.done(tx, mob) {
					t.Fatal("the mob was not sheared")
				}
				if !worn(shears) {
					t.Errorf("shears are %v, want them worn down by one", shears)
				}
				if len(dropped) == 0 {
					t.Fatal("nothing dropped")
				}
				if name, _ := dropped[0].Item().EncodeItem(); name != c.drop {
					t.Errorf("%s dropped, want %s", name, c.drop)
				}
			})
		})
	}

	t.Run("things that are not ready", func(t *testing.T) {
		t.Parallel()
		for name, spawn := range map[string]func(world.EntitySpawnOpts) *world.EntityHandle{
			"copper golem without an antenna": entity.NewCopperGolem,
			"sulfur cube without a block":     entity.NewSulfurCube,
			"mooshroom calf": func(o world.EntitySpawnOpts) *world.EntityHandle {
				return o.New(entity.MooshroomType, entity.MooshroomConfig{Age: -24000})
			},
			"cow": entity.NewCow,
		} {
			w, _ := dispenserWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				shears, dropped := shearedBy(tx, func(front cube.Pos) { tx.AddEntity(spawn(world.EntitySpawnOpts{Position: front.Vec3Middle()})) })
				if shears.Empty() || shears.Durability() != shears.MaxDurability() || len(dropped) != 0 {
					t.Errorf("%s: shears are %v and %d items dropped, want the shears kept and nothing dropped", name, shears, len(dropped))
				}
			})
		}
	})
}

// TestDispenserShearsLeads checks Entity.shearOffAllLeashConnections: a mob in
// front that is on a lead has it cut, and so has every mob led by an entity in
// front; the leads drop and the shears wear down by one. A lead is cut before
// the mob is sheared.
func TestDispenserShearsLeads(t *testing.T) {
	t.Parallel()
	leads := func(dropped []item.Stack) (n int) {
		for _, s := range dropped {
			if _, ok := s.Item().(item.Lead); ok {
				n += s.Count()
			}
		}
		return n
	}
	t.Run("a mob on a lead", func(t *testing.T) {
		t.Parallel()
		w, _ := dispenserWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			var cow *entity.Ent
			holder := addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, 0.5})
			shears, dropped := shearedBy(tx, func(front cube.Pos) {
				cow = tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: front.Vec3Middle()})).(interface{ LeashEnt() *entity.Ent }).LeashEnt()
				cow.AttachLeash(holder, tx)
			})
			if cow.HolderLeashID() != 0 {
				t.Error("the cow is still on its lead")
			}
			if !worn(shears) || leads(dropped) != 1 {
				t.Errorf("shears are %v and %d leads dropped, want the shears worn and one lead", shears, leads(dropped))
			}
		})
	})
	t.Run("a player leading a mob", func(t *testing.T) {
		t.Parallel()
		w, _ := dispenserWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			var holder *player.Player
			cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, 0.5}})).(interface{ LeashEnt() *entity.Ent }).LeashEnt()
			shears, dropped := shearedBy(tx, func(front cube.Pos) {
				holder = addSurvivalPlayer(tx, front.Vec3Middle())
				cow.AttachLeash(holder, tx)
			})
			if cow.HolderLeashID() != 0 {
				t.Error("the cow is still led by the player")
			}
			if !worn(shears) || leads(dropped) != 1 {
				t.Errorf("shears are %v and %d leads dropped, want the shears worn and one lead", shears, leads(dropped))
			}
		})
	})
	t.Run("a sheep on a lead keeps its wool", func(t *testing.T) {
		t.Parallel()
		w, _ := dispenserWorld(t)
		<-w.Exec(func(tx *world.Tx) {
			var sheep world.Entity
			holder := addSurvivalPlayer(tx, mgl64.Vec3{10.5, 64, 0.5})
			shears, dropped := shearedBy(tx, func(front cube.Pos) {
				sheep = tx.AddEntity(entity.NewSheep(world.EntitySpawnOpts{Position: front.Vec3Middle()}))
				sheep.(interface{ LeashEnt() *entity.Ent }).LeashEnt().AttachLeash(holder, tx)
			})
			if sheep.(*entity.Sheep).Sheared() {
				t.Error("the sheep was sheared, want only its lead cut")
			}
			if !worn(shears) || leads(dropped) != 1 || len(dropped) != 1 {
				t.Errorf("shears are %v and %d items dropped, want the shears worn and one lead", shears, len(dropped))
			}
		})
	})
}

// TestDispenserShearsIgnoreSpectators checks EntitySelector.NO_SPECTATORS: a
// spectator in front is not the entity the dispenser shears.
func TestDispenserShearsIgnoreSpectators(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{10.5, 64, 0.5}})).(interface{ LeashEnt() *entity.Ent }).LeashEnt()
		shears, dropped := shearedBy(tx, func(front cube.Pos) {
			spectator := tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: front.Vec3Middle(), GameMode: world.GameModeSpectator})).(*player.Player)
			cow.AttachLeash(spectator, tx)
		})
		if cow.HolderLeashID() == 0 || len(dropped) != 0 || shears.Durability() != shears.MaxDurability() {
			t.Errorf("lead cut: %v, %d items dropped, shears %v; want a spectator left alone", cow.HolderLeashID() == 0, len(dropped), shears)
		}
	})
}
