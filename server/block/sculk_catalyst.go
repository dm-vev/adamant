package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// SculkCatalyst absorbs the experience of mobs dying within eight blocks and
// spreads it through the ground as sculk charges.
type SculkCatalyst struct {
	solid

	// Bloom is set for eight ticks after the catalyst absorbed a death.
	Bloom bool

	spreader *sculkSpreader
}

// NewSculkCatalyst returns a catalyst with its own, empty charge spreader.
func NewSculkCatalyst() SculkCatalyst {
	return SculkCatalyst{spreader: &sculkSpreader{}}
}

// LightEmissionLevel ...
func (SculkCatalyst) LightEmissionLevel() uint8 { return 6 }

// BreakInfo ...
func (c SculkCatalyst) BreakInfo() BreakInfo {
	return newBreakInfo(3, alwaysHarvestable, hoeEffective, silkTouchOnlyDrop(SculkCatalyst{})).withXPDropRange(5, 5).withBlastResistance(3)
}

// UseOnBlock ...
func (c SculkCatalyst) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, used := firstReplaceable(tx, pos, face, c)
	if !used {
		return false
	}
	place(tx, pos, NewSculkCatalyst(), user, ctx)
	return placed(ctx)
}

// Tick ports SculkCatalystBlockEntity.serverTick: the charge cursors advance.
func (c SculkCatalyst) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	c = c.initialised(tx, pos)
	c.spreader.updateCursors(tx, pos, true)
}

// ScheduledTick ends the bloom.
func (c SculkCatalyst) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) {
	if c.Bloom {
		c.Bloom = false
		tx.SetBlock(pos, c, nil)
	}
}

// GameEventRadius ...
func (SculkCatalyst) GameEventRadius() int { return 8 }

// HandleGameEvent ports CatalystListener.handleGameEvent: the nearest catalyst
// to a dying entity consumes its experience as sculk charge and blooms.
func (c SculkCatalyst) HandleGameEvent(tx *world.Tx, pos cube.Pos, event world.GameEvent, position mgl64.Vec3, ctx world.GameEventContext) {
	xp := ctx.Experience
	if event != world.GameEventEntityDie || xp == nil || xp.Consumed {
		return
	}
	c = c.initialised(tx, pos)
	if xp.Eligible && xp.Amount > 0 {
		c.spreader.addCursors(cube.PosFromVec3(position.Add(mgl64.Vec3{0, 0.5})), xp.Amount)
	}
	xp.Consumed = true
	c.Bloom = true
	tx.SetBlock(pos, c, nil)
	tx.ScheduleBlockUpdate(pos, c, 8*time.Second/20)
	for range 2 {
		tx.AddParticle(pos.Vec3().Add(mgl64.Vec3{0.5 + rand.NormFloat64()*0.2, 1.15, 0.5 + rand.NormFloat64()*0.2}), particle.SculkSoul{})
	}
	tx.PlaySound(pos.Vec3Centre(), sound.NativeEvent{SoundType: "block.sculk_catalyst.bloom", ExtraData: -1})
}

// initialised gives a generated or loaded catalyst its own spreader.
func (c SculkCatalyst) initialised(tx *world.Tx, pos cube.Pos) SculkCatalyst {
	if c.spreader == nil {
		c.spreader = &sculkSpreader{}
		tx.SetBlockEntity(pos, c)
	}
	return c
}

// EncodeItem ...
func (SculkCatalyst) EncodeItem() (name string, meta int16) {
	return "minecraft:sculk_catalyst", 0
}

// EncodeBlock ...
func (c SculkCatalyst) EncodeBlock() (string, map[string]any) {
	return "minecraft:sculk_catalyst", map[string]any{"bloom": boolByte(c.Bloom)}
}

// EncodeNBT stores the charge cursors in the native catalyst actor.
func (c SculkCatalyst) EncodeNBT() map[string]any {
	cursors := []any{}
	if c.spreader != nil {
		for _, cursor := range c.spreader.cursors {
			m := map[string]any{
				"x":      int32(cursor.pos.X()),
				"y":      int32(cursor.pos.Y()),
				"z":      int32(cursor.pos.Z()),
				"charge": int32(cursor.charge),
				"decay":  int32(cursor.decayDelay),
				"update": int32(cursor.updateDelay),
			}
			if cursor.hasFacings {
				m["facing"] = int32(cursor.facings)
			}
			cursors = append(cursors, m)
		}
	}
	return map[string]any{"id": "SculkCatalyst", "cursors": cursors}
}

// DecodeNBT restores at most 32 charge cursors, clamped as Java's codec.
func (c SculkCatalyst) DecodeNBT(m map[string]any) any {
	c.spreader = &sculkSpreader{}
	for _, v := range nbtconv.Slice(m, "cursors") {
		cursor, ok := v.(map[string]any)
		if !ok {
			continue
		}
		decoded := &sculkChargeCursor{
			pos:         cube.Pos{int(nbtconv.Int32(cursor, "x")), int(nbtconv.Int32(cursor, "y")), int(nbtconv.Int32(cursor, "z"))},
			charge:      min(max(int(nbtconv.Int32(cursor, "charge")), 0), sculkMaxCharge),
			decayDelay:  min(max(int(nbtconv.Int32(cursor, "decay")), 0), 1),
			updateDelay: max(int(nbtconv.Int32(cursor, "update")), 0),
		}
		if _, ok := cursor["facing"]; ok {
			decoded.facings, decoded.hasFacings = MultiFace(nbtconv.Int32(cursor, "facing")&63), true
		}
		c.spreader.addCursor(decoded)
	}
	return c
}

// allSculkCatalysts returns both bloom states.
func allSculkCatalysts() []world.Block {
	return []world.Block{SculkCatalyst{}, SculkCatalyst{Bloom: true}}
}
