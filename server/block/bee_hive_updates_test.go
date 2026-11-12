package block

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestBeesStayingInAHiveSendNothing checks a hive ticked for the bees in it
// sends its viewers nothing until it releases one: BeehiveBlockEntity.serverTick
// counts the time of the bees without setChanged.
func TestBeesStayingInAHiveSendNothing(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	viewer := &blockUpdateCounter{}
	pos := cube.Pos{8, 64, 8}
	w.Do(func(tx *world.Tx) {
		world.NewLoader(2, w, viewer).Move(tx, mgl64.Vec3{8, 64, 8})
	})
	w.Do(func(tx *world.Tx) { world.NewLoader(2, w, viewer).Load(tx, 100) })
	w.Do(func(tx *world.Tx) {
		nest := BeeNest{Beehive: Beehive{Occupants: []any{map[string]any{"ActorIdentifier": "minecraft:bee", "TicksLeftToStay": int32(100000), "SaveData": map[string]any{}}}}}
		tx.SetBlock(pos, nest, nil)
		tx.ScheduleBlockUpdate(pos, nest, 50*time.Millisecond)
	})
	w.AdvanceTick()
	before := viewer.updates
	for range 40 {
		w.AdvanceTick()
	}
	if n := viewer.updates - before; n != 0 {
		t.Errorf("%d block updates in 40 ticks of bees staying in a hive, want none", n)
	}
	w.Do(func(tx *world.Tx) {
		nest, ok := tx.Block(pos).(BeeNest)
		if !ok || len(nest.Occupants) != 1 {
			t.Fatalf("block = %#v, want the nest with its bee", tx.Block(pos))
		}
		if left := nest.Occupants[0].(map[string]any)["TicksLeftToStay"].(int32); left >= 100000-30 {
			t.Errorf("TicksLeftToStay = %d, want it counting down", left)
		}
	})
}
