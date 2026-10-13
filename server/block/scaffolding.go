package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// scaffoldingMaxDistance is the distance at which scaffolding is no longer
// supported.
const scaffoldingMaxDistance = 7

// Scaffolding is a climbable frame that holds itself up within seven blocks of
// a supported scaffolding column and falls or breaks beyond.
type Scaffolding struct {
	transparent
	sourceWaterDisplacer

	// Distance is the number of scaffolding blocks horizontally between this
	// one and a supported column, 0 for scaffolding standing on a sturdy face.
	Distance int
}

// Model ports ScaffoldingBlock.getCollisionShape for entities above it: the
// top surface they stand on.
func (Scaffolding) Model() world.BlockModel { return scaffoldingModel{} }

// SideClosed lets water flow through the frame.
func (Scaffolding) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

// BreakInfo ...
func (s Scaffolding) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(Scaffolding{}))
}

// FlammabilityInfo ...
func (Scaffolding) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 60, false)
}

// UseOnBlock ports ScaffoldingBlockItem.updatePlacementContext: used on
// scaffolding it extends the column upward, or toward the user's facing when
// used on its top, or toward the clicked face while sneaking, by up to seven
// blocks. Elsewhere it needs a position within the support distance.
func (s Scaffolding) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	if _, ok := tx.Block(pos).(Scaffolding); ok {
		target, ok := scaffoldingExtension(pos, face, tx, user)
		if !ok {
			return false
		}
		place(tx, target, Scaffolding{Distance: scaffoldingDistance(target, tx)}, user, ctx)
		return placed(ctx)
	}
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}
	distance := scaffoldingDistance(pos, tx)
	if distance == scaffoldingMaxDistance {
		return false
	}
	place(tx, pos, Scaffolding{Distance: distance}, user, ctx)
	return placed(ctx)
}

// scaffoldingExtension finds where scaffolding used on scaffolding goes.
func scaffoldingExtension(pos cube.Pos, face cube.Face, tx *world.Tx, user item.User) (cube.Pos, bool) {
	direction := cube.FaceUp
	switch {
	case scaffoldingUserSneaking(user):
		direction = face
	case face == cube.FaceUp:
		direction = user.Rotation().Direction().Face()
	}
	horizontal := 0
	for target := pos.Side(direction); horizontal < scaffoldingMaxDistance; target = target.Side(direction) {
		if target.OutOfBounds(tx.Range()) {
			return cube.Pos{}, false
		}
		b := tx.Block(target)
		if _, ok := b.(Scaffolding); !ok {
			return target, replaceableWith(tx, target, Scaffolding{})
		}
		if direction.Axis() != cube.Y {
			horizontal++
		}
	}
	return cube.Pos{}, false
}

func scaffoldingUserSneaking(user item.User) bool {
	s, ok := user.(interface{ Sneaking() bool })
	return ok && s.Sneaking()
}

// scaffoldingDistance ports ScaffoldingBlock.getDistance: 0 on a sturdy face,
// the distance of scaffolding below, or one more than the nearest horizontal
// neighbour, up to seven.
func scaffoldingDistance(pos cube.Pos, tx *world.Tx) int {
	below := pos.Side(cube.FaceDown)
	distance := scaffoldingMaxDistance
	if s, ok := tx.Block(below).(Scaffolding); ok {
		distance = s.Distance
	} else if tx.Block(below).Model().FaceSolid(below, cube.FaceUp, tx) {
		return 0
	}
	for _, face := range cube.HorizontalFaces() {
		if s, ok := tx.Block(pos.Side(face)).(Scaffolding); ok {
			if distance = min(distance, s.Distance+1); distance == 1 {
				break
			}
		}
	}
	return distance
}

// Placed schedules the support check of newly placed scaffolding.
func (s Scaffolding) Placed(pos cube.Pos, tx *world.Tx, _ world.Block) {
	tx.ScheduleBlockUpdate(pos, s, time.Second/20)
}

// NeighbourUpdateTick schedules a support check.
func (s Scaffolding) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	tx.ScheduleBlockUpdate(pos, s, time.Second/20)
}

// ScheduledTick ports ScaffoldingBlock.tick: scaffolding updates its
// distance, and losing support it breaks, or falls if it was placed
// unsupported.
func (s Scaffolding) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	distance := scaffoldingDistance(pos, tx)
	switch {
	case distance == scaffoldingMaxDistance && s.Distance == scaffoldingMaxDistance:
		tx.SetBlock(pos, nil, nil)
		create := tx.World().EntityRegistry().Config().FallingBlock
		tx.AddEntity(create(world.EntitySpawnOpts{Position: pos.Vec3Middle()}, s))
	case distance == scaffoldingMaxDistance:
		breakBlock(s, pos, tx)
	case distance != s.Distance:
		tx.SetBlock(pos, Scaffolding{Distance: distance}, nil)
	}
}

// SurvivesFall reports whether falling scaffolding is supported where it
// lands.
func (Scaffolding) SurvivesFall(pos cube.Pos, tx *world.Tx) bool {
	return scaffoldingDistance(pos, tx) < scaffoldingMaxDistance
}

// EncodeItem ...
func (Scaffolding) EncodeItem() (name string, meta int16) {
	return "minecraft:scaffolding", 0
}

// EncodeBlock ...
func (s Scaffolding) EncodeBlock() (string, map[string]any) {
	return "minecraft:scaffolding", map[string]any{"stability": int32(s.Distance), "stability_check": false}
}

// allScaffolding returns every scaffolding state.
func allScaffolding() (b []world.Block) {
	for distance := range scaffoldingMaxDistance + 1 {
		b = append(b, Scaffolding{Distance: distance})
	}
	return
}

// scaffoldingModel is the top surface of scaffolding.
type scaffoldingModel struct{}

// BBox ...
func (scaffoldingModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(0, 14.0/16, 0, 1, 1, 1)}
}

// FaceSolid ...
func (scaffoldingModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool { return false }
