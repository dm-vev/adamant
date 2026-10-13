package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// AmethystCluster is an amethyst bud or cluster growing out of a face of a
// budding amethyst. Stage 0 is a small bud, 1 a medium bud, 2 a large bud and
// 3 a fully grown cluster.
type AmethystCluster struct {
	transparent
	sourceWaterDisplacer

	// Stage is the growth stage, from 0 (small bud) to 3 (cluster).
	Stage int
	// Facing is the direction the bud points, away from its support.
	Facing cube.Face
}

// amethystClusterShapes are the Java height and width in pixels and light
// level of each stage.
var amethystClusterShapes = [4]struct{ height, width, light float64 }{
	{3, 8, 1}, {4, 10, 2}, {5, 10, 4}, {7, 10, 5},
}

// Model ports AmethystClusterBlock's shapes: a box of the stage's width
// extending its height from the supporting face.
func (a AmethystCluster) Model() world.BlockModel { return amethystClusterModel{a} }

// LightEmissionLevel ...
func (a AmethystCluster) LightEmissionLevel() uint8 {
	return uint8(amethystClusterShapes[a.Stage].light)
}

// SideClosed lets water flow around the bud.
func (AmethystCluster) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ports the bud and cluster loot tables: silk touch keeps the block,
// a cluster otherwise drops four shards to pickaxes (with Fortune) and two to
// anything else, and buds drop nothing.
func (a AmethystCluster) BreakInfo() BreakInfo {
	return newBreakInfo(1.5, alwaysHarvestable, pickaxeEffective, func(t item.Tool, enchantments []item.Enchantment) []item.Stack {
		if hasSilkTouch(enchantments) {
			return []item.Stack{item.NewStack(AmethystCluster{Stage: a.Stage, Facing: cube.FaceUp}, 1)}
		}
		if a.Stage < 3 {
			return nil
		}
		if t.ToolType() == item.TypePickaxe {
			return []item.Stack{item.NewStack(item.AmethystShard{}, fortuneOreCount(4, enchantments))}
		}
		return []item.Stack{item.NewStack(item.AmethystShard{}, 2)}
	}).withBlastResistance(1.5)
}

// UseOnBlock places the bud pointing away from the clicked face.
func (a AmethystCluster) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, face, used := firstReplaceable(tx, pos, face, a)
	if !used {
		return false
	}
	a.Facing = face
	if !a.supported(pos, tx) {
		return false
	}
	place(tx, pos, a, user, ctx)
	return placed(ctx)
}

// NeighbourUpdateTick breaks the bud when its support no longer presents a
// sturdy face.
func (a AmethystCluster) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !a.supported(pos, tx) {
		breakBlock(a, pos, tx)
	}
}

// supported ports AmethystClusterBlock.canSurvive.
func (a AmethystCluster) supported(pos cube.Pos, tx *world.Tx) bool {
	support := pos.Side(a.Facing.Opposite())
	return tx.Block(support).Model().FaceSolid(support, a.Facing, tx)
}

// EncodeItem ...
func (a AmethystCluster) EncodeItem() (name string, meta int16) {
	return "minecraft:" + amethystClusterNames[a.Stage], 0
}

// EncodeBlock ...
func (a AmethystCluster) EncodeBlock() (string, map[string]any) {
	return "minecraft:" + amethystClusterNames[a.Stage], map[string]any{"minecraft:block_face": a.Facing.String()}
}

var amethystClusterNames = [4]string{"small_amethyst_bud", "medium_amethyst_bud", "large_amethyst_bud", "amethyst_cluster"}

// allAmethystClusters returns every bud and cluster state.
func allAmethystClusters() (b []world.Block) {
	for stage := range 4 {
		for _, face := range cube.Faces() {
			b = append(b, AmethystCluster{Stage: stage, Facing: face})
		}
	}
	return
}

// amethystClusterModel is the collision of a bud or cluster.
type amethystClusterModel struct{ a AmethystCluster }

func (m amethystClusterModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	shape := amethystClusterShapes[m.a.Stage]
	half, height := shape.width/32, shape.height/16
	box := cube.Box(0.5-half, 0, 0.5-half, 0.5+half, height, 0.5+half)
	switch m.a.Facing {
	case cube.FaceDown:
		box = cube.Box(0.5-half, 1-height, 0.5-half, 0.5+half, 1, 0.5+half)
	case cube.FaceNorth:
		box = cube.Box(0.5-half, 0.5-half, 1-height, 0.5+half, 0.5+half, 1)
	case cube.FaceSouth:
		box = cube.Box(0.5-half, 0.5-half, 0, 0.5+half, 0.5+half, height)
	case cube.FaceWest:
		box = cube.Box(1-height, 0.5-half, 0.5-half, 1, 0.5+half, 0.5+half)
	case cube.FaceEast:
		box = cube.Box(0, 0.5-half, 0.5-half, height, 0.5+half, 0.5+half)
	}
	return []cube.BBox{box}
}

func (amethystClusterModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }

// RandomTick ports BuddingAmethystBlock.randomTick: one time in five a random
// face grows a small bud in air or water, or advances the bud already there.
func (b BuddingAmethyst) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if r.IntN(5) != 0 {
		return
	}
	face := cube.Faces()[r.IntN(6)]
	growPos := pos.Side(face)
	switch existing := tx.Block(growPos).(type) {
	case Air:
		tx.SetBlock(growPos, AmethystCluster{Facing: face}, nil)
	case Water:
		if existing.Depth == 8 {
			tx.SetBlock(growPos, AmethystCluster{Facing: face}, nil)
		}
	case AmethystCluster:
		if existing.Facing == face && existing.Stage < 3 {
			existing.Stage++
			tx.SetBlock(growPos, existing, nil)
		}
	}
}
