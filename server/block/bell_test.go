package block_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBellRingingTimer(t *testing.T) {
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
	defer w.Close()
	pos := cube.Pos{0, 64, 0}

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
		tx.SetBlock(pos, block.Bell{Attach: block.StandingBellAttachment(), Toggle: true}, nil)
	})
	for tick := 1; tick <= 59; tick++ {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			bell := tx.Block(pos).(block.Bell)
			if !bell.Toggle {
				t.Errorf("bell stopped ringing at tick %d", tick)
			}
			if got := bell.EncodeNBT()["Ticks"]; got != int32(tick) {
				t.Errorf("ring ticks at tick %d = %v", tick, got)
			}
		})
	}
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		bell := tx.Block(pos).(block.Bell)
		if bell.Toggle || bell.EncodeNBT()["Ticks"] != int32(0) {
			t.Errorf("bell state after 60 ticks = ringing %v, ticks %v", bell.Toggle, bell.EncodeNBT()["Ticks"])
		}
	})
}
