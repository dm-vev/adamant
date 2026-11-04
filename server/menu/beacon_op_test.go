package menu_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/world"
)

// beaconOfLevel sets a beacon at pos on a pyramid of iron of level levels
// and has it find the pyramid.
func beaconOfLevel(tx *world.Tx, pos cube.Pos, level int) {
	for l := 1; l <= level; l++ {
		for x := -l; x <= l; x++ {
			for z := -l; z <= l; z++ {
				tx.SetBlock(pos.Add(cube.Pos{x, -l, z}), block.Iron{}, nil)
			}
		}
	}
	tx.SetBlock(pos, block.Beacon{}, nil)
	tx.Block(pos).(block.Beacon).Tick(0, pos, tx)
}

// TestPayBeacon checks the effects a beacon allows at each level of its
// pyramid, as primary and as secondary effect, and that a payment allowed
// sets them, takes the payment and leaves an effect not passed as it was.
func TestPayBeacon(t *testing.T) {
	// The level from which each lasting effect is allowed, 5 for never.
	minLevel := map[effect.LastingType]int{nil: 0}
	for id := 1; id < 64; id++ {
		if e, ok := effect.ByID(id); ok {
			if lasting, ok := e.(effect.LastingType); ok {
				minLevel[lasting] = 5
			}
		}
	}
	for e, level := range map[effect.LastingType]int{
		effect.Speed: 1, effect.Haste: 1, effect.Resistance: 2, effect.JumpBoost: 2, effect.Strength: 3, effect.Regeneration: 4,
	} {
		minLevel[e] = level
	}
	pos := cube.Pos{0, 64, 0}
	for level := 0; level <= 4; level++ {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
		<-w.Exec(func(tx *world.Tx) {
			beaconOfLevel(tx, pos, level)
			if got := tx.Block(pos).(block.Beacon).Level(); got != level {
				t.Fatalf("the beacon is of level %d, want %d", got, level)
			}
			m := menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
			for e, from := range minLevel {
				for _, secondary := range []bool{false, true} {
					primary, second := e, effect.LastingType(nil)
					if secondary {
						primary, second = effect.Speed, e
					}
					allowed := level >= from && level >= minLevel[primary] && (!secondary || e == nil || level >= 4)

					tx.SetBlock(pos, block.Beacon{}, nil)
					tx.Block(pos).(block.Beacon).Tick(0, pos, tx)
					b, inv := testBatch(tx, m, nil)
					_ = inv.UI.SetItem(menu.UIBeaconPayment, item.NewStack(item.IronIngot{}, 1))
					err := b.PayBeacon(primary, second)
					if (err == nil) != allowed {
						t.Errorf("level %d, primary %T, secondary %T: paid %v, want %v (%v)", level, primary, second, err == nil, allowed, err)
						continue
					}
					got := tx.Block(pos).(block.Beacon)
					if !allowed {
						if len(b.Changes()) != 0 || got.Primary != nil {
							t.Errorf("level %d, %T and %T: a refusal changed the beacon or a slot", level, primary, second)
						}
						continue
					}
					if got.Primary != primary || got.Secondary != second {
						t.Errorf("level %d: the beacon has %T and %T, want %T and %T", level, got.Primary, got.Secondary, primary, second)
					}
					if left, _ := inv.UI.Item(menu.UIBeaconPayment); !left.Empty() {
						t.Errorf("the payment %v was not taken", left)
					}
					changes := b.Changes()
					if len(changes) != 1 || changes[0].Ref.Name != testName(menu.Slot{Index: menu.UIBeaconPayment}, menu.RoleBeaconPayment) {
						t.Errorf("the changes are %v, want the payment taken", changes)
					}
				}
			}
		})
		_ = w.Close()
	}
}

// TestPayBeaconRefuses checks the payments a beacon refuses whatever the
// effects, and that no effect leaves the effects of the beacon.
func TestPayBeaconRefuses(t *testing.T) {
	tests := []struct {
		name             string
		noMenu, noBeacon bool
		payment          item.Stack
		want             bool
	}{
		{name: "an iron ingot", payment: item.NewStack(item.IronIngot{}, 1), want: true},
		{name: "an emerald", payment: item.NewStack(item.Emerald{}, 1), want: true},
		{name: "a gold ingot", payment: item.NewStack(item.GoldIngot{}, 1), want: true},
		{name: "a diamond", payment: item.NewStack(item.Diamond{}, 1), want: true},
		{name: "a netherite ingot", payment: item.NewStack(item.NetheriteIngot{}, 1), want: true},
		{name: "an apple", payment: item.NewStack(item.Apple{}, 1)},
		{name: "nothing"},
		{name: "no menu", noMenu: true, payment: item.NewStack(item.IronIngot{}, 1)},
		{name: "no beacon", noBeacon: true, payment: item.NewStack(item.IronIngot{}, 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{0, 64, 0}
				beaconOfLevel(tx, pos, 4)
				before := tx.Block(pos).(block.Beacon)
				before.Primary, before.Secondary = effect.Haste, effect.Regeneration
				tx.SetBlock(pos, before, nil)
				var m *menu.Menu
				if !test.noMenu {
					m = menu.OpenBlock(tx, pos, &fakeViewer{}, nil)
				}
				if test.noBeacon {
					tx.SetBlock(pos, block.Stone{}, nil)
				}
				b, inv := testBatch(tx, m, nil)
				_ = inv.UI.SetItem(menu.UIBeaconPayment, test.payment)
				err := b.PayBeacon(nil, nil)
				if (err == nil) != test.want {
					t.Fatalf("paid %v, want %v (%v)", err == nil, test.want, err)
				}
				if !test.want {
					if len(b.Changes()) != 0 {
						t.Errorf("a refusal made %d changes", len(b.Changes()))
					}
					return
				}
				if got := tx.Block(pos).(block.Beacon); got.Primary != effect.Haste || got.Secondary != effect.Regeneration {
					t.Errorf("no effects changed the effects of the beacon to %T and %T", got.Primary, got.Secondary)
				}
			})
		})
	}
}
