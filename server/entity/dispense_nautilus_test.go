package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// TestDispenserEquipsTamedNautilus checks AbstractNautilus.canDispenserEquipIntoSlot:
// a tamed adult nautilus takes a saddle and nautilus armour from a dispenser,
// once each, and a wild one takes nothing.
func TestDispenserEquipsTamedNautilus(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name  string
		tamed bool
		stack item.Stack
		worn  func(m *oceanMob) bool
	}{
		{"saddle on a tamed nautilus", true, item.NewStack(item.Saddle{}, 1), func(m *oceanMob) bool { return m.b().saddle }},
		{"saddle on a wild nautilus", false, item.NewStack(item.Saddle{}, 1), func(m *oceanMob) bool { return m.b().saddle }},
		{"armour on a tamed nautilus", true, item.NewStack(item.NautilusArmor{Material: item.NautilusArmorIron}, 1), func(m *oceanMob) bool { return m.b().bodyArmorMaterial == int(item.NautilusArmorIron) }},
		{"armour on a wild nautilus", false, item.NewStack(item.NautilusArmor{Material: item.NautilusArmorIron}, 1), func(m *oceanMob) bool { return m.b().bodyArmorMaterial >= 0 }},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
			defer func() { _ = w.Close() }()
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 64, 0}
				d := block.NewDispenser()
				d.Facing = cube.FaceEast
				tx.SetBlock(pos, d, nil)
				_ = d.Inventory(tx, pos).SetItem(0, c.stack)
				front := pos.Side(cube.FaceEast)
				mob := tx.AddEntity(world.EntitySpawnOpts{Position: front.Vec3Middle()}.New(NautilusType, oceanConfig{kind: oceanNautilus, tamed: c.tamed}))
				tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, nil)

				taken := c.tamed
				if got := c.worn(mob.(*Nautilus).oceanMob); got != taken {
					t.Fatalf("nautilus took the item: %v, want %v", got, taken)
				}
				left, _ := d.Inventory(tx, pos).Item(0)
				items := 0
				for e := range tx.Entities() {
					if e.H().Type() == ItemType {
						items++
					}
				}
				if taken && (!left.Empty() || items != 0) {
					t.Errorf("dispenser holds %v and %d items dropped, want the item worn", left, items)
				}
				if !taken && items != 1 {
					t.Errorf("%d items dropped, want the item ejected", items)
				}
			})
		})
	}
}
