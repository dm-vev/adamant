package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// javaRuntime is what the player keeps of Java's server player, and what its
// ticks send.
type javaRuntime struct {
	javaState          map[string]any
	wardenSpawnTracker *world.WardenSpawnTracker
	// tickCount is Entity.tickCount, the ticks the player has existed in
	// this session.
	tickCount int64
	// sentFood is the food state last sent to the client.
	sentFood hungerSnapshot
}

// Tick ticks the entity, performing actions such as checking if the player is still breaking a block.
//
// It runs the phases of ServerPlayer.tick and the baseTick of the living entity in the order the player has
// always had them: the sleep, the surroundings (liquid, glide, freezing, the ground), the effects, food and air,
// the damage of the world (void, walls, fire), the item and the block in use, the housekeeping of the
// anti-cheat and the client, and the movement.
func (p *Player) Tick(tx *world.Tx, current int64) {
	p.bindTx(tx)
	p.showMove()
	p.wardenSpawnTracker.Tick()
	p.itemUseTick = current
	p.nextTick()
	if p.Dead() {
		p.ReleaseShoulderParrots()
		p.cancelItemUse()
		return
	}
	p.tickSleeping(tx)
	p.sleepTick()
	p.tickShoulderParrots()
	p.tickSpectating()
	p.tickCarriedMaps(current)
	p.tickScoreboard(tx, current)
	p.tickMapFrames(current)
	p.tickSwimming()
	p.tickFallFlying()

	p.checkBlockCollisions(p.data.Vel)
	p.tickFreezing()
	p.onGround = p.checkOnGround(mgl64.Vec3{})
	p.tickFrostWalker()
	p.checkEntitySteppers()

	p.effects.Tick(p, p.tx)

	p.tickFood()
	p.tickAirSupply()

	p.checkBelowWorld()
	p.tickInsideWall()
	p.tickFire()
	p.tickBlockingDelay()

	if p.usingItem {
		p.tickItemUse(current)
	}
	if p.breaking {
		p.ContinueBreaking(p.breakingFace)
	}
	p.checkMenuReach()

	p.expireCooldowns()
	p.tickAttackStrength()

	p.client().SendDebugShapes(tx.World().Dimension())
	p.client().SendHudUpdates()

	p.tickChangedWorld(tx)
	p.travel()

	p.portalTravel.StopPortalContact()
}

// tickSleeping wakes a sleeping player whose bed is no longer one to sleep in: it is gone, it is day, or a
// straw bed has lost its room.
func (p *Player) tickSleeping(tx *world.Tx) {
	if !p.sleeping {
		return
	}
	if bed, loaded := tx.BlockLoaded(p.sleepPos); loaded {
		switch bed := bed.(type) {
		case block.StrawBed:
			if _, valid := bed.SleepHeight(p.sleepPos, tx); !valid || !tx.IsDarkOutside() {
				p.wakeInTx(tx)
			}
		case block.Bed:
			if !tx.IsDarkOutside() {
				p.wakeInTx(tx)
			}
		default:
			p.wakeInTx(tx)
		}
	}
}

// tickShoulderParrots releases the parrots on the shoulders of a player that moves too much for them to stay:
// in water, flying, sleeping, falling or in powder snow (Player.removeEntitiesOnShoulder).
func (p *Player) tickShoulderParrots() {
	blockName := ""
	if b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(p.Position())); loaded {
		blockName, _ = b.EncodeBlock()
	}
	if p.insideOfWater() || p.Flying() || p.sleeping || p.fallDistance > 0.5 || blockName == "minecraft:powder_snow" {
		p.ReleaseShoulderParrots()
	}
}

// tickSwimming stops the player swimming when it is not in a liquid, and gives it Water Breathing for a turtle
// shell on its head (Player.turtleHelmetTick).
func (p *Player) tickSwimming() {
	liquidPos := cube.PosFromVec3(p.Position())
	if p.movementChunkReady(liquidPos) {
		if _, ok := p.tx.Liquid(liquidPos); !ok {
			p.StopSwimming()
			if _, ok := p.Armour().Helmet().Item().(item.TurtleShell); ok {
				p.AddEffect(effect.New(effect.WaterBreathing, 1, time.Second*10).WithoutParticles())
			}
		}
	}
}

// tickFallFlying wears the elytra of a gliding player, a point of durability a second, and ends the glide when
// it is about to break (LivingEntity.updateFallFlying).
func (p *Player) tickFallFlying() {
	if _, ok := p.Armour().Chestplate().Item().(item.Elytra); ok && p.Gliding() {
		if p.glideTicks += 1; p.glideTicks%20 == 0 {
			d := p.damageItem(p.Armour().Chestplate(), 1)
			p.armour.SetChestplate(d)
			if d.Durability() < 2 {
				p.StopGliding()
			}
		}
	}
}

// tickFreezing freezes and thaws a player in or out of powder snow (LivingEntity.aiStep), telling the viewers
// when the freezing changes.
func (p *Player) tickFreezing() {
	if frozen := p.data.FrozenTicks; frozen > 0 || p.data.InPowderSnow || p.data.WasInPowderSnow {
		entity.TickFreezing(p.tx, p, p.data)
		if p.data.FrozenTicks != frozen {
			p.updateState()
		}
	}
}

// checkBelowWorld hurts a player below the bottom of the world (Entity.checkBelowWorld).
func (p *Player) checkBelowWorld() {
	if p.Position()[1] < float64(p.tx.Range()[0]) {
		p.Hurt(4, entity.VoidDamageSource{})
	}
}

// tickInsideWall hurts a player inside a solid block (LivingEntity.baseTick, isInWall).
func (p *Player) tickInsideWall() {
	if p.insideOfSolid() {
		p.Hurt(1, entity.SuffocationDamageSource{})
	}
}

// tickFire burns a player that is on fire (Entity.baseTick): it puts the fire out for a player that cannot take
// damage, or in rain, and hurts it each second.
func (p *Player) tickFire() {
	if p.OnFireDuration() > 0 {
		p.fireTicks -= 1
		if !p.GameMode().AllowsTakingDamage() || p.OnFireDuration() <= 0 || p.tx.RainingAt(cube.PosFromVec3(p.Position())) {
			p.Extinguish()
		}
		if p.OnFireDuration()%time.Second == 0 {
			p.Hurt(1, block.FireDamageSource{})
		}
	}
}

// tickBlockingDelay counts down the delay a player must wait to raise its shield again.
func (p *Player) tickBlockingDelay() {
	if p.blockingDelayTicks > 0 {
		p.blockingDelayTicks--
		if p.blockingDelayTicks <= 0 {
			p.SetBlockingDelay(0)
		}
	}
}

// tickChangedWorld starts the grace of the anti-cheat and tells the handler when the player changed world
// since the last tick.
func (p *Player) tickChangedWorld(tx *world.Tx) {
	if p.prevWorld != tx.World() && p.prevWorld != nil {
		p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.WorldChange }))
		p.resetTimer()
		p.Handler().HandleChangeWorld(p, p.prevWorld, tx.World())
	}
	p.prevWorld = tx.World()
}

// travel moves the player by the tick of its movement controller (LivingEntity.travel). A player spectating an
// entity stays with it, as ServerPlayer.tick moves it to its camera, and does not fall.
func (p *Player) travel() {
	if p.simulated() && !p.Immobile() && p.spectating == nil {
		m := p.mc.TickMovement(p, p.Position(), p.Velocity(), p.Rotation(), p.Rotation(), p.tx)
		m.Send()

		p.data.Vel = m.Velocity()
		p.Move(m.Position().Sub(p.Position()), 0, 0)
	} else {
		p.data.Vel = mgl64.Vec3{}
	}
}
