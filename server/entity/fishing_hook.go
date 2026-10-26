package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/item/fishing"
	loottable "github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// fishingHookGravity is FishingHook.getDefaultGravity.
const fishingHookGravity = 0.03

// FishingHookConfig holds configuration to spawn a fishing hook.
type FishingHookConfig struct {
	Owner *world.EntityHandle
	Rod   item.Stack
}

// Apply applies the configuration to the entity data: the rod's Luck of
// the Sea gives the hook its luck and Lure takes five seconds a level off
// the wait for a fish, as FishingRodItem.use.
func (conf FishingHookConfig) Apply(data *world.EntityData) {
	b := &FishingHookBehaviour{owner: conf.Owner, openWater: true, mc: &MovementComputer{}}
	for _, enchant := range conf.Rod.Enchantments() {
		switch enchant.Type() {
		case enchantment.LuckOfTheSea:
			b.luck = enchant.Level()
		case enchantment.Lure:
			b.lureSpeed = enchant.Level() * 5 * 20
		}
	}
	data.Data = b
}

// fishHookState is FishingHook.FishHookState.
type fishHookState uint8

const (
	fishHookFlying fishHookState = iota
	fishHookHookedInEntity
	fishHookBobbing
)

// FishingHookBehaviour ports FishingHook: the hook flies until it hooks an
// entity or lands in water, where it bobs while fish are lured, approach
// and nibble.
type FishingHookBehaviour struct {
	owner *world.EntityHandle
	mc    *MovementComputer

	luck, lureSpeed int

	state    fishHookState
	hookedIn *world.EntityHandle

	life                            int
	nibble                          int
	timeUntilLured, timeUntilHooked int
	fishAngle                       float32
	biting                          bool
	outOfWaterTime                  int
	openWater                       bool
}

// Biting reports if a fish nibbles at the hook, FishingHook.DATA_BITING.
func (b *FishingHookBehaviour) Biting() bool { return b.biting }

// Owner returns the owner of the fishing hook.
func (b *FishingHookBehaviour) Owner() *world.EntityHandle {
	return b.owner
}

// Tick ports FishingHook.tick.
func (b *FishingHookBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	owner, ok := b.ownerEntity(tx)
	if !ok || b.shouldStopFishing(e, owner) {
		_ = e.CloseIn(tx)
		return nil
	}
	if b.mc.OnGround() {
		if b.life++; b.life >= 1200 {
			_ = e.CloseIn(tx)
			return nil
		}
	} else {
		b.life = 0
	}

	blockPos := cube.PosFromVec3(e.Position())
	liquidHeight := 0.0
	liquid, waterHere := tx.Liquid(blockPos)
	if waterHere = waterHere && liquid.LiquidType() == "water"; waterHere {
		liquidHeight = fishingFluidHeight(tx, blockPos, liquid)
	}
	inWater := liquidHeight > 0

	switch b.state {
	case fishHookFlying:
		if b.hookedIn != nil {
			e.data.Vel = mgl64.Vec3{}
			b.state = fishHookHookedInEntity
			return nil
		}
		if inWater {
			e.data.Vel = mgl64.Vec3{e.data.Vel[0] * 0.3, e.data.Vel[1] * 0.2, e.data.Vel[2] * 0.3}
			b.state = fishHookBobbing
			return nil
		}
		b.checkCollision(e, tx, owner)
	case fishHookHookedInEntity:
		if b.hookedIn != nil {
			if hooked, ok := b.hookedIn.Entity(tx); ok && entityAliveForHook(hooked) {
				pos := hooked.Position()
				pos[1] += hooked.H().Type().BBox(hooked).Height() * 0.8
				prev := e.Position()
				e.data.Pos, e.data.Vel = pos, mgl64.Vec3{}
				viewers := tx.Viewers(prev)
				for _, v := range viewers {
					v.ViewEntityTeleport(e, pos)
				}
				tx.ReleaseViewers(viewers)
			} else {
				b.hookedIn = nil
				b.state = fishHookFlying
			}
		}
		return nil
	case fishHookBobbing:
		vel := e.data.Vel
		force := e.Position()[1] + vel[1] - float64(blockPos[1]) - liquidHeight
		if math.Abs(force) < 0.01 {
			force += math.Copysign(0.1, force)
		}
		e.data.Vel = mgl64.Vec3{vel[0] * 0.9, vel[1] - force*float64(rand.Float32())*0.2, vel[2] * 0.9}
		if b.nibble <= 0 && b.timeUntilHooked <= 0 {
			b.openWater = true
		} else {
			b.openWater = b.openWater && b.outOfWaterTime < 10 && b.calculateOpenWater(tx, blockPos)
		}
		if inWater {
			b.outOfWaterTime = max(0, b.outOfWaterTime-1)
			if b.biting {
				e.data.Vel[1] += -0.1 * float64(rand.Float32()*rand.Float32())
			}
			b.catchingFish(e, tx, blockPos)
		} else {
			b.outOfWaterTime = min(10, b.outOfWaterTime+1)
		}
	}

	if !waterHere && !b.mc.OnGround() && b.hookedIn == nil {
		e.data.Vel[1] -= fishingHookGravity
	}
	prev, prevVel := e.Position(), e.data.Vel
	dPos, vel := b.mc.CheckCollision(tx, e, prev, e.data.Vel)
	e.data.Pos = prev.Add(dPos)
	if b.state == fishHookFlying && (b.mc.OnGround() || b.mc.CollidedHorizontally()) {
		vel = mgl64.Vec3{}
	}
	e.data.Vel = vel.Mul(0.92)
	viewers := tx.Viewers(prev)
	return &Movement{v: viewers, release: func() { tx.ReleaseViewers(viewers) }, e: e,
		pos: e.data.Pos, vel: e.data.Vel, dpos: dPos, dvel: e.data.Vel.Sub(prevVel), rot: e.data.Rot, onGround: b.mc.OnGround()}
}

// fishingFluidHeight ports FluidState.getHeight for water: a full block below
// water of its kind, otherwise its amount over nine.
func fishingFluidHeight(tx *world.Tx, pos cube.Pos, l world.Liquid) float64 {
	if above, ok := tx.Liquid(pos.Side(cube.FaceUp)); ok && above.LiquidType() == l.LiquidType() {
		return 1
	}
	if l.LiquidFalling() {
		return 8.0 / 9
	}
	return float64(l.LiquidDepth()) / 9
}

func entityAliveForHook(e world.Entity) bool {
	if l, ok := e.(Living); ok {
		return !l.Dead()
	}
	return true
}

func (b *FishingHookBehaviour) ownerEntity(tx *world.Tx) (world.Entity, bool) {
	if b.owner == nil {
		return nil, false
	}
	return b.owner.Entity(tx)
}

// shouldStopFishing ports FishingHook.shouldStopFishing: the owner must hold
// a fishing rod and be within 32 blocks.
func (b *FishingHookBehaviour) shouldStopFishing(e *Ent, owner world.Entity) bool {
	if l, ok := owner.(Living); ok && l.Dead() {
		return true
	}
	holder, ok := owner.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return true
	}
	main, off := holder.HeldItems()
	_, mainRod := main.Item().(item.FishingRod)
	_, offRod := off.Item().(item.FishingRod)
	return !(mainRod || offRod) || owner.Position().Sub(e.Position()).LenSqr() > 1024
}

// checkCollision ports FishingHook.checkCollision for entities: the hook
// catches the first living entity or item along its movement other than its
// owner.
func (b *FishingHookBehaviour) checkCollision(e *Ent, tx *world.Tx, owner world.Entity) {
	box := e.H().Type().BBox(e).Translate(e.Position()).Extend(e.data.Vel).Grow(1)
	for other := range tx.EntitiesWithin(box) {
		if other.H() == e.H() || other.H() == owner.H() {
			continue
		}
		_, living := other.(Living)
		if !living && other.H().Type().EncodeEntity() != "minecraft:item" {
			continue
		}
		if trace.BBoxIntersects(other.H().Type().BBox(other).Translate(other.Position()).Grow(0.3), e.Position(), e.Position().Add(e.data.Vel)) {
			b.hookedIn = other.H()
			return
		}
	}
}

// catchingFish ports FishingHook.catchingFish: rain above speeds fishing up,
// a covered sky slows it down; fish are lured for 100 to 600 ticks less the
// lure speed, approach for 20 to 80 and nibble for 20 to 40.
func (b *FishingHookBehaviour) catchingFish(e *Ent, tx *world.Tx, blockPos cube.Pos) {
	speed := 1
	above := blockPos.Side(cube.FaceUp)
	if rand.Float32() < 0.25 && tx.RainingAt(above) {
		speed++
	}
	// Level.canSeeSky: the sky light there is full.
	if rand.Float32() < 0.5 && tx.SkyLight(above) < 15 {
		speed--
	}
	switch {
	case b.nibble > 0:
		if b.nibble--; b.nibble <= 0 {
			b.timeUntilLured, b.timeUntilHooked, b.biting = 0, 0, false
		}
	case b.timeUntilHooked > 0:
		if b.timeUntilHooked -= speed; b.timeUntilHooked > 0 {
			b.fishAngle += float32(9.188 * (rand.Float64() - rand.Float64()))
			if rand.Float32() < 0.15 {
				b.viewAction(e, tx, FishHookBubbleAction{})
			}
		} else {
			tx.AddParticle(e.Position().Add(mgl64.Vec3{0, 0.5}), particle.Splash{})
			b.nibble = 20 + rand.IntN(21)
			b.biting = true
			b.viewAction(e, tx, FishHookBiteAction{})
		}
	case b.timeUntilLured > 0:
		b.timeUntilLured -= speed
		tease := float32(0.15)
		switch {
		case b.timeUntilLured < 20:
			tease += float32(20-b.timeUntilLured) * 0.05
		case b.timeUntilLured < 40:
			tease += float32(40-b.timeUntilLured) * 0.02
		case b.timeUntilLured < 60:
			tease += float32(60-b.timeUntilLured) * 0.01
		}
		if rand.Float32() < tease {
			b.viewAction(e, tx, FishHookTeaseAction{})
		}
		if b.timeUntilLured <= 0 {
			b.fishAngle = rand.Float32() * 360
			b.timeUntilHooked = 20 + rand.IntN(61)
		}
	default:
		b.timeUntilLured = 100 + rand.IntN(501) - b.lureSpeed
	}
}

func (b *FishingHookBehaviour) viewAction(e *Ent, tx *world.Tx, a world.EntityAction) {
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers {
		v.ViewEntityAction(e, a)
	}
	tx.ReleaseViewers(viewers)
}

// openWaterType is FishingHook.OpenWaterType.
type openWaterType uint8

const (
	openWaterAbove openWaterType = iota
	openWaterInside
	openWaterInvalid
)

// calculateOpenWater ports FishingHook.calculateOpenWater: the 5x5 layers
// from below the hook up to two above are water sources and then air, never
// water again above air.
func (b *FishingHookBehaviour) calculateOpenWater(tx *world.Tx, pos cube.Pos) bool {
	previous := openWaterInvalid
	for y := -1; y <= 2; y++ {
		layer := openWaterInvalid
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				t := b.openWaterTypeFor(tx, pos.Add(cube.Pos{x, y, z}))
				if x == -2 && z == -2 {
					layer = t
				} else if t != layer {
					layer = openWaterInvalid
				}
			}
		}
		switch layer {
		case openWaterAbove:
			if previous == openWaterInvalid {
				return false
			}
		case openWaterInside:
			if previous == openWaterAbove {
				return false
			}
		case openWaterInvalid:
			return false
		}
		previous = layer
	}
	return true
}

// openWaterTypeFor ports FishingHook.getOpenWaterTypeForBlock.
func (b *FishingHookBehaviour) openWaterTypeFor(tx *world.Tx, pos cube.Pos) openWaterType {
	bl := tx.Block(pos)
	name, _ := bl.EncodeBlock()
	if name == "minecraft:air" || name == "minecraft:waterlily" {
		return openWaterAbove
	}
	l, ok := tx.Liquid(pos)
	if ok && l.LiquidType() == "water" && l.LiquidDepth() == 8 && !l.LiquidFalling() && len(bl.Model().BBox(pos, tx)) == 0 {
		return openWaterInside
	}
	return openWaterInvalid
}

// Retrieve ports FishingHook.retrieve: a hooked entity is pulled towards the
// owner, a nibbling fish yields the fishing loot flying to the owner and an
// experience orb at the owner for each item. It returns the durability the
// rod loses: 5 for a pulled entity, 3 for a pulled item, 1 for a catch, 2 if
// the hook lies on the ground.
func (b *FishingHookBehaviour) Retrieve(e *Ent, tx *world.Tx) (int, error) {
	owner, ok := b.ownerEntity(tx)
	if !ok || b.shouldStopFishing(e, owner) {
		_ = e.CloseIn(tx)
		return 0, nil
	}
	dmg := 0
	var err error
	if b.hookedIn != nil {
		if hooked, ok := b.hookedIn.Entity(tx); ok {
			if mover, ok := hooked.(interface {
				Velocity() mgl64.Vec3
				SetVelocity(mgl64.Vec3)
			}); ok {
				mover.SetVelocity(mover.Velocity().Add(owner.Position().Sub(e.Position()).Mul(0.1)))
			}
			dmg = 5
			if hooked.H().Type().EncodeEntity() == "minecraft:item" {
				dmg = 3
			}
		}
	} else if b.nibble > 0 {
		var catch item.Stack
		catch, err = b.loot(e, tx, owner)
		if !catch.Empty() {
			d := owner.Position().Sub(e.Position())
			vel := mgl64.Vec3{d[0] * 0.1, d[1]*0.1 + math.Sqrt(math.Sqrt(d.Len()))*0.08, d[2] * 0.1}
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: e.Position(), Velocity: vel}, catch))
			for _, orb := range NewExperienceOrbs(owner.Position().Add(mgl64.Vec3{0, 0.5, 0.5}), rand.IntN(6)+1) {
				tx.AddEntity(orb)
			}
		}
		dmg = 1
	}
	if b.mc.OnGround() {
		dmg = 2
	}
	_ = e.CloseIn(tx)
	return dmg, err
}

// loot rolls the fishing loot table with the hook's luck and the owner's.
func (b *FishingHookBehaviour) loot(e *Ent, tx *world.Tx, owner world.Entity) (item.Stack, error) {
	sequence, err := loottable.RandomSequence("minecraft:gameplay/fishing")
	if err != nil {
		return item.Stack{}, err
	}
	luck := b.luck
	if l, ok := owner.(interface{ Luck() float64 }); ok {
		luck += int(l.Luck())
	}
	rod, _ := owner.(interface{ HeldItems() (item.Stack, item.Stack) }).HeldItems()
	if _, ok := rod.Item().(item.FishingRod); !ok {
		_, rod = owner.(interface{ HeldItems() (item.Stack, item.Stack) }).HeldItems()
	}
	return fishing.LootWithRandom(luck, b.openWater, tx.LootRandomSequence(sequence, 0), e, rod)
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
