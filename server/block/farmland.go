package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Farmland is a block that crops are grown on. Farmland is created by interacting with a grass or dirt block using a
// hoe. Farmland can be hydrated by nearby water, with no hydration resulting in it turning into a dirt block.
type Farmland struct {
	tilledGrass

	// Hydration is how much moisture the farmland block has. Hydration starts at 0 & caps at 7. During a random tick
	// update, if there is water within 4 blocks from the farmland block, hydration is set to 7. Otherwise, it
	// decrements until it turns into dirt.
	Hydration int
}

// SoilFor ...
func (f Farmland) SoilFor(block world.Block) bool {
	switch block.(type) {
	case ShortGrass, Fern, DoubleTallGrass, Flower, GoldenDandelion, Torchflower, Eyeblossom, DoubleFlower, PitcherPlant, NetherSprouts, PinkPetals, DeadBush, SweetBerries, TreeSapling, Azalea, FloweringAzalea:
		return true
	}
	return false
}

// NeighbourUpdateTick ...
func (f Farmland) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if solid := tx.Block(pos.Side(cube.FaceUp)).Model().FaceSolid(pos.Side(cube.FaceUp), cube.FaceDown, tx); solid {
		tx.SetBlock(pos, Dirt{}, nil)
	}
}

// RandomTick ...
func (f Farmland) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if !f.hydrated(pos, tx) {
		if f.Hydration > 0 {
			f.Hydration--
			tx.SetBlock(pos, f, nil)
		} else {
			blockAbove := tx.Block(pos.Side(cube.FaceUp))
			if _, cropAbove := blockAbove.(Crop); !cropAbove {
				tx.SetBlock(pos, Dirt{}, nil)
			}
		}
	} else {
		f.Hydration = 7
		tx.SetBlock(pos, f, nil)
	}
}

// hydrated checks for water within 4 blocks in each direction from the farmland.
func (f Farmland) hydrated(pos cube.Pos, tx *world.Tx) bool {
	posX, posY, posZ := pos.X(), pos.Y(), pos.Z()
	for y := 0; y <= 1; y++ {
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				if liquid, ok := tx.Liquid(cube.Pos{posX + x, posY + y, posZ + z}); ok {
					if _, ok := liquid.(Water); ok {
						return true
					}
				}
			}
		}
	}
	return false
}

// EntityLand ports FarmlandBlock.fallOn: a living entity landing from more
// than half a block may trample the farmland into dirt, with a chance of the
// distance minus 0.5, if it is large enough and a player or mob_griefing is
// on.
func (f Farmland) EntityLand(pos cube.Pos, tx *world.Tx, e world.Entity, distance *float64) {
	if _, ok := e.(livingEntity); !ok || rand.Float64() >= *distance-0.5 {
		return
	}
	if _, player := e.(interface{ GameMode() world.GameMode }); !player && !tx.World().GameRuleBool("mob_griefing") {
		return
	}
	box := e.H().Type().BBox(e)
	if width := box.Width(); width*width*box.Height() <= 0.512 {
		return
	}
	ctx := tx.Event()
	if tx.World().Handler().HandleCropTrample(ctx, pos); !ctx.Cancelled() {
		tx.SetBlock(pos, Dirt{}, nil)
		pushEntitiesUp(tx, pos, f)
		tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: e, AffectedState: Dirt{}})
	}
}

// fallDistanceEntity is an entity that has a fall distance.
type fallDistanceEntity interface {
	// ResetFallDistance resets the entities fall distance.
	ResetFallDistance()
	// FallDistance returns the entities fall distance.
	FallDistance() float64
}

// BreakInfo ...
func (f Farmland) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, shovelEffective, oneOf(Dirt{}))
}

// EncodeBlock ...
func (f Farmland) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:farmland", map[string]any{"moisturized_amount": int32(f.Hydration)}
}

// EncodeItem ...
func (f Farmland) EncodeItem() (name string, meta int16) {
	return "minecraft:farmland", 0
}

// allFarmland returns all possible states that a block of farmland can be in.
func allFarmland() (b []world.Block) {
	for i := 0; i <= 7; i++ {
		b = append(b, Farmland{Hydration: i})
	}
	return
}
