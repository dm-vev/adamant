package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
)

func mobGriefingDisabled(tx *world.Tx) bool {
	return !tx.World().GameRuleBool("mob_griefing")
}

// brightOutside ports Level.isBrightOutside: the sky darkening from the
// time of day, rain and thunder stays below 4.
func brightOutside(tx *world.Tx) bool {
	w := tx.World()
	if !w.Dimension().TimeCycle() {
		return false
	}
	d := math.Mod(float64(w.Time())/24000-0.25, 1)
	if d < 0 {
		d++
	}
	timeOfDay := (d*2 + (0.5 - math.Cos(d*math.Pi)/2)) / 3
	v := 1 - (math.Cos(timeOfDay*2*math.Pi)*2 + 0.5)
	v = 1 - min(max(v, 0), 1)
	if tx.Raining() {
		v *= 1 - 5.0/16
	}
	if tx.Thundering() {
		v *= 1 - 5.0/16
	}
	return int((1-v)*11) < 4
}

// seaLevel ports Level.getSeaLevel: the sea level of the noise settings of
// the dimension.
func seaLevel(tx *world.Tx) int {
	switch tx.World().Dimension() {
	case world.Nether:
		return 32
	case world.End:
		return 0
	}
	return 63
}

// difficultyID is Difficulty.getId.
func difficultyID(d world.Difficulty) int {
	switch d {
	case world.DifficultyEasy:
		return 1
	case world.DifficultyNormal:
		return 2
	case world.DifficultyHard:
		return 3
	}
	return 0
}

// blockShape returns the source shape metadata of the block at pos.
func blockShape(tx *world.Tx, pos cube.Pos) (gamedata.GenerationShape, bool) {
	id, err := tx.BlockStateID(pos)
	if err != nil {
		return gamedata.GenerationShape{}, false
	}
	shape, err := gamedata.BlockGenerationShapeID(id)
	return shape, err == nil
}
