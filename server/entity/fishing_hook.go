package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/fishing"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"math/rand/v2"
)

const (
	fishingHookGravity   = 0.07
	fishingHookDrag      = 0.05
	fishingHookLifetime  = 1200 // Ticks (60 seconds).
	minFishingWaitChance = 20
)

// FishingHookConfig holds configuration to spawn a fishing hook.
type FishingHookConfig struct {
	Owner *world.EntityHandle
	Rod   item.Stack
}

// Apply applies the configuration to the entity data.
func (conf FishingHookConfig) Apply(data *world.EntityData) {
	behaviour := &FishingHookBehaviour{
		owner: conf.Owner,
		mc: &MovementComputer{
			Gravity:           fishingHookGravity,
			Drag:              fishingHookDrag,
			DragBeforeGravity: true,
		},
	}

	for _, enchant := range conf.Rod.Enchantments() {
		switch enchant.Type() {
		case enchantment.LuckOfTheSea:
			behaviour.luckLevel = enchant.Level()
		case enchantment.Lure:
			behaviour.lureLevel = enchant.Level()
		}
	}
	behaviour.waitChance = maxInt(minFishingWaitChance, 120-25*behaviour.lureLevel)
	data.Data = behaviour
}

// FishingHookBehaviour implements the behaviour of a fishing hook entity.
type FishingHookBehaviour struct {
	owner *world.EntityHandle
	mc    *MovementComputer

	luckLevel int
	lureLevel int

	waitChance int
	biteTimer  int
	biteWindow int
	fishReady  bool
	started    bool

	target *world.EntityHandle

	lifeTicks int
}

// Owner returns the owner of the fishing hook.
func (b *FishingHookBehaviour) Owner() *world.EntityHandle {
	return b.owner
}

// Reel resolves the result of reeling in the fishing hook.
func (b *FishingHookBehaviour) Reel(e *Ent, tx *world.Tx) (loot item.Stack, experience int, pulled world.Entity) {
	if b.fishReady {
		loot = fishing.Loot(b.luckLevel, b.lureLevel)
		if !loot.Empty() {
			experience = rand.IntN(3) + 1
		}
		b.fishReady = false
	}
	if b.target != nil {
		if ent, ok := b.target.Entity(tx); ok {
			pulled = ent
		}
		b.target = nil
	}
	return
}

// Tick ticks the fishing hook behaviour, updating its movement and state.
func (b *FishingHookBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.owner != nil {
		if _, ok := b.owner.Entity(tx); !ok {
			_ = e.Close()
			return nil
		}
	}

	if b.target != nil && b.followTarget(e, tx) {
		return nil
	}

	if b.lifeTicks > fishingHookLifetime {
		_ = e.Close()
		return nil
	}
	b.lifeTicks++

	pos := e.Position()
	movement := b.mc.TickMovement(e, pos, e.Velocity(), e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = movement.Position(), movement.Velocity(), movement.Rotation()
	movement.Send()

	inWater := b.isInWater(tx, e.Position())
	if inWater {
		// Damp horizontal movement heavily when inside water.
		e.data.Vel[0] *= 0.2
		e.data.Vel[2] *= 0.2
		if e.data.Vel[1] < 0 {
			e.data.Vel[1] *= 0.5
		}

		if !b.started {
			b.started = true
			b.resetBiteTimer()
		}

		if b.biteTimer > 0 {
			b.biteTimer--
			if b.biteTimer == 0 {
				b.triggerBite(e, tx)
			}
		} else if b.fishReady && b.biteWindow > 0 {
			b.biteWindow--
			if b.biteWindow == 0 {
				b.resetBiteTimer()
			}
		}
	}

	if b.target == nil {
		b.detectCollision(e, tx)
	}

	return movement
}

func (b *FishingHookBehaviour) followTarget(e *Ent, tx *world.Tx) bool {
	if b.target == nil {
		return false
	}
	target, ok := b.target.Entity(tx)
	if !ok {
		b.target = nil
		return false
	}

	targetPos := target.Position()
	height := target.H().Type().BBox(target).Height()
	newPos := targetPos.Add(mgl64.Vec3{0, height * 0.75, 0})
	oldPos := e.Position()

	e.data.Pos = newPos
	e.data.Vel = mgl64.Vec3{}
	for _, viewer := range tx.Viewers(oldPos) {
		viewer.ViewEntityTeleport(e, newPos)
		viewer.ViewEntityVelocity(e, mgl64.Vec3{})
	}
	return true
}

func (b *FishingHookBehaviour) isInWater(tx *world.Tx, pos mgl64.Vec3) bool {
	blockPos := cube.PosFromVec3(pos)
	if liquid, ok := tx.Liquid(blockPos); ok && liquid.LiquidType() == "water" {
		return true
	}
	if liquid, ok := tx.Liquid(blockPos.Side(cube.FaceDown)); ok && liquid.LiquidType() == "water" {
		return true
	}
	if liquid, ok := tx.Liquid(blockPos.Side(cube.FaceUp)); ok && liquid.LiquidType() == "water" {
		return true
	}
	return false
}

func (b *FishingHookBehaviour) detectCollision(e *Ent, tx *world.Tx) {
	box := e.H().Type().BBox(e).Translate(e.Position()).Grow(0.2)
	for other := range tx.EntitiesWithin(box) {
		if other.H() == e.H() {
			continue
		}
		if b.owner != nil && other.H() == b.owner {
			continue
		}
		if _, ok := other.(Living); !ok {
			continue
		}
		b.target = other.H()
		return
	}
}

func (b *FishingHookBehaviour) triggerBite(e *Ent, tx *world.Tx) {
	b.fishReady = true
	b.biteWindow = 20 + rand.IntN(20)
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityAction(e, FishHookBubbleAction{})
		v.ViewEntityAction(e, FishHookBiteAction{})
		v.ViewEntityAction(e, FishHookTeaseAction{})
	}
}

func (b *FishingHookBehaviour) resetBiteTimer() {
	wait := maxInt(minFishingWaitChance, b.waitChance)
	b.biteTimer = wait + rand.IntN(wait)
	b.biteWindow = 0
	b.fishReady = false
}

// FishingHookType is a world.EntityType implementation for FishingHook.
var FishingHookType fishingHookType

type fishingHookType struct{}

func (f fishingHookType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (f fishingHookType) EncodeEntity() string { return "minecraft:fishing_hook" }

func (f fishingHookType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

func (f fishingHookType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	conf := FishingHookConfig{}
	conf.Apply(data)
}

func (f fishingHookType) EncodeNBT(_ *world.EntityData) map[string]any {
	return map[string]any{}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SurfaceHeight returns the Y position of the water surface at the hook position.
func (b *FishingHookBehaviour) SurfaceHeight(tx *world.Tx, pos cube.Pos) float64 {
	maxY := tx.Range()[1]
	for y := pos[1]; y < maxY; y++ {
		if liquid, ok := tx.Liquid(cube.Pos{pos[0], y, pos[2]}); !ok || liquid.LiquidType() != "water" {
			return float64(y)
		}
	}
	return float64(pos[1])
}

// PullVelocity computes the velocity applied to an entity when it is pulled by the hook.
func (b *FishingHookBehaviour) PullVelocity(owner world.Entity, hookPos mgl64.Vec3) mgl64.Vec3 {
	diff := owner.Position().Sub(hookPos).Mul(0.1)
	top := owner.Position().Add(mgl64.Vec3{0, owner.H().Type().BBox(owner).Height(), 0})
	diff[1] += math.Sqrt(top.Sub(hookPos).LenSqr()) * 0.08
	return diff
}
