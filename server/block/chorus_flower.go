package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ChorusFlower is a plant block found in the End, on top of chorus plants.
type ChorusFlower struct {
	empty
	transparent

	// Age is the growth stage of the chorus flower. Values range from 0 to 5, where 5 is a dead flower.
	Age int
}

// Model ...
func (ChorusFlower) Model() world.BlockModel {
	// Chorus flowers have a full collision box but no solid faces.
	return model.Leaves{}
}

// BreakInfo ...
func (c ChorusFlower) BreakInfo() BreakInfo {
	info := newBreakInfo(0.4, alwaysHarvestable, nothingEffective, simpleDrops())
	info.BreakHandler = func(pos cube.Pos, tx *world.Tx, u item.User) {
		if u == nil {
			// Chorus flowers do not drop when broken by scheduled updates, liquids, etc.
			return
		}
		if gm, ok := u.(interface{ GameMode() world.GameMode }); ok && gm.GameMode().CreativeInventory() {
			return
		}
		dropItem(tx, item.NewStack(ChorusFlower{}, 1), pos.Vec3Centre())
	}
	return info
}

// NeighbourUpdateTick ...
func (c ChorusFlower) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) {
	if !c.canSurvive(pos, tx) {
		breakBlock(c, pos, tx)
	}
}

// RandomTick ...
func (c ChorusFlower) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if !c.canSurvive(pos, tx) {
		breakBlock(c, pos, tx)
		return
	}
	if c.Age >= 5 {
		// Dead chorus flowers do not grow.
		return
	}

	up := pos.Side(cube.FaceUp)
	if up.Y() > tx.Range().Max() {
		return
	}
	if _, ok := tx.Block(up).(Air); !ok {
		return
	}

	// Ensure a second air block above exists, like in vanilla.
	if up2 := up.Side(cube.FaceUp); up2.Y() > tx.Range().Max() {
		return
	} else if _, ok := tx.Block(up2).(Air); !ok {
		return
	}

	var (
		flag  bool
		flag1 bool
	)

	down := pos.Side(cube.FaceDown)
	switch tx.Block(down).(type) {
	case EndStone:
		flag = true
	case ChorusPlant:
		j := 1
		for k := 0; k < 4; k++ {
			below := pos.Sub(cube.Pos{0, j + 1})
			if _, ok := tx.Block(below).(ChorusPlant); !ok {
				if _, ok := tx.Block(below).(EndStone); ok {
					flag1 = true
				}
				break
			}
			j++
		}
		i1 := 4
		if flag1 {
			i1++
		}
		if j < 2 || r.IntN(i1) >= j {
			flag = true
		}
	case Air:
		flag = true
	}

	if flag && areAllHorizontalNeighborsAir(tx, up, cube.FaceDown) {
		tx.SetBlock(pos, ChorusPlant{}, nil)
		tx.SetBlock(up, ChorusFlower{Age: c.Age}, nil)
		return
	}

	if c.Age < 4 {
		l := r.IntN(4)
		if flag1 {
			l++
		}

		branched := false
		for j1 := 0; j1 < l; j1++ {
			face := cube.HorizontalFaces()[r.IntN(4)]
			side := pos.Side(face)

			if _, ok := tx.Block(side).(Air); !ok {
				continue
			}
			if _, ok := tx.Block(side.Side(cube.FaceDown)).(Air); !ok {
				continue
			}
			if !areAllHorizontalNeighborsAir(tx, side, face.Opposite()) {
				continue
			}

			tx.SetBlock(side, ChorusFlower{Age: c.Age + 1}, nil)
			branched = true
		}

		if branched {
			tx.SetBlock(pos, ChorusPlant{}, nil)
		} else {
			tx.SetBlock(pos, ChorusFlower{Age: 5}, nil)
		}
		return
	}

	if c.Age == 4 {
		tx.SetBlock(pos, ChorusFlower{Age: 5}, nil)
	}
}

func areAllHorizontalNeighborsAir(tx *world.Tx, pos cube.Pos, excluding cube.Face) bool {
	for _, face := range cube.HorizontalFaces() {
		if face == excluding {
			continue
		}
		if _, ok := tx.Block(pos.Side(face)).(Air); !ok {
			return false
		}
	}
	return true
}

// HasLiquidDrops ...
func (ChorusFlower) HasLiquidDrops() bool {
	return true
}

// FlammabilityInfo ...
func (ChorusFlower) FlammabilityInfo() FlammabilityInfo {
	return newFlammabilityInfo(60, 100, false)
}

// CompostChance ...
func (ChorusFlower) CompostChance() float64 {
	return 0.65
}

// UseOnBlock ...
func (c ChorusFlower) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	c.Age = 0
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used || !c.canSurvive(pos, tx) {
		return false
	}
	place(tx, pos, c, user, ctx)
	return placed(ctx)
}

// EncodeItem ...
func (ChorusFlower) EncodeItem() (name string, meta int16) {
	return "minecraft:chorus_flower", 0
}

// EncodeBlock ...
func (c ChorusFlower) EncodeBlock() (name string, properties map[string]any) {
	return "minecraft:chorus_flower", map[string]any{"age": int32(c.Age)}
}

func (c ChorusFlower) canSurvive(pos cube.Pos, tx *world.Tx) bool {
	switch tx.Block(pos.Side(cube.FaceDown)).(type) {
	case ChorusPlant, EndStone:
		return true
	case Air:
		plants := 0
		for _, face := range cube.HorizontalFaces() {
			switch tx.Block(pos.Side(face)).(type) {
			case ChorusPlant:
				plants++
			case Air:
				// continue
			default:
				return false
			}
		}
		return plants == 1
	default:
		return false
	}
}

func allChorusFlowers() (b []world.Block) {
	for age := 0; age <= 5; age++ {
		b = append(b, ChorusFlower{Age: age})
	}
	return b
}
