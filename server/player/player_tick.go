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
	if p.sleeping {
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
	if p.sleeping {
		p.timeSinceRest = 0
		// Player.tick counts the ticks slept up to 100.
		p.sleepTimer = min(p.sleepTimer+1, 100)
	} else if p.timeSinceRest < int32(^uint32(0)>>1) {
		p.timeSinceRest++
	}
	blockName := ""
	if b, loaded := p.tx.BlockLoaded(cube.PosFromVec3(p.Position())); loaded {
		blockName, _ = b.EncodeBlock()
	}
	if p.insideOfWater() || p.Flying() || p.sleeping || p.fallDistance > 0.5 || blockName == "minecraft:powder_snow" {
		p.ReleaseShoulderParrots()
	}
	p.tickSpectating()
	p.tickCarriedMaps(current)
	p.tickScoreboard(tx, current)
	p.tickMapFrames(current)
	liquidPos := cube.PosFromVec3(p.Position())
	if p.movementChunkReady(liquidPos) {
		if _, ok := p.tx.Liquid(liquidPos); !ok {
			p.StopSwimming()
			if _, ok := p.Armour().Helmet().Item().(item.TurtleShell); ok {
				p.AddEffect(effect.New(effect.WaterBreathing, 1, time.Second*10).WithoutParticles())
			}
		}
	}

	if _, ok := p.Armour().Chestplate().Item().(item.Elytra); ok && p.Gliding() {
		if p.glideTicks += 1; p.glideTicks%20 == 0 {
			d := p.damageItem(p.Armour().Chestplate(), 1)
			p.armour.SetChestplate(d)
			if d.Durability() < 2 {
				p.StopGliding()
			}
		}
	}

	p.checkBlockCollisions(p.data.Vel)
	if frozen := p.data.FrozenTicks; frozen > 0 || p.data.InPowderSnow || p.data.WasInPowderSnow {
		entity.TickFreezing(p.tx, p, p.data)
		if p.data.FrozenTicks != frozen {
			p.updateState()
		}
	}
	p.onGround = p.checkOnGround(mgl64.Vec3{})
	p.tickFrostWalker()
	p.checkEntitySteppers()

	p.effects.Tick(p, p.tx)

	p.tickFood()
	p.tickAirSupply()

	if p.Position()[1] < float64(p.tx.Range()[0]) {
		p.Hurt(4, entity.VoidDamageSource{})
	}
	if p.insideOfSolid() {
		p.Hurt(1, entity.SuffocationDamageSource{})
	}

	if p.OnFireDuration() > 0 {
		p.fireTicks -= 1
		if !p.GameMode().AllowsTakingDamage() || p.OnFireDuration() <= 0 || p.tx.RainingAt(cube.PosFromVec3(p.Position())) {
			p.Extinguish()
		}
		if p.OnFireDuration()%time.Second == 0 {
			p.Hurt(1, block.FireDamageSource{})
		}
	}

	if p.blockingDelayTicks > 0 {
		p.blockingDelayTicks--
		if p.blockingDelayTicks <= 0 {
			p.SetBlockingDelay(0)
		}
	}

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

	if p.prevWorld != tx.World() && p.prevWorld != nil {
		p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.WorldChange }))
		p.resetTimer()
		p.Handler().HandleChangeWorld(p, p.prevWorld, tx.World())
	}
	p.prevWorld = tx.World()

	// A player spectating an entity stays with it, as ServerPlayer.tick moves
	// it to its camera, and does not fall.
	if p.simulated() && !p.Immobile() && p.spectating == nil {
		m := p.mc.TickMovement(p, p.Position(), p.Velocity(), p.Rotation(), p.Rotation(), p.tx)
		m.Send()

		p.data.Vel = m.Velocity()
		p.Move(m.Position().Sub(p.Position()), 0, 0)
	} else {
		p.data.Vel = mgl64.Vec3{}
	}

	p.portalTravel.StopPortalContact()
}
