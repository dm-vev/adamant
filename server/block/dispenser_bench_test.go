package block_test

import (
	"log/slog"
	"math/rand/v2"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// BenchmarkDispense measures one dispense of a dispenser that drops an item,
// shoots a projectile, or uses an item on the block it faces, removing what
// it spawned after each.
func BenchmarkDispense(b *testing.B) {
	for _, bc := range []struct {
		name string
		it   world.Item
	}{
		{"drop", item.Diamond{}},
		{"projectile", item.Snowball{}},
		{"use on block", item.BoneMeal{}},
	} {
		b.Run(bc.name, func(b *testing.B) {
			w := world.Config{Log: slog.New(slog.DiscardHandler), Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
			defer func() { _ = w.Close() }()
			pos := cube.Pos{0, 65, 0}
			r := rand.New(rand.NewPCG(1, 2))
			<-w.Exec(func(tx *world.Tx) {
				d := block.NewDispenser()
				d.Facing, d.Triggered = cube.FaceEast, true
				tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(pos.Side(cube.FaceEast).Side(cube.FaceDown), block.Stone{}, nil)
				tx.SetBlock(pos, d, nil)
				inv := d.Inventory(tx, pos)
				stack := item.NewStack(bc.it, 64)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					_ = inv.SetItem(0, stack)
					tx.Block(pos).(block.Dispenser).ScheduledTick(pos, tx, r)
					var spawned []world.Entity
					for e := range tx.Entities() {
						spawned = append(spawned, e)
					}
					for _, e := range spawned {
						tx.RemoveEntity(e)
					}
				}
			})
		})
	}
}
