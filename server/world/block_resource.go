package world

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// nativeBlockResourcePosition is BlockType::popResource's three independent
// binary32 offsets. Keep multiplication and addition separate from each other
// and from the conversion/addition of the integer block coordinates.
func nativeBlockResourcePosition(pos cube.Pos, next func() float32) mgl64.Vec3 {
	var p mgl64.Vec3
	for axis := range p {
		offset := float32(next() * .7)
		offset = float32(offset + .15)
		p[axis] = float64(float32(float32(pos[axis]) + offset))
	}
	return p
}

// DropBlockResource creates a resource belonging to the block at pos. The
// stack uses the Item factory's concrete stack type and reports whether it is
// empty. Inventory spills and explicit item spawning use their own origins.
func (tx *Tx) DropBlockResource(pos cube.Pos, stack interface{ Empty() bool }) Entity {
	if !tx.World().GameRuleBool("block_drops") || stack.Empty() {
		return nil
	}
	p := nativeBlockResourcePosition(pos, tx.RandomFloat32)
	// The native factory raises a block resource by ItemActor's half-height.
	// Script spawnItem supplies its requested actor origin instead.
	p[1] = float64(float32(float32(p[1]) + .125))
	// Actor construction consumes one shared float after popResource's three
	// offsets. Its cosmetic/yaw/launch draws belong to a separate TLS stream.
	_ = tx.RandomFloat32()
	opts := EntitySpawnOpts{Position: p, ItemToss: true}
	e := tx.AddEntity(tx.World().EntityRegistry().Config().Item(opts, stack))
	if item, ok := e.(interface{ SetItemPickupDelay(time.Duration) }); ok {
		item.SetItemPickupDelay(time.Second / 2)
	}
	return e
}
