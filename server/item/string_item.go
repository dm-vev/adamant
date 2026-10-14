package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// String is a crafting material dropped by spiders and found in various structures.
type String struct{}

// MaxCount ...
func (String) MaxCount() int {
	return 64
}

// EncodeItem ...
func (String) EncodeItem() (name string, meta int16) {
	return "minecraft:string", 0
}

// UseOnBlock places tripwire when the string is used on a block, leaving
// placement to the tripwire block.
func (String) UseOnBlock(pos cube.Pos, face cube.Face, clickPos mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	tripwire, ok := world.BlockByName("minecraft:trip_wire", map[string]any{
		"attached_bit": uint8(0), "disarmed_bit": uint8(0), "powered_bit": uint8(0), "suspended_bit": uint8(1),
		"minecraft:connection_north": uint8(0), "minecraft:connection_east": uint8(0),
		"minecraft:connection_south": uint8(0), "minecraft:connection_west": uint8(0),
	})
	if !ok {
		return false
	}
	usable, ok := tripwire.(UsableOnBlock)
	return ok && usable.UseOnBlock(pos, face, clickPos, tx, user, ctx)
}
