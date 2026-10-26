package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// growingPlantBoneMeal ports the bone meal of GrowingPlantHeadBlock and
// GrowingPlantBodyBlock: from the head, the last block of the plant in the
// direction it grows, up to blocks more of the plant grow into the space
// ahead, each a year older than the last up to 25. The plant must have room
// for one.
func growingPlantBoneMeal(pos cube.Pos, tx *world.Tx, grow cube.Face, age func(world.Block) (int, bool), canGrowInto func(world.Block) bool, withAge func(int) world.Block, blocks int) item.BoneMealResult {
	head, headAge := pos, 0
	if a, ok := age(tx.Block(pos)); ok {
		headAge = a
	}
	for {
		next := head.Side(grow)
		a, ok := age(tx.Block(next))
		if !ok {
			break
		}
		head, headAge = next, a
	}
	forward := head.Side(grow)
	if forward.OutOfBounds(tx.Range()) || !canGrowInto(tx.Block(forward)) {
		return item.BoneMealResultNone
	}
	nextAge := min(headAge+1, 25)
	for i := 0; i < blocks && !forward.OutOfBounds(tx.Range()) && canGrowInto(tx.Block(forward)); i++ {
		tx.SetBlock(forward, withAge(nextAge), nil)
		forward = forward.Side(grow)
		nextAge = min(nextAge+1, 25)
	}
	return item.BoneMealResultSmall
}

// netherVinesBlocksToGrow ports NetherVines.getBlocksToGrowWhenBonemealed:
// each further block grows with 82.6% of the chance of the last.
func netherVinesBlocksToGrow() int {
	count := 0
	for p := 1.0; rand.Float64() < p; p *= 0.826 {
		count++
	}
	return count
}

// BoneMeal grows weeping vines down by NetherVines' count of blocks.
func (v WeepingVines) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return growingPlantBoneMeal(pos, tx, cube.FaceDown, func(b world.Block) (int, bool) {
		w, ok := b.(WeepingVines)
		return w.Age, ok
	}, isAir, func(age int) world.Block { return WeepingVines{Age: age} }, netherVinesBlocksToGrow())
}

// BoneMeal grows twisting vines up by NetherVines' count of blocks.
func (v TwistingVines) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return growingPlantBoneMeal(pos, tx, cube.FaceUp, func(b world.Block) (int, bool) {
		w, ok := b.(TwistingVines)
		return w.Age, ok
	}, isAir, func(age int) world.Block { return TwistingVines{Age: age} }, netherVinesBlocksToGrow())
}

// BoneMeal grows a glow berry on the clicked cave vines, as CaveVinesBlock
// and CaveVinesPlantBlock.performBonemeal.
func (v CaveVines) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if v.HasBerries() {
		return item.BoneMealResultNone
	}
	tx.SetBlock(pos, v.WithBerries(pos, tx), nil)
	return item.BoneMealResultSmall
}

// BoneMeal grows kelp up by a block into water, as KelpBlock and
// KelpPlantBlock with GrowingPlantHeadBlock.performBonemeal.
func (k Kelp) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return growingPlantBoneMeal(pos, tx, cube.FaceUp, func(b world.Block) (int, bool) {
		kelp, ok := b.(Kelp)
		return kelp.Age, ok
	}, func(b world.Block) bool {
		w, ok := b.(Water)
		return ok && w.Depth == 8
	}, func(age int) world.Block { return Kelp{Age: age} }, 1)
}

// BoneMeal grows the pale hanging moss a block longer below its tip, as
// HangingMossBlock.performBonemeal.
func (p PaleHangingMoss) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	tip := pos
	for {
		if _, ok := tx.Block(tip.Side(cube.FaceDown)).(PaleHangingMoss); !ok {
			break
		}
		tip = tip.Side(cube.FaceDown)
	}
	grow := tip.Side(cube.FaceDown)
	if grow.OutOfBounds(tx.Range()) || !isAir(tx.Block(grow)) {
		return item.BoneMealResultNone
	}
	p.Tip = true
	tx.SetBlock(grow, p, nil)
	return item.BoneMealResultSmall
}

// BoneMeal hangs a new mangrove propagule below mangrove leaves, as
// MangroveLeavesBlock.performBonemeal.
func (l Leaves) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	below := pos.Side(cube.FaceDown)
	if l.Type != MangroveLeaves() || !isAir(tx.Block(below)) {
		return item.BoneMealResultNone
	}
	tx.SetBlock(below, TreeSapling{Type: "mangrove_propagule", Hanging: true}, &world.SetOpts{DisableBlockUpdates: true})
	return item.BoneMealResultSmall
}

// BoneMeal turns netherrack next to nylium into that nylium, or into either
// at random next to both, while sky light could pass down onto it, as
// NetherrackBlock.performBonemeal.
func (n Netherrack) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	if tx.LightBlockedInto(pos.Add(cube.Pos{0, 2, 0}), cube.FaceDown) != 0 {
		return item.BoneMealResultNone
	}
	var warped, crimson bool
	for x := -1; x <= 1; x++ {
		for y := -1; y <= 1; y++ {
			for z := -1; z <= 1; z++ {
				if nylium, ok := tx.Block(pos.Add(cube.Pos{x, y, z})).(Nylium); ok {
					warped, crimson = warped || nylium.Warped, crimson || !nylium.Warped
				}
			}
		}
	}
	switch {
	case warped && crimson:
		tx.SetBlock(pos, Nylium{Warped: rand.IntN(2) == 0}, nil)
	case warped || crimson:
		tx.SetBlock(pos, Nylium{Warped: warped}, nil)
	default:
		return item.BoneMealResultNone
	}
	return item.BoneMealResultSmall
}

// BoneMeal grows hanging roots below rooted dirt, as
// RootedDirtBlock.performBonemeal.
func (d DirtWithRoots) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	below := pos.Side(cube.FaceDown)
	if below.OutOfBounds(tx.Range()) || !isAir(tx.Block(below)) {
		return item.BoneMealResultNone
	}
	tx.SetBlock(below, HangingRoots{}, nil)
	return item.BoneMealResultSmall
}

// BoneMeal grows seagrass with water above into tall seagrass, as
// SeagrassBlock.performBonemeal.
func (s SeaGrass) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	above := pos.Side(cube.FaceUp)
	if s.Type != SeaGrassDefault() {
		return item.BoneMealResultNone
	}
	if _, water := tx.Block(above).(Water); !water {
		return item.BoneMealResultNone
	}
	opts := &world.SetOpts{DisableBlockUpdates: true}
	tx.SetBlock(pos, SeaGrass{Type: SeaGrassDoubleBottom()}, opts)
	tx.SetBlock(above, SeaGrass{Type: SeaGrassDoubleTop()}, opts)
	return item.BoneMealResultSmall
}

// BoneMeal grows short dry grass into tall dry grass, as
// ShortDryGrassBlock.performBonemeal.
func (g ShortDryGrass) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	tx.SetBlock(pos, TallDryGrass{}, nil)
	return item.BoneMealResultSmall
}

// BoneMeal spreads short dry grass to a random empty horizontal neighbour it
// survives in, as TallDryGrassBlock.performBonemeal.
func (g TallDryGrass) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	faces := cube.HorizontalFaces()
	for _, i := range rand.Perm(len(faces)) {
		neighbour := pos.Side(faces[i])
		if isAir(tx.Block(neighbour)) && supportsVegetation(ShortDryGrass{}, tx.Block(neighbour.Side(cube.FaceDown))) {
			tx.SetBlock(neighbour, ShortDryGrass{}, nil)
			return item.BoneMealResultSmall
		}
	}
	return item.BoneMealResultNone
}

// GrowWaterPlants ports BoneMealItem.growWaterPlant for bone meal used on a
// sturdy face under water: 128 attempts walk randomly away from the water
// source, a step further every 16 attempts, stopping at full blocks. Each
// grows seagrass where it survives, or in biomes producing corals from bone
// meal a quarter of the time a random coral, the first a wall fan on the
// clicked face when it is horizontal; seagrass already there grows tall a
// tenth of the time. It reports if the water was a source to grow from.
func (w Water) GrowWaterPlants(pos cube.Pos, face cube.Face, tx *world.Tx) bool {
	if w.Depth != 8 {
		return false
	}
	for j := 0; j < 128; j++ {
		test := pos
		blocked := false
		for i := 0; i < j/16; i++ {
			test = test.Add(cube.Pos{rand.IntN(3) - 1, (rand.IntN(3) - 1) * rand.IntN(3) / 2, rand.IntN(3) - 1})
			if fullCollisionBlock(tx, test) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		var grow world.Block = SeaGrass{Type: SeaGrassDefault()}
		if biomeProducesCorals(tx, test) {
			if j == 0 && face.Axis() != cube.Y {
				grow = CoralWallFan{Type: randomCoralType(), Facing: face.Direction()}
			} else if rand.IntN(4) == 0 {
				grow = randomUnderwaterBoneMeal()
			}
		}
		if fan, ok := grow.(CoralWallFan); ok {
			for d := 0; !waterPlantSurvives(tx, test, fan) && d < 4; d++ {
				fan.Facing = cube.Directions()[rand.IntN(4)]
				grow = fan
			}
		}
		if !waterPlantSurvives(tx, test, grow) {
			continue
		}
		switch existing := tx.Block(test).(type) {
		case Water:
			if existing.Depth == 8 {
				tx.SetBlock(test, grow, nil)
			}
		case SeaGrass:
			if _, water := tx.Block(test.Side(cube.FaceUp)).(Water); water && existing.Type == SeaGrassDefault() && rand.IntN(10) == 0 {
				existing.BoneMeal(test, tx)
			}
		}
	}
	return true
}

// biomeProducesCorals reports if the biome at pos is in
// produces_corals_from_bonemeal.
func biomeProducesCorals(tx *world.Tx, pos cube.Pos) bool {
	in, _ := gamedata.BiomeTagContains("minecraft:produces_corals_from_bonemeal", world.JavaBiomeName(tx.Biome(pos)))
	return in
}

func randomCoralType() CoralType {
	types := CoralTypes()
	return types[rand.IntN(len(types))]
}

// randomUnderwaterBoneMeal picks a block of underwater_bonemeals: seagrass,
// a coral, a coral fan or a coral wall fan facing north.
func randomUnderwaterBoneMeal() world.Block {
	var blocks []world.Block
	blocks = append(blocks, SeaGrass{Type: SeaGrassDefault()})
	for _, t := range CoralTypes() {
		blocks = append(blocks, Coral{Type: t}, CoralFan{Type: t}, CoralWallFan{Type: t, Facing: cube.North})
	}
	return blocks[rand.IntN(len(blocks))]
}

// waterPlantSurvives ports canSurvive of seagrass, corals, coral fans and
// coral wall fans: they stand on a sturdy top face that is no magma block for
// seagrass, and wall fans hang on the sturdy face behind them.
func waterPlantSurvives(tx *world.Tx, pos cube.Pos, b world.Block) bool {
	if fan, ok := b.(CoralWallFan); ok {
		behind := pos.Side(fan.Facing.Face().Opposite())
		return tx.Block(behind).Model().FaceSolid(behind, fan.Facing.Face(), tx)
	}
	below := pos.Side(cube.FaceDown)
	under := tx.Block(below)
	if _, magma := under.(Magma); magma {
		if _, seagrass := b.(SeaGrass); seagrass {
			return false
		}
	}
	return under.Model().FaceSolid(below, cube.FaceUp, tx)
}

// shearGrowingPlantTip ports ShearsItem.useOn on a GrowingPlantHeadBlock:
// the tip of the plant, the last block in the direction it grows, stops
// growing by taking the maximum age of 25. It reports if it did.
func shearGrowingPlantTip(pos cube.Pos, tx *world.Tx, grow cube.Face, age func(world.Block) (int, bool), withAge func(int) world.Block) bool {
	a, ok := age(tx.Block(pos))
	if !ok || a >= 25 {
		return false
	}
	if _, more := age(tx.Block(pos.Side(grow))); more {
		return false
	}
	tx.SetBlock(pos, withAge(25), nil)
	return true
}

// ShearTip stops the weeping vines growing.
func (v WeepingVines) ShearTip(pos cube.Pos, tx *world.Tx) bool {
	return shearGrowingPlantTip(pos, tx, cube.FaceDown, func(b world.Block) (int, bool) { w, ok := b.(WeepingVines); return w.Age, ok }, func(a int) world.Block { v.Age = a; return v })
}

// ShearTip stops the twisting vines growing.
func (v TwistingVines) ShearTip(pos cube.Pos, tx *world.Tx) bool {
	return shearGrowingPlantTip(pos, tx, cube.FaceUp, func(b world.Block) (int, bool) { w, ok := b.(TwistingVines); return w.Age, ok }, func(a int) world.Block { v.Age = a; return v })
}

// ShearTip stops the cave vines growing.
func (v CaveVines) ShearTip(pos cube.Pos, tx *world.Tx) bool {
	return shearGrowingPlantTip(pos, tx, cube.FaceDown, func(b world.Block) (int, bool) { w, ok := b.(CaveVines); return w.Age, ok }, func(a int) world.Block { v.Age = a; return v })
}

// ShearTip stops the kelp growing.
func (k Kelp) ShearTip(pos cube.Pos, tx *world.Tx) bool {
	return shearGrowingPlantTip(pos, tx, cube.FaceUp, func(b world.Block) (int, bool) { w, ok := b.(Kelp); return w.Age, ok }, func(a int) world.Block { k.Age = a; return k })
}
