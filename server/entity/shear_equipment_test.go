package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestShearsTakeEquipmentOffMounts checks Mob.attemptToShearEquipment for a
// horse: shears take its armour off first, then its saddle, each dropping as
// an item.
func TestShearsTakeEquipmentOffMounts(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		conf := defaultMountConfig(mountHorse)
		conf.Saddled, conf.Tamed = true, true
		e := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}}.New(HorseType, conf))
		horse := mountFromEntity(e)
		horse.b().armor = item.NewStack(item.IronHorseArmour{}, 1)
		dropped := func() []item.Stack {
			var out []item.Stack
			for e := range tx.Entities() {
				if s, ok := pickableItemAnyDelay(e); ok {
					out = append(out, s)
				}
			}
			return out
		}
		if !horse.ShearEquipment(tx, nil) || !horse.b().armor.Empty() || !horse.b().saddled {
			t.Fatal("shears did not take the armour off a saddled horse first")
		}
		if !horse.ShearEquipment(tx, nil) || horse.b().saddled {
			t.Fatal("shears did not take the saddle off a horse without armour")
		}
		if horse.ShearEquipment(tx, nil) {
			t.Fatal("shears sheared a horse with nothing left to shear")
		}
		if d := dropped(); len(d) != 2 {
			t.Fatalf("shearing a horse twice dropped %v, want its armour and saddle", d)
		}
	})
}

// pickableItemAnyDelay returns the stack of an item entity.
func pickableItemAnyDelay(e world.Entity) (item.Stack, bool) {
	ent, ok := e.(*Ent)
	if !ok {
		return item.Stack{}, false
	}
	i, ok := ent.Behaviour().(*ItemBehaviour)
	if !ok {
		return item.Stack{}, false
	}
	return i.i, !i.i.Empty()
}
