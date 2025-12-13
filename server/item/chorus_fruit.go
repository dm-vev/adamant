package item

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// ChorusFruit is an edible item obtained by breaking chorus plants. It teleports the consumer to a random nearby
// location on consumption.
type ChorusFruit struct {
	defaultFood
}

// Consume ...
func (ChorusFruit) Consume(tx *world.Tx, c Consumer) Stack {
	c.Saturate(4, 2.4)

	teleporter, ok := c.(interface {
		Teleport(pos mgl64.Vec3)
		Position() mgl64.Vec3
	})
	if !ok {
		return Stack{}
	}

	origin := teleporter.Position()
	minY, maxY := tx.Range().Min(), tx.Range().Max()

	for i := 0; i < 16; i++ {
		x := int(origin[0]) + rand.IntN(17) - 8
		y := int(origin[1]) + rand.IntN(17) - 8
		z := int(origin[2]) + rand.IntN(17) - 8

		if y < minY || y > maxY {
			continue
		}

		target := cube.Pos{x, y, z}
		target, ok = chorusFruitLanding(tx, target)
		if !ok {
			continue
		}

		tx.PlaySound(origin, sound.Teleport{})
		teleporter.Teleport(target.Vec3Middle())
		tx.PlaySound(target.Vec3Middle(), sound.Teleport{})
		break
	}
	return Stack{}
}

func chorusFruitLanding(tx *world.Tx, start cube.Pos) (cube.Pos, bool) {
	minY, maxY := tx.Range().Min(), tx.Range().Max()

	pos := start
	if pos[1] < minY {
		pos[1] = minY
	} else if pos[1] > maxY {
		pos[1] = maxY
	}

	for y := pos[1]; y > minY; y-- {
		below := cube.Pos{pos[0], y - 1, pos[2]}
		if len(tx.Block(below).Model().BBox(below, tx)) == 0 {
			continue
		}
		feet := cube.Pos{pos[0], y, pos[2]}
		head := cube.Pos{pos[0], y + 1, pos[2]}
		if head[1] > maxY {
			return cube.Pos{}, false
		}
		if len(tx.Block(feet).Model().BBox(feet, tx)) != 0 {
			continue
		}
		if len(tx.Block(head).Model().BBox(head, tx)) != 0 {
			continue
		}
		return feet, true
	}
	return cube.Pos{}, false
}

// EncodeItem ...
func (ChorusFruit) EncodeItem() (name string, meta int16) {
	return "minecraft:chorus_fruit", 0
}
