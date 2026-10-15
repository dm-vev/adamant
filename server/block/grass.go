package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// Grass blocks generate abundantly across the surface of the world.
type Grass struct {
	solid
}

// SoilFor ...
func (g Grass) SoilFor(block world.Block) bool {
	switch block.(type) {
	case ShortGrass, Fern, DoubleTallGrass, Flower, GoldenDandelion, Torchflower, Eyeblossom, DoubleFlower, PitcherPlant, NetherSprouts, PinkPetals, SugarCane, DeadBush, BambooSapling, Bamboo, SweetBerries, TreeSapling, Azalea, FloweringAzalea:
		return true
	}
	return false
}

// RandomTick handles the ticking of grass, which may or may not result in the spreading of grass onto dirt.
func (g Grass) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	aboveLight := tx.Light(pos.Side(cube.FaceUp))
	if aboveLight < 4 {
		// The light above the block is too low: The grass turns to dirt.
		tx.SetBlock(pos, Dirt{}, nil)
		return
	}
	if aboveLight < 9 {
		// Don't attempt to spread if the light level is lower than 9.
		return
	}

	// Generate a single uint32 as we only need 28 bits (7 bits each iteration).
	n := r.Uint32()

	// Four attempts to spread to another block.
	for i := 0; i < 4; i++ {
		x, y, z := int(n)%3, int(n>>2)%5, int(n>>5)%3
		n >>= 7

		spreadPos := pos.Add(cube.Pos{x - 1, y - 3, z - 1})
		// Don't spread grass to locations where dirt is exposed to hardly any light.
		if tx.Light(spreadPos.Side(cube.FaceUp)) < 4 {
			continue
		}
		b := tx.Block(spreadPos)
		if dirt, ok := b.(Dirt); !ok || dirt.Coarse {
			continue
		}
		tx.SetBlock(spreadPos, g, nil)
	}
}

// BoneMeal ports GrassBlock.performBonemeal: with air above, 128 walks over
// the grass around grow short grass, now and then one of the biome's bone
// meal flowers, and tall grass from short grass.
func (g Grass) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	above := pos.Side(cube.FaceUp)
	if _, air := tx.Block(above).(Air); !air {
		return item.BoneMealResultNone
	}
	features, ok := tx.Features()
	if !ok {
		return item.BoneMealResultNone
	}
	random := world.FeatureRandom()
walks:
	for attempt := range 128 {
		test := above
		for range attempt / 16 {
			dx := int(random.NextInt(3)) - 1
			dy := (int(random.NextInt(3)) - 1) * int(random.NextInt(3)) / 2
			dz := int(random.NextInt(3)) - 1
			test = test.Add(cube.Pos{dx, dy, dz})
			if _, grass := tx.Block(test.Side(cube.FaceDown)).(Grass); !grass || fullCollisionBlock(tx, test) {
				continue walks
			}
		}
		switch b := tx.Block(test).(type) {
		case ShortGrass:
			if random.NextFloat() < 0.1 {
				b.BoneMeal(test, tx)
			}
		case Air:
			if test.OutOfBounds(tx.Range()) {
				continue
			}
			if random.NextFloat() < 0.125 {
				flowers := features.BoneMealFeatures(tx, test)
				if len(flowers) == 0 {
					continue
				}
				features.PlaceConfigured(tx, flowers[random.NextInt(int32(len(flowers)))], test, random)
			} else {
				features.PlacePlaced(tx, "minecraft:grass_bonemeal", test, random)
			}
		}
	}
	return item.BoneMealResultArea
}

// fullCollisionBlock reports whether the block at pos collides as a full
// cube, as BlockState.isCollisionShapeFullBlock does.
func fullCollisionBlock(tx *world.Tx, pos cube.Pos) bool {
	boxes := tx.Block(pos).Model().BBox(pos, tx)
	return len(boxes) == 1 && boxes[0] == cube.Box(0, 0, 0, 1, 1, 1)
}

// BreakInfo ...
func (g Grass) BreakInfo() BreakInfo {
	return newBreakInfo(0.6, alwaysHarvestable, shovelEffective, silkTouchOneOf(Dirt{}, g))
}

// CompostChance ...
func (Grass) CompostChance() float64 {
	return 0.3
}

// EncodeItem ...
func (Grass) EncodeItem() (name string, meta int16) {
	return "minecraft:grass_block", 0
}

// EncodeBlock ...
func (Grass) EncodeBlock() (string, map[string]any) {
	return "minecraft:grass_block", nil
}

// Till ...
func (g Grass) Till() (world.Block, bool) {
	return Farmland{}, true
}

// Shovel ...
func (g Grass) Shovel() (world.Block, bool) {
	return DirtPath{}, true
}
