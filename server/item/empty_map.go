package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// EmptyMap is a blank map. Using it creates a filled map of the area around
// the user, centred on the map grid.
type EmptyMap struct {
	// Locator is set for Bedrock's empty locator map. Java maps always track
	// the positions of players, so both kinds create the same map.
	Locator bool
}

// Use ports EmptyMapItem.use: one map is consumed for a filled map of scale 0
// covering the user's position.
func (m EmptyMap) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	data, err := tx.CreateMap(cube.PosFromVec3(user.Position()), 0)
	if err != nil {
		return false
	}
	ctx.SubtractFromCount(1)
	ctx.NewItem = NewStack(FilledMap{NativeID: data.ID, HasNativeID: true, JavaID: data.JavaID, HasJavaID: data.HasJavaID}, 1)
	ctx.NewItemSurvivalOnly = false
	tx.PlaySound(user.Position(), sound.Custom{Name: "ui.cartography_table.take_result", Volume: 1, Pitch: 1})
	return true
}

// EncodeItem ...
func (m EmptyMap) EncodeItem() (name string, meta int16) {
	if m.Locator {
		return "minecraft:empty_map", 2
	}
	return "minecraft:empty_map", 0
}
