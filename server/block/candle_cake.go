package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// CandleCake is a cake with a single candle placed on top of it.
type CandleCake struct {
	transparent
	sourceWaterDisplacer

	// Colour indicates the colour of the candle. When Coloured is false the candle is undyed.
	Colour   item.Colour
	Coloured bool
	// Lit specifies if the candle is lit.
	Lit bool
}

// LightEmissionLevel ...
func (c CandleCake) LightEmissionLevel() uint8 {
	if !c.Lit {
		return 0
	}
	return 3
}

// BreakInfo ...
func (c CandleCake) BreakInfo() BreakInfo {
	return newBreakInfo(0.5, neverHarvestable, nothingEffective, simpleDrops(item.NewStack(Candle{Colour: c.Colour, Coloured: c.Coloured}, 1)))
}

// SideClosed ...
func (CandleCake) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool {
	return false
}

// HasLiquidDrops ...
func (CandleCake) HasLiquidDrops() bool {
	return true
}

// Activate ...
func (c CandleCake) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	held, _ := u.HeldItems()
	if c.Lit {
		tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
		c.Lit = false
		tx.SetBlock(pos, c, nil)
		return true
	}

	if !held.Empty() {
		return false
	}
	if i, ok := u.(interface {
		Saturate(food int, saturation float64)
	}); ok {
		i.Saturate(2, 0.4)
		tx.PlaySound(u.Position().Add(mgl64.Vec3{0, 1.5}), sound.Burp{})
		dropItem(tx, item.NewStack(Candle{Colour: c.Colour, Coloured: c.Coloured}, 1), pos.Vec3Centre())
		tx.SetBlock(pos, Cake{Bites: 1}, nil)
		return true
	}
	return false
}

// Ignite ...
func (c CandleCake) Ignite(pos cube.Pos, tx *world.Tx, _ world.Entity) bool {
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
func (c CandleCake) Splash(tx *world.Tx, pos cube.Pos) {
	if !c.Lit {
		return
	}
	tx.PlaySound(pos.Vec3Centre(), sound.FireExtinguish{})
	c.Lit = false
	tx.SetBlock(pos, c, nil)
}

// NeighbourUpdateTick ...
func (c CandleCake) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if _, air := tx.Block(pos.Side(cube.FaceDown)).(Air); air {
		breakBlock(c, pos, tx)
		return
	}
	if _, ok := tx.Liquid(pos); ok {
		breakBlock(c, pos, tx)
	}
}

// EncodeBlock ...
func (c CandleCake) EncodeBlock() (name string, properties map[string]any) {
	if c.Coloured {
		name = "minecraft:" + c.Colour.String() + "_candle_cake"
	} else {
		name = "minecraft:candle_cake"
	}
	properties = map[string]any{
		"bite_counter": int32(0),
		"candles":      int32(1),
		"lit":          c.Lit,
	}
	return
}

// Model ...
func (CandleCake) Model() world.BlockModel {
	return model.Cake{Bites: 0}
}

// allCandleCakes returns all candle cake block states.
func allCandleCakes() (b []world.Block) {
	for _, variant := range candleVariants() {
		base := CandleCake{Colour: variant.Colour, Coloured: variant.Coloured}
		b = append(b, base)
		base.Lit = true
		b = append(b, base)
	}
	return
}
