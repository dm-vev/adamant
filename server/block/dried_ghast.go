package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// DriedGhast is a block that rehydrates in water and can spawn a happy ghastling.
type DriedGhast struct {
	transparent
	solid
	sourceWaterDisplacer

	Facing      cube.Direction
	Rehydration int
}

func (d DriedGhast) normalized() DriedGhast {
	if d.Rehydration < 0 {
		d.Rehydration = 0
	} else if d.Rehydration > 3 {
		d.Rehydration = 3
	}
	if d.Facing != cube.North && d.Facing != cube.East && d.Facing != cube.South && d.Facing != cube.West {
		d.Facing = cube.North
	}
	return d
}

// allDriedGhast returns all native dried ghast block states.
func allDriedGhast() (blocks []world.Block) {
	for _, facing := range []cube.Direction{cube.North, cube.East, cube.South, cube.West} {
		for rehydration := 0; rehydration <= 3; rehydration++ {
			blocks = append(blocks, DriedGhast{Facing: facing, Rehydration: rehydration})
		}
	}
	return
}

// Model returns the dried ghast's collision model.
func (DriedGhast) Model() world.BlockModel {
	return driedGhastModel{}
}

type driedGhastModel struct{}

func (driedGhastModel) BBox(cube.Pos, world.BlockSource) []cube.BBox {
	return []cube.BBox{cube.Box(.1875, 0, .1875, .8125, .625, .8125)}
}

func (driedGhastModel) FaceSolid(cube.Pos, cube.Face, world.BlockSource) bool {
	return false
}

func (d DriedGhast) BreakInfo() BreakInfo {
	return newBreakInfo(0, alwaysHarvestable, nothingEffective, oneOf(d.normalized()))
}

func (DriedGhast) EncodeItem() (string, int16) {
	return "minecraft:dried_ghast", 0
}

func (d DriedGhast) EncodeBlock() (string, map[string]any) {
	d = d.normalized()
	return "minecraft:dried_ghast", map[string]any{
		"minecraft:cardinal_direction": d.Facing.String(),
		"rehydration_level":            int32(d.Rehydration),
	}
}

func (d DriedGhast) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, ok := firstReplaceable(tx, pos, face, d)
	if !ok {
		return false
	}
	d.Facing = cube.North
	if user != nil {
		d.Facing = user.Rotation().Direction().Opposite()
	}
	place(tx, pos, d, user, ctx)
	return placed(ctx)
}

func (d DriedGhast) Placed(pos cube.Pos, tx *world.Tx, previous world.Block) {
	if _, ok := previous.(DriedGhast); ok {
		return
	}
	soundName := "block.dried_ghast.place"
	if liquid, ok := tx.Liquid(pos); ok && liquid.LiquidType() == "water" {
		soundName = "block.dried_ghast.place_in_water"
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: soundName, Volume: 1, Pitch: 1})
}

// RandomTick starts the source-defined 5,000-game-tick delay for hydration or dehydration.
func (d DriedGhast) RandomTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if liquid, ok := tx.Liquid(pos); (ok && liquid.LiquidType() == "water") || d.Rehydration > 0 {
		tx.ScheduleBlockUpdate(pos, d, 250*time.Second)
	}
}

// ScheduledTick performs one source-defined hydration or dehydration step.
func (d DriedGhast) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	d = d.normalized()
	liquid, waterlogged := tx.Liquid(pos)
	waterlogged = waterlogged && liquid.LiquidType() == "water"
	if waterlogged {
		if d.Rehydration < 3 {
			d.Rehydration++
			tx.SetBlock(pos, d, nil)
			tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.dried_ghast.transition", Volume: 1, Pitch: 1})
			return
		}
		spawn := tx.World().EntityRegistry().Config().SpawnMob
		tx.SetBlock(pos, nil, nil)
		if spawn == nil {
			return
		}
		opts := world.EntitySpawnOpts{
			Position:   pos.Vec3().Add(mgl64.Vec3{.5, 0, .5}),
			Rotation:   cube.Rotation{ghastYaw(d.Facing), 0},
			SpawnReason: "breeding",
		}
		ghastling := spawn("minecraft:happy_ghast", opts)
		if ghastling == nil {
			return
		}
		newborn := tx.AddEntity(ghastling)
		tx.PlaySound(newborn.Position(), sound.Custom{Name: "entity.ghastling.spawn", Volume: 1, Pitch: 1})
		return
	}
	if d.Rehydration > 0 {
		d.Rehydration--
		tx.SetBlock(pos, d, nil)
	}
}

func (DriedGhast) SideClosed(cube.Pos, cube.Pos, *world.Tx) bool { return false }

func (DriedGhast) ReplaceableBy(world.Block) bool { return false }

func ghastYaw(d cube.Direction) float64 {
	switch d {
	case cube.South:
		return 0
	case cube.West:
		return 90
	case cube.North:
		return 180
	case cube.East:
		return -90
	default:
		return 0
	}
}

var _ world.RandomTicker = DriedGhast{}
var _ world.ScheduledTicker = DriedGhast{}
var _ world.PlacementHandler = DriedGhast{}
var _ world.LiquidDisplacer = DriedGhast{}
var _ world.BlockModel = driedGhastModel{}
