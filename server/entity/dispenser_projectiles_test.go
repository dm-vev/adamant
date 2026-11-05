package entity_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dispenserShot is an item a dispenser shoots and the entity it should spawn.
type dispenserShot struct {
	name string
	item world.Item
	typ  world.EntityType
}

func dispenserShots() []dispenserShot {
	return []dispenserShot{
		{"egg", item.Egg{}, entity.EggType},
		{"snowball", item.Snowball{}, entity.SnowballType},
		{"fire charge", item.FireCharge{}, entity.SmallFireballType},
		{"wind charge", item.WindCharge{}, entity.WindChargeType},
		{"arrow", item.Arrow{}, entity.ArrowType},
		{"tipped arrow", item.Arrow{Tip: potion.Poison()}, entity.ArrowType},
		{"bottle o' enchanting", item.BottleOfEnchanting{}, entity.BottleOfEnchantingType},
		{"splash potion", item.SplashPotion{Type: potion.Harming()}, entity.SplashPotionType},
		{"water splash potion", item.SplashPotion{Type: potion.Water()}, entity.SplashPotionType},
		{"lingering potion", item.LingeringPotion{Type: potion.Poison()}, entity.LingeringPotionType},
		{"firework", item.Firework{Duration: time.Second}, entity.FireworkType},
	}
}

// dispenserWorld returns a world with a stone floor below y 64 and a counter
// of the faults the world recovered from.
func dispenserWorld(t *testing.T) (*world.World, *atomic.Int32) {
	t.Helper()
	faults := new(atomic.Int32)
	w := world.Config{
		Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1,
		Fault: func(world.Fault) { faults.Add(1) },
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		for x := -3; x <= 40; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				// A wall for the shots that fall no further than the floor.
				for y := 64; y <= 70 && x == 30; y++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
				}
			}
		}
	})
	return w, faults
}

// loadDispenser puts a dispenser at the origin facing east that holds the
// stack, and dispenses from it on the next tick, as a redstone pulse does.
func loadDispenser(tx *world.Tx, stack item.Stack) cube.Pos {
	pos := cube.Pos{0, 64, 0}
	d := block.NewDispenser()
	d.Facing = cube.FaceEast
	tx.SetBlock(pos, d, nil)
	_ = d.Inventory(tx, pos).SetItem(0, stack)
	tx.ScheduleBlockUpdate(pos, d, time.Millisecond)
	return pos
}

func entitiesOfType(tx *world.Tx, typ world.EntityType) (out []world.Entity) {
	for e := range tx.Entities() {
		if e.H().Type() == typ {
			out = append(out, e)
		}
	}
	return out
}

// TestDispensersShootProjectilesWithoutOwner dispenses every item a dispenser
// shoots from a real dispenser: the entity of the item's type appears with no
// owner, flies until it lands, and neither the dispenser nor the entity
// panics.
func TestDispensersShootProjectilesWithoutOwner(t *testing.T) {
	t.Parallel()
	for _, c := range dispenserShots() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w, faults := dispenserWorld(t)
			var pos cube.Pos
			var held int
			<-w.Exec(func(tx *world.Tx) {
				pos = loadDispenser(tx, item.NewStack(c.item, 2))
				stack, _ := tx.Block(pos).(block.Dispenser).Inventory(tx, pos).Item(0)
				held = stack.Count()
			})
			stepWorld(t, w, 2)
			<-w.Exec(func(tx *world.Tx) {
				shot := entitiesOfType(tx, c.typ)
				if len(shot) != 1 {
					t.Fatalf("%d %T entities after dispensing, want 1", len(shot), c.typ)
				}
				if owned, ok := shot[0].(interface{ ProjectileOwner() *world.EntityHandle }); !ok || owned.ProjectileOwner() != nil {
					t.Fatalf("dispensed %s has owner %v (has accessor %v)", c.name, owned.ProjectileOwner(), ok)
				}
				left, _ := tx.Block(pos).(block.Dispenser).Inventory(tx, pos).Item(0)
				if left.Count() != held-1 {
					t.Fatalf("dispenser holds %d items, want %d", left.Count(), held-1)
				}
			})
			// Everything lands, hits and is gone within ten seconds but
			// arrows, which stay in the ground.
			for range 200 {
				stepWorld(t, w, 1)
			}
			<-w.Exec(func(tx *world.Tx) {
				left := entitiesOfType(tx, c.typ)
				if c.typ == entity.ArrowType {
					if len(left) != 1 || left[0].(*entity.Ent).Velocity().Len() > 1e-3 {
						t.Fatalf("arrow did not land: %d arrows", len(left))
					}
				} else if len(left) != 0 {
					t.Fatalf("%d %T entities still flying after ten seconds", len(left), c.typ)
				}
			})
			if n := faults.Load(); n != 0 {
				t.Fatalf("world recovered from %d panics", n)
			}
			if q := w.Quarantined(); len(q) != 0 {
				t.Fatalf("quarantined: %+v", q)
			}
		})
	}
}

// TestPlayersPickUpDispensedArrows checks ProjectileItem.asProjectile of
// ArrowItem, which sets Pickup.ALLOWED: a player takes the arrow a dispenser
// shot, with its tip, as an arrow item.
func TestPlayersPickUpDispensedArrows(t *testing.T) {
	t.Parallel()
	w, _ := dispenserWorld(t)
	<-w.Exec(func(tx *world.Tx) { loadDispenser(tx, item.NewStack(item.Arrow{Tip: potion.Poison()}, 1)) })
	stepWorld(t, w, 80)
	var p *player.Player
	<-w.Exec(func(tx *world.Tx) {
		arrows := entitiesOfType(tx, entity.ArrowType)
		if len(arrows) != 1 {
			t.Fatalf("%d arrows, want 1", len(arrows))
		}
		p = addSurvivalPlayer(tx, arrows[0].Position().Add(mgl64.Vec3{0, 0.1}))
	})
	stepWorld(t, w, 10)
	<-w.Exec(func(tx *world.Tx) {
		if n := len(entitiesOfType(tx, entity.ArrowType)); n != 0 {
			t.Fatalf("%d arrows left, want the arrow picked up", n)
		}
		var got item.Stack
		for _, s := range p.Inventory().Slots() {
			if _, ok := s.Item().(item.Arrow); ok {
				got = s
			}
		}
		if got.Empty() || got.Item().(item.Arrow).Tip != potion.Poison() {
			t.Fatalf("player holds %v, want a poison arrow", got)
		}
	})
}

// TestProjectilesWithoutOwnerFly creates the projectiles with a constructor
// that takes an owner without one, as a command or a plugin may, and ticks
// them: none panics.
func TestProjectilesWithoutOwnerFly(t *testing.T) {
	t.Parallel()
	w, faults := dispenserWorld(t)
	conf := w.EntityRegistry().Config()
	opts := world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64.5, 0.5}, Velocity: mgl64.Vec3{0.5, 0.2, 0.1}}
	rod := item.NewStack(item.FishingRod{}, 1)
	<-w.Exec(func(tx *world.Tx) {
		for _, h := range []*world.EntityHandle{
			conf.Arrow(opts, world.ArrowSpawnConfig{Damage: 2, Tip: potion.Potion{}}),
			conf.BottleOfEnchanting(opts, nil),
			conf.SplashPotion(opts, potion.Harming(), nil),
			conf.LingeringPotion(opts, potion.Poison(), nil),
			conf.EnderPearl(opts, nil),
			conf.FishingHook(opts, nil, rod),
			conf.Trident(opts, nil, item.NewStack(item.Trident{}, 1), 0, 0, false),
			conf.Firework(opts, item.Firework{Duration: 1}, nil, 1, 0, true),
			entity.NewArrow(opts, nil),
			entity.NewTippedArrow(opts, nil, potion.Poison()),
		} {
			tx.AddEntity(h)
		}
	})
	stepWorld(t, w, 100)
	if n := faults.Load(); n != 0 {
		t.Fatalf("world recovered from %d panics", n)
	}
}
