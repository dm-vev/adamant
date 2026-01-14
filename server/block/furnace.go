package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"math/rand/v2"
	"time"
)

// Furnace is a utility block used for the smelting of blocks and items.
// The empty value of Furnace is not valid. It must be created using block.NewFurnace(cube.Face).
type Furnace struct {
	solid
	bassDrum
	*smelter

	// Facing is the direction the furnace is facing.
	Facing cube.Direction
	// Lit is true if the furnace is lit.
	Lit bool
}

// NewFurnace creates a new initialised furnace. The smelter is properly initialised.
func NewFurnace(face cube.Direction) Furnace {
	return Furnace{
		Facing:  face,
		smelter: newSmelter(),
	}
}

// Tick is called to check if the furnace should update and start or stop smelting.
func (f Furnace) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if f.Lit && rand.Float64() <= 0.016 { // Every three or so seconds.
		tx.PlaySound(pos.Vec3Centre(), sound.FurnaceCrackle{})
	}
	if lit := f.smelter.tickSmelting(time.Second*10, f.Lit, func(item.SmeltInfo) bool {
		return true
	}); f.Lit != lit {
		f.Lit = lit
		tx.SetBlock(pos, f, nil)
	}
}

// EncodeItem ...
func (f Furnace) EncodeItem() (name string, meta int16) {
	return "minecraft:furnace", 0
}

// EncodeBlock ...
func (f Furnace) EncodeBlock() (name string, properties map[string]interface{}) {
	if f.Lit {
		return "minecraft:lit_furnace", map[string]interface{}{"minecraft:cardinal_direction": f.Facing.String()}
	}
	return "minecraft:furnace", map[string]interface{}{"minecraft:cardinal_direction": f.Facing.String()}
}

// UseOnBlock ...
func (f Furnace) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, f)
	if !used {
		return false
	}

	place(tx, pos, NewFurnace(user.Rotation().Direction().Opposite()), user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (f Furnace) BreakInfo() BreakInfo {
	xp := f.Experience()
	return newBreakInfo(3.5, alwaysHarvestable, pickaxeEffective, oneOf(f)).withXPDropRange(xp, xp).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		for _, i := range f.Inventory(tx, pos).Clear() {
			dropItem(tx, i, pos.Vec3())
		}
	})
}

// Activate ...
func (f Furnace) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

// EncodeNBT ...
func (f Furnace) EncodeNBT() map[string]interface{} {
	if f.smelter == nil {
		//noinspection GoAssignmentToReceiver
		f = NewFurnace(f.Facing)
	}
	remaining, maximum, cook := f.Durations()
	burnTimeTicks := smelterTicks(remaining)
	cookTimeTicks := smelterTicks(cook)
	burnDurationTicks := smelterBurnDurationTicks(remaining, maximum, time.Second*10)
	maxTimeTicks := smelterTicks(maximum)
	return map[string]interface{}{
		"BurnTime": burnTimeTicks,
		"CookTime": cookTimeTicks,
		// BurnDuration is the UI-scaled burn progress, while MaxTime persists the full fuel duration.
		"BurnDuration": burnDurationTicks,
		"MaxTime":      maxTimeTicks,
		"StoredXpInt":  int16(f.Experience()),
		"Items":        nbtconv.InvToNBT(f.inventory),
		"id":           "Furnace",
	}
}

// DecodeNBT ...
func (f Furnace) DecodeNBT(data map[string]interface{}) interface{} {
	burnTimeTicks := nbtconv.Int16(data, "BurnTime")
	if burnTimeTicks < 0 {
		burnTimeTicks = 0
	}
	cookTimeTicks := nbtconv.Int16(data, "CookTime")
	if cookTimeTicks < 0 {
		cookTimeTicks = 0
	}
	burnDurationTicks := nbtconv.Int16(data, "BurnDuration")
	if burnDurationTicks < 0 {
		burnDurationTicks = 0
	}
	maxTimeTicks := nbtconv.Int16(data, "MaxTime")
	if maxTimeTicks < 0 {
		maxTimeTicks = 0
	}

	requirementTicks := smelterTicks(time.Second * 10)
	maxTimeTicks = smelterNBTMaxTimeTicks(burnTimeTicks, maxTimeTicks, burnDurationTicks, requirementTicks)
	if burnTimeTicks == 0 {
		cookTimeTicks = 0
	}

	remaining := time.Duration(burnTimeTicks) * time.Millisecond * 50
	maximum := time.Duration(maxTimeTicks) * time.Millisecond * 50
	cook := time.Duration(cookTimeTicks) * time.Millisecond * 50

	xpValue, ok := data["StoredXpInt"].(int16)
	if !ok {
		xpValue, _ = data["StoredXPInt"].(int16)
	}
	xp := int(xpValue)
	lit := f.Lit

	//noinspection GoAssignmentToReceiver
	f = NewFurnace(f.Facing)
	f.Lit = lit
	f.setExperience(xp)
	f.setDurations(remaining, maximum, cook)
	nbtconv.InvFromNBT(f.inventory, nbtconv.Slice(data, "Items"))
	return f
}

// allFurnaces ...
func allFurnaces() (furnaces []world.Block) {
	for _, face := range cube.Directions() {
		furnaces = append(furnaces, Furnace{Facing: face})
		furnaces = append(furnaces, Furnace{Facing: face, Lit: true})
	}
	return
}
