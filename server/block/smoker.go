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

// Smoker is a type of furnace that cooks food items, similar to a furnace, but twice as fast. It also serves as a
// butcher's job site block.
// The empty value of Smoker is not valid. It must be created using block.NewSmoker(cube.Face).
type Smoker struct {
	solid
	bassDrum
	*smelter

	// Facing is the direction the smoker is facing.
	Facing cube.Direction
	// Lit is true if the smoker is lit.
	Lit bool
}

// NewSmoker creates a new initialised smoker. The smelter is properly initialised.
func NewSmoker(face cube.Direction) Smoker {
	return Smoker{
		Facing:  face,
		smelter: newSmelter(),
	}
}

// Tick is called to check if the smoker should update and start or stop smelting.
func (s Smoker) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if s.Lit && rand.Float64() <= 0.016 { // Every three or so seconds.
		tx.PlaySound(pos.Vec3Centre(), sound.SmokerCrackle{})
	}
	if lit := s.smelter.tickSmelting(time.Second*5, s.Lit, func(i item.SmeltInfo) bool {
		return i.Food
	}); s.Lit != lit {
		s.Lit = lit
		tx.SetBlock(pos, s, nil)
	}
}

// EncodeItem ...
func (s Smoker) EncodeItem() (name string, meta int16) {
	return "minecraft:smoker", 0
}

// EncodeBlock ...
func (s Smoker) EncodeBlock() (name string, properties map[string]interface{}) {
	if s.Lit {
		return "minecraft:lit_smoker", map[string]interface{}{"minecraft:cardinal_direction": s.Facing.String()}
	}
	return "minecraft:smoker", map[string]interface{}{"minecraft:cardinal_direction": s.Facing.String()}
}

// UseOnBlock ...
func (s Smoker) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, s)
	if !used {
		return false
	}

	place(tx, pos, NewSmoker(user.Rotation().Direction().Opposite()), user, ctx)
	return placed(ctx)
}

// BreakInfo ...
func (s Smoker) BreakInfo() BreakInfo {
	xp := s.Experience()
	return newBreakInfo(3.5, alwaysHarvestable, pickaxeEffective, oneOf(s)).withXPDropRange(xp, xp).withBreakHandler(func(pos cube.Pos, tx *world.Tx, u item.User) {
		for _, i := range s.Inventory(tx, pos).Clear() {
			dropItem(tx, i, pos.Vec3())
		}
	})
}

// Activate ...
func (s Smoker) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, _ *item.UseContext) bool {
	if opener, ok := u.(ContainerOpener); ok {
		opener.OpenBlockContainer(pos, tx)
		return true
	}
	return false
}

// EncodeNBT ...
func (s Smoker) EncodeNBT() map[string]interface{} {
	if s.smelter == nil {
		//noinspection GoAssignmentToReceiver
		s = NewSmoker(s.Facing)
	}
	remaining, maximum, cook := s.Durations()
	burnTimeTicks := smelterTicks(remaining)
	cookTimeTicks := smelterTicks(cook)
	burnDurationTicks := smelterBurnDurationTicks(remaining, maximum, time.Second*5)
	maxTimeTicks := smelterTicks(maximum)
	return map[string]interface{}{
		"BurnTime": burnTimeTicks,
		"CookTime": cookTimeTicks,
		// BurnDuration is the UI-scaled burn progress, while MaxTime persists the full fuel duration.
		"BurnDuration": burnDurationTicks,
		"MaxTime":      maxTimeTicks,
		"StoredXpInt":  int16(s.Experience()),
		"Items":        nbtconv.InvToNBT(s.inventory),
		"id":           "Smoker",
	}
}

// DecodeNBT ...
func (s Smoker) DecodeNBT(data map[string]interface{}) interface{} {
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

	requirementTicks := smelterTicks(time.Second * 5)
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
	lit := s.Lit

	//noinspection GoAssignmentToReceiver
	s = NewSmoker(s.Facing)
	s.Lit = lit
	s.setExperience(xp)
	s.setDurations(remaining, maximum, cook)
	nbtconv.InvFromNBT(s.inventory, nbtconv.Slice(data, "Items"))
	return s
}

// allSmokers ...
func allSmokers() (smokers []world.Block) {
	for _, face := range cube.Directions() {
		smokers = append(smokers, Smoker{Facing: face})
		smokers = append(smokers, Smoker{Facing: face, Lit: true})
	}
	return
}
