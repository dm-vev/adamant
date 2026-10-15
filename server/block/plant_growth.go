package block

import (
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// weightedFeature is a configured feature of a WeightedList.
type weightedFeature struct {
	name   string
	weight int32
}

// pickFeature ports WeightedList.getRandom: no feature of an empty list,
// otherwise one drawn by weight with a single nextInt.
func pickFeature(list []weightedFeature, random *java26.Random) string {
	var total int32
	for _, f := range list {
		total += f.weight
	}
	if total == 0 {
		return ""
	}
	i := random.NextInt(total)
	for _, f := range list {
		if i < f.weight {
			return f.name
		}
		i -= f.weight
	}
	return ""
}

// treeGrower ports TreeGrower: the trees, two-by-two trees and trees grown
// near flowers a sapling grows into, and the shortest of them.
type treeGrower struct {
	trees, megaTrees, flowerTrees []weightedFeature
	shortest                      string
}

func one(name string) []weightedFeature { return []weightedFeature{{name, 1}} }

var treeGrowers = map[string]treeGrower{
	"oak": {
		trees:       []weightedFeature{{"minecraft:oak", 9}, {"minecraft:fancy_oak", 1}},
		flowerTrees: []weightedFeature{{"minecraft:oak_bees_005", 9}, {"minecraft:fancy_oak_bees_005", 1}},
		shortest:    "minecraft:oak",
	},
	"spruce": {
		trees:     one("minecraft:spruce"),
		megaTrees: []weightedFeature{{"minecraft:mega_spruce", 1}, {"minecraft:mega_pine", 1}},
		shortest:  "minecraft:spruce",
	},
	"mangrove": {trees: []weightedFeature{{"minecraft:mangrove", 15}, {"minecraft:tall_mangrove", 85}}, shortest: "minecraft:mangrove"},
	"azalea":   {trees: one("minecraft:azalea_tree"), shortest: "minecraft:azalea_tree"},
	"birch":    {trees: one("minecraft:birch"), flowerTrees: one("minecraft:birch_bees_005"), shortest: "minecraft:birch"},
	"jungle": {
		trees: one("minecraft:jungle_tree_no_vine"), megaTrees: one("minecraft:mega_jungle_tree"), shortest: "minecraft:jungle_tree_no_vine",
	},
	"acacia":   {trees: one("minecraft:acacia"), shortest: "minecraft:acacia"},
	"cherry":   {trees: one("minecraft:cherry"), flowerTrees: one("minecraft:cherry_bees_005"), shortest: "minecraft:cherry"},
	"dark_oak": {megaTrees: one("minecraft:dark_oak")},
	"pale_oak": {megaTrees: one("minecraft:pale_oak_bonemeal")},
	"poplar": {
		trees:    []weightedFeature{{"minecraft:red_poplar", 1}, {"minecraft:orange_poplar", 1}, {"minecraft:yellow_poplar", 1}},
		shortest: "minecraft:red_poplar",
	},
}

// growTree ports TreeGrower.growTree: a two-by-two tree where four of the
// same saplings stand, otherwise a tree, one grown near flowers if there are
// some. The saplings make way for the tree and come back if it does not fit.
func (g treeGrower) growTree(tx *world.Tx, pos cube.Pos, same func(world.Block) bool, random *java26.Random) bool {
	features, ok := tx.Features()
	if !ok {
		return false
	}
	if mega := pickFeature(g.megaTrees, random); mega != "" {
		if dx, dz, found := twoByTwoSaplings(tx, pos, same); found {
			corner := pos.Add(cube.Pos{dx, 0, dz})
			saplings := []cube.Pos{corner, corner.Add(cube.Pos{1, 0, 0}), corner.Add(cube.Pos{0, 0, 1}), corner.Add(cube.Pos{1, 0, 1})}
			saved := removeSaplings(tx, saplings)
			if features.PlaceConfigured(tx, mega, corner, random) {
				return true
			}
			restoreSaplings(tx, saved)
			return false
		}
	}
	trees := g.trees
	if len(g.flowerTrees) != 0 && hasFlowersAround(tx, pos) {
		trees = g.flowerTrees
	}
	tree := pickFeature(trees, random)
	if tree == "" {
		return false
	}
	saved := removeSaplings(tx, []cube.Pos{pos})
	if features.PlaceConfigured(tx, tree, pos, random) {
		return true
	}
	restoreSaplings(tx, saved)
	return false
}

// canGrow ports TreeGrower.canGrow: a grower without trees grows only from
// four saplings.
func (g treeGrower) canGrow(tx *world.Tx, pos cube.Pos, same func(world.Block) bool) bool {
	hasTree := len(g.trees) != 0 || len(g.flowerTrees) != 0 && hasFlowersAround(tx, pos)
	if !hasTree && len(g.megaTrees) != 0 {
		_, _, found := twoByTwoSaplings(tx, pos, same)
		return found
	}
	return true
}

// minimumHeight ports TreeGrower.getMinimumHeight: the base height of the
// trunk of the shortest tree.
func (g treeGrower) minimumHeight(tx *world.Tx) int {
	features, ok := tx.Features()
	if !ok || g.shortest == "" {
		return 0
	}
	typeName, config, ok := features.FeatureConfig(g.shortest)
	if !ok || typeName != "tree" {
		return 0
	}
	placer, _ := config["trunk_placer"].(map[string]any)
	height, _ := placer["base_height"].(float64)
	return int(height)
}

// twoByTwoSaplings ports TreeGrower.findTwoByTwoSaplingPos: the offset of the
// north-west corner of a square of four saplings including pos.
func twoByTwoSaplings(tx *world.Tx, pos cube.Pos, same func(world.Block) bool) (int, int, bool) {
	for dx := 0; dx >= -1; dx-- {
		for dz := 0; dz >= -1; dz-- {
			corner := pos.Add(cube.Pos{dx, 0, dz})
			if same(tx.Block(corner)) && same(tx.Block(corner.Add(cube.Pos{1, 0, 0}))) &&
				same(tx.Block(corner.Add(cube.Pos{0, 0, 1}))) && same(tx.Block(corner.Add(cube.Pos{1, 0, 1}))) {
				return dx, dz, true
			}
		}
	}
	return 0, 0, false
}

// hasFlowersAround ports TreeGrower.hasFlowers: a flower within two blocks
// sideways and one up or down.
func hasFlowersAround(tx *world.Tx, pos cube.Pos) bool {
	for x := -2; x <= 2; x++ {
		for y := -1; y <= 1; y++ {
			for z := -2; z <= 2; z++ {
				if javaBlockTag(tx, pos.Add(cube.Pos{x, y, z}), "minecraft:flowers") {
					return true
				}
			}
		}
	}
	return false
}

// savedPlant is a plant that made way for the feature it grows into.
type savedPlant struct {
	pos    cube.Pos
	block  world.Block
	liquid world.Liquid
}

// removeSaplings leaves the fluid each sapling was in in its place, without
// updating neighbours, as TreeGrower.removeSapling does.
func removeSaplings(tx *world.Tx, positions []cube.Pos) []savedPlant {
	quiet := &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true}
	saved := make([]savedPlant, len(positions))
	for i, pos := range positions {
		liquid, _ := tx.Liquid(pos)
		saved[i] = savedPlant{pos: pos, block: tx.Block(pos), liquid: liquid}
		if liquid != nil {
			tx.SetBlock(pos, liquid, quiet)
			tx.SetLiquid(pos, nil)
		} else {
			tx.SetBlock(pos, nil, quiet)
		}
	}
	return saved
}

// restoreSaplings puts back the plants a feature that did not fit replaced.
func restoreSaplings(tx *world.Tx, saved []savedPlant) {
	for _, s := range saved {
		tx.SetBlock(s.pos, s.block, &world.SetOpts{DisableBlockUpdates: true, DisableLiquidDisplacement: true})
		if s.liquid != nil {
			tx.SetLiquid(s.pos, s.liquid)
		}
	}
}

// grower returns the tree grower of the sapling.
func (s TreeSapling) grower() treeGrower {
	if s.Type == "mangrove_propagule" {
		return treeGrowers["mangrove"]
	}
	return treeGrowers[strings.TrimSuffix(s.Type, "_sapling")]
}

// sameSapling reports whether b is a sapling of the same kind as s.
func (s TreeSapling) sameSapling(b world.Block) bool {
	other, ok := b.(TreeSapling)
	return ok && other.Type == s.Type
}

// RandomTick ports SaplingBlock.randomTick, and MangrovePropaguleBlock's: a
// sapling lit well enough grows one time in seven, a propagule in any light,
// and a hanging propagule ages until fully grown.
func (s TreeSapling) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if s.Type == "mangrove_propagule" {
		if !s.Hanging {
			if r.IntN(7) == 0 {
				s.advanceTree(pos, tx, world.FeatureRandom())
			}
		} else if s.Age < 4 {
			s.Age++
			tx.SetBlock(pos, s, &world.SetOpts{DisableBlockUpdates: true})
		}
		return
	}
	if tx.RawBrightness(pos.Side(cube.FaceUp)) >= 9 && r.IntN(7) == 0 {
		s.advanceTree(pos, tx, world.FeatureRandom())
	}
}

// advanceTree ports SaplingBlock.advanceTree: a sapling first grows to its
// second stage, then into a tree. Bedrock's propagule has no stage for this,
// so a propagule grows into a tree at once.
func (s TreeSapling) advanceTree(pos cube.Pos, tx *world.Tx, random *java26.Random) {
	if s.Type != "mangrove_propagule" && s.Stage == 0 {
		s.Stage = 1
		tx.SetBlock(pos, s, &world.SetOpts{DisableBlockUpdates: true})
		return
	}
	s.grower().growTree(tx, pos, s.sameSapling, random)
}

// BoneMeal ports SaplingBlock's bone meal: a sapling that can grow and has
// room above for its shortest tree advances 45% of the time. A hanging
// propagule ages instead until fully grown.
func (s TreeSapling) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	random := world.FeatureRandom()
	if s.Type == "mangrove_propagule" && s.Hanging {
		if s.Age >= 4 {
			return item.BoneMealResultNone
		}
		s.Age++
		tx.SetBlock(pos, s, &world.SetOpts{DisableBlockUpdates: true})
		return item.BoneMealResultSmall
	}
	g := s.grower()
	if s.Type != "mangrove_propagule" {
		if !g.canGrow(tx, pos, s.sameSapling) || pos.Add(cube.Pos{0, g.minimumHeight(tx), 0}).OutOfBounds(tx.Range()) {
			return item.BoneMealResultNone
		}
	}
	if random.NextFloat() < 0.45 {
		s.advanceTree(pos, tx, random)
	}
	return item.BoneMealResultSmall
}

// azaleaBoneMeal ports AzaleaBlock's bone meal: with room above and no fluid
// on top, an azalea grows into an azalea tree 45% of the time.
func azaleaBoneMeal(pos cube.Pos, tx *world.Tx, same func(world.Block) bool) item.BoneMealResult {
	g := treeGrowers["azalea"]
	if pos.Add(cube.Pos{0, g.minimumHeight(tx) + 2, 0}).OutOfBounds(tx.Range()) {
		return item.BoneMealResultNone
	}
	if _, fluid := tx.Liquid(pos.Side(cube.FaceUp)); fluid {
		return item.BoneMealResultNone
	}
	random := world.FeatureRandom()
	if random.NextFloat() < 0.45 {
		g.growTree(tx, pos, same, random)
	}
	return item.BoneMealResultSmall
}

// BoneMeal ...
func (a Azalea) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return azaleaBoneMeal(pos, tx, func(b world.Block) bool { _, ok := b.(Azalea); return ok })
}

// BoneMeal ...
func (a FloweringAzalea) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return azaleaBoneMeal(pos, tx, func(b world.Block) bool { _, ok := b.(FloweringAzalea); return ok })
}

// growFeaturePlant ports MushroomBlock.growMushroom: the plant makes way for
// the feature and comes back if it does not fit.
func growFeaturePlant(tx *world.Tx, pos cube.Pos, feature string, random *java26.Random) bool {
	features, ok := tx.Features()
	if !ok {
		return false
	}
	saved := tx.Block(pos)
	tx.SetBlock(pos, nil, nil)
	if features.PlaceConfigured(tx, feature, pos, random) {
		return true
	}
	tx.SetBlock(pos, saved, nil)
	return false
}

// mushroomCanSurvive ports MushroomBlock.canSurvive: on a block that lets
// mushrooms grow in any light, or in raw light below 13 on a full block.
func mushroomCanSurvive(tx *world.Tx, pos cube.Pos) bool {
	below := pos.Side(cube.FaceDown)
	if javaBlockTag(tx, below, "minecraft:overrides_mushroom_light_requirement") {
		return true
	}
	return tx.Light(pos) < 13 && tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx)
}

// mushroomRandomTick ports MushroomBlock.randomTick: one time in 25 a
// mushroom with at most four of its kind around wanders a few blocks and
// spreads where it can survive.
func mushroomRandomTick(tx *world.Tx, pos cube.Pos, m world.Block, r *rand.Rand) {
	if r.IntN(25) != 0 {
		return
	}
	same := 0
	for x := -4; x <= 4; x++ {
		for y := -1; y <= 1; y++ {
			for z := -4; z <= 4; z++ {
				if sameBlockType(tx.Block(pos.Add(cube.Pos{x, y, z})), m) {
					if same++; same > 4 {
						return
					}
				}
			}
		}
	}
	free := func(p cube.Pos) bool {
		_, air := tx.Block(p).(Air)
		return air && mushroomCanSurvive(tx, p)
	}
	offset := func(p cube.Pos) cube.Pos {
		return p.Add(cube.Pos{r.IntN(3) - 1, r.IntN(2) - r.IntN(2), r.IntN(3) - 1})
	}
	next := offset(pos)
	for range 4 {
		if free(next) {
			pos = next
		}
		next = offset(pos)
	}
	if free(next) {
		tx.SetBlock(next, m, nil)
	}
}

// mushroomBoneMeal ports MushroomBlock's bone meal: with room for its cap, a
// mushroom grows huge 40% of the time.
func mushroomBoneMeal(tx *world.Tx, pos cube.Pos, feature string) item.BoneMealResult {
	features, ok := tx.Features()
	if !ok {
		return item.BoneMealResultNone
	}
	_, config, ok := features.FeatureConfig(feature)
	if !ok {
		return item.BoneMealResultNone
	}
	radius, _ := config["foliage_radius"].(float64)
	if pos.Add(cube.Pos{0, 4 + int(radius), 0}).OutOfBounds(tx.Range()) {
		return item.BoneMealResultNone
	}
	random := world.FeatureRandom()
	if random.NextFloat() < 0.4 {
		growFeaturePlant(tx, pos, feature, random)
	}
	return item.BoneMealResultSmall
}

// RandomTick ...
func (m BrownMushroom) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	mushroomRandomTick(tx, pos, m, r)
}

// BoneMeal ...
func (m BrownMushroom) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return mushroomBoneMeal(tx, pos, "minecraft:huge_brown_mushroom")
}

// RandomTick ...
func (m RedMushroom) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	mushroomRandomTick(tx, pos, m, r)
}

// BoneMeal ...
func (m RedMushroom) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return mushroomBoneMeal(tx, pos, "minecraft:huge_red_mushroom")
}

// BoneMeal ports NetherFungusBlock's bone meal: on its own nylium, a fungus
// grows huge 40% of the time.
func (f Fungus) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	nylium, ok := tx.Block(pos.Side(cube.FaceDown)).(Nylium)
	if !ok || nylium.Warped != f.Warped || pos.Side(cube.FaceUp).OutOfBounds(tx.Range()) {
		return item.BoneMealResultNone
	}
	feature := "minecraft:crimson_fungus_planted"
	if f.Warped {
		feature = "minecraft:warped_fungus_planted"
	}
	random := world.FeatureRandom()
	if random.NextFloat() < 0.4 {
		if features, ok := tx.Features(); ok {
			features.PlaceConfigured(tx, feature, pos, random)
		}
	}
	return item.BoneMealResultSmall
}

// BoneMeal ports NyliumBlock's bone meal: nether vegetation grows on and
// around nylium with air above it.
func (n Nylium) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return placeAboveBoneMeal(tx, pos, "minecraft:nylium_bonemeal")
}

// BoneMeal ports BonemealableFeaturePlacerBlock: a moss patch spreads from
// moss with air above it.
func (m MossBlock) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return placeAboveBoneMeal(tx, pos, "minecraft:moss_patch_bonemeal")
}

// BoneMeal ports BonemealableFeaturePlacerBlock: a pale moss patch spreads
// from pale moss with air above it.
func (m PaleMossBlock) BoneMeal(pos cube.Pos, tx *world.Tx) item.BoneMealResult {
	return placeAboveBoneMeal(tx, pos, "minecraft:pale_moss_patch_bonemeal")
}

// placeAboveBoneMeal places a feature on the block above pos if it is air,
// as bone meal on blocks that spread vegetation does.
func placeAboveBoneMeal(tx *world.Tx, pos cube.Pos, feature string) item.BoneMealResult {
	above := pos.Side(cube.FaceUp)
	if _, air := tx.Block(above).(Air); !air || above.OutOfBounds(tx.Range()) {
		return item.BoneMealResultNone
	}
	if features, ok := tx.Features(); ok {
		features.PlaceConfigured(tx, feature, above, world.FeatureRandom())
	}
	return item.BoneMealResultArea
}

// sameBlockType reports whether a and b are the same block, in any state.
func sameBlockType(a, b world.Block) bool {
	an, _ := a.EncodeBlock()
	bn, _ := b.EncodeBlock()
	return an == bn
}
