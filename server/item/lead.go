package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"strings"
)

// Lead is an item used to lead leashable entities.
type Lead struct{}

// EncodeItem returns the Bedrock item identifier.
func (Lead) EncodeItem() (string, int16) { return "minecraft:lead", 0 }

// UseOnEntity attaches the targeted leashable entity to the user.
func (Lead) UseOnEntity(target world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	if knot, ok := target.(interface {
		TransferLeashes(world.Entity, *world.Tx) bool
	}); ok {
		return knot.TransferLeashes(user, tx)
	}
	if target.H() == nil {
		return false
	}
	id := target.H().Type().EncodeEntity()
	_, living := target.(interface{ Health() float64 })
	if (!living || strings.HasSuffix(id, ":player")) && !strings.Contains(id, "boat") {
		return false
	}
	leashable, ok := target.(interface {
		AttachLeash(world.Entity, *world.Tx) bool
	})
	if !ok || !leashable.AttachLeash(user, tx) {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}

// UseOnBlock ties nearby entities leashed to the user to a fence knot.
func (Lead) UseOnBlock(pos cube.Pos, _ cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, _ *UseContext) bool {
	if _, ok := tx.Block(pos).(interface{ LeashAttachable() bool }); !ok {
		return false
	}
	create := tx.World().EntityRegistry().Config().TieLeashedEntities
	return create != nil && create(pos, tx, user)
}
