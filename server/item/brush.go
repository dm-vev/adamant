package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Brush is a tool used to uncover archaeological finds and brush armadillos.
type Brush struct{}

// MaxCount returns the brush stack limit.
func (Brush) MaxCount() int { return 1 }

// DurabilityInfo returns Java's brush durability.
func (Brush) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{MaxDurability: 64, BrokenItem: simpleItem(Stack{}), BreakDurability: 1}
}

// EncodeItem returns the Bedrock item identifier.
func (Brush) EncodeItem() (name string, meta int16) { return "minecraft:brush", 0 }

// UseOnEntity brushes an entity that exposes the Java brush interaction.
func (Brush) UseOnEntity(target world.Entity, _ *world.Tx, user User, ctx *UseContext) bool {
	brushable, ok := target.(interface{ Brush() Stack })
	if !ok {
		return false
	}
	drop := brushable.Brush()
	if drop.Empty() {
		return false
	}
	if dropper, ok := user.(interface {
		DropItemAt(mgl64.Vec3, Stack, mgl64.Vec3)
	}); ok {
		pos := target.Position()
		pos[1] += 0.5
		dropper.DropItemAt(pos, drop, mgl64.Vec3{0, 0.1})
	} else {
		ctx.NewItem = drop
		ctx.NewItemSurvivalOnly = true
	}
	// Armadillo.mobInteract wears the brush down by 16.
	ctx.DamageItem(16)
	return true
}

// UseOnBlock starts Java's held brush animation when a block is under the player's view.
func (Brush) UseOnBlock(_ cube.Pos, _ cube.Face, _ mgl64.Vec3, _ *world.Tx, user User, _ *UseContext) bool {
	starter, ok := user.(interface{ StartBrushUse() bool })
	return ok && starter.StartBrushUse()
}

// UseTick applies Java's five-tick wind-up and ten-tick brush cadence.
func (Brush) UseTick(tx *world.Tx, user User, stack Stack, ticks int) Stack {
	targeter, ok := user.(interface {
		BrushTarget(*world.Tx) (cube.Pos, cube.Face, bool)
	})
	if !ok {
		user.ReleaseItem()
		return stack
	}
	pos, face, hit := targeter.BrushTarget(tx)
	if !hit {
		user.ReleaseItem()
		return stack
	}
	if ticks%10 != 5 {
		return stack
	}
	block := tx.Block(pos)
	soundName := "item.brush.brushing.generic"
	if b, ok := block.(interface{ BrushSound() string }); ok {
		soundName = b.BrushSound()
	}
	tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: soundName, Volume: 1, Pitch: 1})
	for range 9 {
		tx.AddParticle(pos.Vec3Centre(), particle.PunchBlock{Block: block, Face: face})
	}
	brushable, ok := block.(interface {
		BrushAt(cube.Pos, cube.Face, *world.Tx, User, Stack) (bool, error)
	})
	if !ok {
		return stack
	}
	completed, err := brushable.BrushAt(pos, face, tx, user, stack)
	if err != nil {
		if logger, ok := user.(interface{ LogError(string, ...any) }); ok {
			logger.LogError("brush archaeological block at %v: %v", pos, err)
		}
		return stack
	}
	if completed {
		if damager, ok := user.(interface{ DamageItem(Stack, int) Stack }); ok {
			return damager.DamageItem(stack, 1)
		}
		return stack.Damage(1)
	}
	return stack
}

// UseDurationTicks returns Java's 200-tick held-use duration.
func (Brush) UseDurationTicks() int { return 200 }
