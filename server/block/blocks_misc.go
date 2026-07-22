package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// DriedGhast is a decorative block with a facing direction.
type DriedGhast struct {
	transparent
	solid

	Facing      cube.Direction
	Rehydration int
}

// UseOnBlock ...
func (d DriedGhast) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) (used bool) {
	pos, _, used = firstReplaceable(tx, pos, face, d)
	if !used {
		return
	}
	d.Facing = user.Rotation().Direction().Opposite()
	d.Rehydration = 0

	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (d DriedGhast) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(d))
}

// EncodeItem ...
func (DriedGhast) EncodeItem() (name string, meta int16) {
	return "minecraft:dried_ghast", 0
}

// EncodeBlock ...
func (d DriedGhast) EncodeBlock() (string, map[string]any) {
	return "minecraft:dried_ghast", map[string]any{
		"minecraft:cardinal_direction": d.Facing.String(),
		"rehydration_level":            int32(d.Rehydration),
	}
}

// allDriedGhast returns all dried ghast states.
func allDriedGhast() (b []world.Block) {
	for _, d := range cube.Directions() {
		for i := 0; i <= 3; i++ {
			b = append(b, DriedGhast{Facing: d, Rehydration: i})
		}
	}
	return
}

// HeavyCore is a decorative block.
type HeavyCore struct {
	transparent
	solid
	sourceWaterDisplacer
}

// BreakInfo ...
func (h HeavyCore) BreakInfo() BreakInfo {
	return newBreakInfo(10, pickaxeHarvestable, pickaxeEffective, oneOf(h)).withBlastResistance(1200)
}

// EncodeItem ...
func (HeavyCore) EncodeItem() (name string, meta int16) {
	return "minecraft:heavy_core", 0
}

// EncodeBlock ...
func (HeavyCore) EncodeBlock() (string, map[string]any) {
	return "minecraft:heavy_core", nil
}

// GlowingObsidian is an obsidian block that emits light.
type GlowingObsidian struct {
	solid
	bassDrum
}

// LightEmissionLevel ...
func (GlowingObsidian) LightEmissionLevel() uint8 {
	return 12
}

// BreakInfo ...
func (o GlowingObsidian) BreakInfo() BreakInfo {
	return newBreakInfo(35, func(t item.Tool) bool {
		return t.ToolType() == item.TypePickaxe && t.HarvestLevel() >= item.ToolTierDiamond.HarvestLevel
	}, pickaxeEffective, oneOf(o)).withBlastResistance(1200)
}

// EncodeItem ...
func (GlowingObsidian) EncodeItem() (name string, meta int16) {
	return "minecraft:glowingobsidian", 0
}

// EncodeBlock ...
func (GlowingObsidian) EncodeBlock() (string, map[string]any) {
	return "minecraft:glowingobsidian", nil
}

// MangroveRoots are a decorative block.
type MangroveRoots struct {
	transparent
	solid
	sourceWaterDisplacer
}

// BreakInfo ...
func (m MangroveRoots) BreakInfo() BreakInfo {
	return newBreakInfo(0.7, alwaysHarvestable, axeEffective, oneOf(m)).withBlastResistance(0.7)
}

// FlammabilityInfo ...
func (MangroveRoots) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(30, 60, true)
}

// EncodeItem ...
func (MangroveRoots) EncodeItem() (name string, meta int16) {
	return "minecraft:mangrove_roots", 0
}

// EncodeBlock ...
func (MangroveRoots) EncodeBlock() (string, map[string]any) {
	return "minecraft:mangrove_roots", nil
}
