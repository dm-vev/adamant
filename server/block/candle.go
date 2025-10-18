package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Candle is a non-solid decorative block that can be lit to emit light. Up to four candles may occupy the same block
// space, increasing the emitted light level.
type Candle struct {
	empty
	transparent
	sourceWaterDisplacer

	// AdditionalCandles is the amount of extra candles clustered together. Valid values range from 0-3.
	AdditionalCandles int
	// Lit specifies if the candle cluster is lit.
	Lit bool
	// Colour indicates if the candle is dyed. When Coloured is false, the candle has the default, undyed variant.
	Colour   item.Colour
	Coloured bool
}

// matchesColour returns true if both candles share the same colour information.
func (c Candle) matchesColour(other Candle) bool {
	if c.Coloured != other.Coloured {
		return false
	}
	if !c.Coloured {
		return true
	}
	return c.Colour == other.Colour
}

// baseItem returns the candle as it should appear in item form.
func (c Candle) baseItem() Candle {
	return Candle{Colour: c.Colour, Coloured: c.Coloured}
}

// LightEmissionLevel ...
func (c Candle) LightEmissionLevel() uint8 {
	if !c.Lit {
		return 0
	}
	return uint8(3 + c.AdditionalCandles*3)
}

// BreakInfo ...
func (c Candle) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, simpleDrops(item.NewStack(c.baseItem(), c.AdditionalCandles+1)))
}

// SideClosed ...
func (Candle) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// UseOnBlock ...
func (c Candle) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if existing, ok := tx.Block(pos).(Candle); ok && c.matchesColour(existing) {
		if existing.AdditionalCandles >= 3 {
			return false
		}
		existing.AdditionalCandles++
		place(tx, pos, existing, user, ctx)
		return placed(ctx)
	}

	if cake, ok := tx.Block(pos).(Cake); ok && face == cube.FaceUp && cake.Bites == 0 {
		candleCake := CandleCake{Colour: c.Colour, Coloured: c.Coloured}
		place(tx, pos, candleCake, user, ctx)
		return placed(ctx)
	}

	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return false
	}
	if _, ok := tx.Liquid(pos); ok {
		return false
	}

	place(tx, pos, c.baseItem(), user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick ...
func (c Candle) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	below := pos.Side(cube.FaceDown)
	if !tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		breakBlock(c, pos, tx)
		return
	}
	if _, ok := tx.Liquid(pos); ok {
		breakBlock(c, pos, tx)
	}
}

// HasLiquidDrops ...
func (Candle) HasLiquidDrops() bool {
	return true
}

// Activate ...
func (c Candle) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if !c.Lit {
		return false
	}

	tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
	c.Lit = false
	tx.SetBlock(pos, c, nil)
	return true
}

// Ignite ...
func (c Candle) Ignite(pos cube.Pos, tx *world.Tx, _ world.Entity) bool {
	if existing, ok := tx.Block(pos).(Candle); ok {
		c = existing
	}
	if c.Lit {
		return false
	}
	if _, ok := tx.Liquid(pos); ok {
		return false
	}

	tx.PlaySound(pos.Vec3Centre(), sound.Ignite{})
	c.Lit = true
	tx.SetBlock(pos, c, nil)
	return true
}

// Splash ...
func (c Candle) Splash(tx *world.Tx, pos cube.Pos) {
	if existing, ok := tx.Block(pos).(Candle); ok {
		c = existing
	}
	if !c.Lit {
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
	c.Lit = false
	tx.SetBlock(pos, c, nil)
}

// EncodeItem ...
func (c Candle) EncodeItem() (name string, meta int16) {
	if c.Coloured {
		return "minecraft:" + c.Colour.String() + "_candle", 0
	}
	return "minecraft:candle", 0
}

// EncodeBlock ...
func (c Candle) EncodeBlock() (name string, properties map[string]any) {
	if c.Coloured {
		name = "minecraft:" + c.Colour.String() + "_candle"
	} else {
		name = "minecraft:candle"
	}
	properties = map[string]any{
		"candles": int32(c.AdditionalCandles),
		"lit":     c.Lit,
	}
	return
}

// allCandles returns all candle block states.
func allCandles() (b []world.Block) {
	for _, variant := range candleVariants() {
		base := Candle{Colour: variant.Colour, Coloured: variant.Coloured}
		for additional := 0; additional < 4; additional++ {
			candle := base
			candle.AdditionalCandles = additional
			b = append(b, candle)
			candle.Lit = true
			b = append(b, candle)
		}
	}
	return
}

type candleVariant struct {
	Colour   item.Colour
	Coloured bool
}

func candleVariants() []candleVariant {
	variants := []candleVariant{{}}
	for _, colour := range item.Colours() {
		variants = append(variants, candleVariant{Colour: colour, Coloured: true})
	}
	return variants
}
