package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// Activate links or relinks a compass to the lodestone.
func (l Lodestone) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	held, _ := u.HeldItems()
	compass, ok := held.Item().(item.Compass)
	if !ok {
		return false
	}
	relink := compass.TrackingHandle != 0
	l.trackingHandle = tx.World().TrackPosition(pos, l.trackingHandle)
	tx.SetBlock(pos, l, nil)
	// The inventory update must reach the client before the tracking update.
	tx.ScheduleBlockUpdate(pos, l, time.Second/20)
	linked := held.WithItem(item.Compass{TrackingHandle: l.trackingHandle})
	if relink {
		ctx.NewItem = linked
		ctx.ReplaceHeldItem = true
		ctx.SubtractFromCount(held.Count())
	} else {
		ctx.NewItem = linked.Grow(1 - held.Count())
		ctx.SubtractFromCount(1)
	}
	tx.PlaySound(pos.Vec3Centre(), sound.LodestoneCompassLink{})
	return true
}

// ScheduledTick sends the delayed position tracking update for newly linked compasses.
func (l Lodestone) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	viewers := tx.Viewers(pos.Vec3Centre())
	defer tx.ReleaseViewers(viewers)
	if len(viewers) == 0 {
		return
	}
	dim, ok := world.DimensionID(tx.World().Dimension())
	if !ok {
		return
	}
	for _, viewer := range viewers {
		viewer.ViewBlockAction(pos, world.PositionTrackingUpdateAction{
			Handle: l.trackingHandle, Position: pos, Dimension: dim,
		})
	}
}

// TrackingHandle returns the position tracking handle assigned to the block.
func (l Lodestone) TrackingHandle() int32 { return l.trackingHandle }

// WithTrackingHandle returns the lodestone with a position tracking handle assigned.
func (l Lodestone) WithTrackingHandle(handle int32) world.Block {
	l.trackingHandle = handle
	return l
}

// EncodeNBT encodes the Bedrock lodestone block actor data.
func (l Lodestone) EncodeNBT() map[string]any {
	return map[string]any{"id": "Lodestone", "trackingHandle": l.trackingHandle}
}

// DecodeNBT decodes the Bedrock lodestone block actor data.
func (l Lodestone) DecodeNBT(data map[string]any) any {
	l.trackingHandle = nbtconv.Int32(data, "trackingHandle")
	return l
}
