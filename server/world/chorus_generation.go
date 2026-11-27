package world

import (
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/vanilla"
)

// ChorusGenerationAccess is ChorusFlowerBlock's live LevelAccessor view.
// Generation setters retain Source flags and may reject a write independently
// of the recursive plant's bookkeeping and random consumption.
type ChorusGenerationAccess interface {
	BlockStateID(cube.Pos) (uint32, error)
	SetBlockState(cube.Pos, uint32, int) bool
}

var chorusGenerationStates = func() [65]uint32 {
	var states [65]uint32
	plant := stateDefault("chorus_plant")
	for mask := range 64 {
		state := plant
		for side, property := range [...]string{"down", "up", "north", "east", "south", "west"} {
			state = stateWith(state, property, mask&(1<<side) != 0)
		}
		states[mask] = state
	}
	states[64] = stateWith(stateDefault("chorus_flower"), "age", 5)
	return states
}()

// GenerateChorusPlant runs the Source helper, not its feature's acceptance
// predicate. ChorusPlantFeature passes maxHorizontalSpread=8. Generic access
// keeps native End's small view on the stack instead of boxing it per plant.
func GenerateChorusPlant[A ChorusGenerationAccess](access A, origin cube.Pos, random *vanilla.Random, maxHorizontalSpread int) {
	setChorusPlant(access, origin)
	growChorusPlant(access, origin, origin, random, maxHorizontalSpread, 0)
}

func chorusStateAt[A ChorusGenerationAccess](access A, pos cube.Pos) uint32 {
	id, err := access.BlockStateID(pos)
	if err != nil {
		panic(err)
	}
	return id
}

func setChorusPlant[A ChorusGenerationAccess](access A, pos cube.Pos) {
	mask := 0
	for side, offset := range [...]cube.Pos{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
		id := chorusStateAt(access, pos.Add(offset))
		name := stateName(id)
		connected := name == "chorus_plant" || name == "chorus_flower"
		if side == 0 && !connected {
			var context shapeContext
			connected = context.tag(id, "minecraft:supports_chorus_plant")
		}
		if connected {
			mask |= 1 << side
		}
	}
	access.SetBlockState(pos, chorusGenerationStates[mask], 2)
}

func chorusNeighboursEmpty[A ChorusGenerationAccess](access A, pos cube.Pos, excluded int) bool {
	for side, offset := range [...]cube.Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}} {
		if side != excluded && !stateIsAir(chorusStateAt(access, pos.Add(offset))) {
			return false
		}
	}
	return true
}

func growChorusPlant[A ChorusGenerationAccess](access A, current, origin cube.Pos, random *vanilla.Random, maxHorizontalSpread, depth int) {
	height := int(random.NextInt(4)) + 1
	if depth == 0 {
		height++
	}
	for i := range height {
		pos := current.Add(cube.Pos{0, i + 1, 0})
		if !chorusNeighboursEmpty(access, pos, -1) {
			return
		}
		setChorusPlant(access, pos)
		setChorusPlant(access, pos.Side(cube.FaceDown))
	}
	branched := false
	if depth < 4 {
		stems := int(random.NextInt(4))
		if depth == 0 {
			stems++
		}
		for range stems {
			direction := int(random.NextInt(4))
			offset := [...]cube.Pos{{0, 0, -1}, {1, 0, 0}, {0, 0, 1}, {-1, 0, 0}}[direction]
			pos := current.Add(cube.Pos{offset[0], height, offset[2]})
			dx, dz := int32(pos[0]-origin[0]), int32(pos[2]-origin[2])
			if dx < 0 {
				dx = -dx
			}
			if dz < 0 {
				dz = -dz
			}
			if int(dx) >= maxHorizontalSpread || int(dz) >= maxHorizontalSpread ||
				!stateIsAir(chorusStateAt(access, pos)) || !stateIsAir(chorusStateAt(access, pos.Side(cube.FaceDown))) ||
				!chorusNeighboursEmpty(access, pos, (direction+2)&3) {
				continue
			}
			branched = true
			setChorusPlant(access, pos)
			setChorusPlant(access, current.Add(cube.Pos{0, height, 0}))
			growChorusPlant(access, pos, origin, random, maxHorizontalSpread, depth+1)
		}
	}
	if !branched {
		access.SetBlockState(current.Add(cube.Pos{0, height, 0}), chorusGenerationStates[64], 2)
	}
}
