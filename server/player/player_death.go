package player

import (
	"errors"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SetRespawnPosition updates the player's persistent respawn point and the client-visible destination.
func (p *Player) SetRespawnPosition(pos cube.Pos, dim world.Dimension) {
	p.respawnDimension, p.respawnPosition = dim, pos
	p.respawnRotation, p.hasRespawnPoint = p.Rotation(), true
	if p.tx != nil {
		p.tx.World().SetPlayerSpawn(p.dataUUID, pos)
	}
	p.client().SetPlayerSpawn(pos, dim)
}

// Dead checks if the player is considered dead. True is returned if the health of the player is equal to or
// lower than 0.
func (p *Player) Dead() bool {
	return p.Health() <= mgl64.Epsilon
}

// DeathPosition returns the last position the player was at when they died. If the player has never died, the third
// return value will be false.
func (p *Player) DeathPosition() (mgl64.Vec3, world.Dimension, bool) {
	if p.deathPos == nil {
		return mgl64.Vec3{}, nil, false
	}
	return *p.deathPos, p.deathDimension, true
}

// kill kills the player, clearing its inventories and resetting it to its base state.
func (p *Player) kill(src world.DamageSource) {
	// The container of a player that dies closes, as ServerPlayer.die does.
	p.closeMenu(p.tx, false)
	viewers, release := p.viewers()
	for _, viewer := range viewers {
		viewer.ViewEntityAction(p, entity.DeathAction{})
	}
	releaseBorrowedViewers(p.tx, viewers, release)

	p.addHealth(-p.MaxHealth())

	p.StopFishing(p.tx, false)

	p.awardDeathScores(src)
	p.broadcastDeathMessage(src)
	entity.ForgiveDeadPlayer(p.tx, p)
	p.vanishEnderPearls()
	keepInv := p.tx.World().GameRuleBool("keep_inventory")
	handledKeepInv := false
	p.Handler().HandleDeath(p, src, &handledKeepInv)
	keepInv = keepInv || handledKeepInv
	p.StopSneaking()
	p.StopSprinting()

	pos := p.Position()
	// Player.getBaseExperienceReward: nothing with keepInventory or in
	// spectator mode. A sculk catalyst handling entity_die absorbs it.
	xp := &world.DeathExperience{Eligible: true}
	if !keepInv && p.GameMode() != world.GameModeSpectator {
		xp.Amount = min(p.experience.Level()*7, 100)
	}
	p.tx.EmitGameEvent(world.GameEventEntityDie, pos, world.GameEventContext{Source: p, Experience: xp})
	if !keepInv {
		if !xp.Consumed {
			for _, orb := range entity.NewExperienceOrbs(p.tx, pos, xp.Amount) {
				p.tx.AddEntity(orb)
			}
		}
		p.experience.Reset()
		p.client().SendExperience(p.ExperienceLevel(), p.ExperienceProgress())
		p.dropItems()
	}
	entity.DropWitherRose(p.tx, pos, src)
	p.deathEffects = p.Effects()
	for _, e := range p.deathEffects {
		p.RemoveEffect(e.Type())
	}

	p.deathPos, p.deathDimension = &pos, p.tx.World().Dimension()

	// Wait a little before removing the entity. The client displays a death
	// animation while the player is dying.
	DoAfter(p.handle, time.Millisecond*1100, func(_ *world.Tx, p *Player) {
		finishDying(p)
	})
}

// finishDying completes the death of a player, removing it from the world.
func finishDying(p *Player) {
	if !p.hasClient() {
		_ = p.Close()
		return
	}
	if p.Dead() {
		p.SetInvisible()
		// We have an actual client connected to this player: We change its
		// position server side so that in the future, the client won't respawn
		// on the death location when disconnecting. The client should not see
		// the movement itself yet, though.
		pos, _, _, _, _ := p.spawnLocation()

		p.setPosition(pos.Vec3())
	}
}

// dropItems drops all items of the Player on the ground in random directions.
func (p *Player) dropItems() {
	pos := p.Position()

	p.MoveItemsToInventory()
	for _, it := range append(p.inv.Clear(), append(p.armour.Clear(), p.offHand.Clear()...)...) {
		if _, ok := it.Enchantment(enchantment.CurseOfVanishing); ok {
			continue
		}
		opts := world.EntitySpawnOpts{Position: pos, Velocity: mgl64.Vec3{rand.Float64()*0.2 - 0.1, 0.2, rand.Float64()*0.2 - 0.1}}
		p.tx.AddEntity(entity.NewItem(opts, it))
	}
}

// Respawn spawns the player after it dies, so that its health is replenished,
// and it is spawned in the world again. Nothing will happen if the player does
// not have a session connected to it.
// Calling Respawn may lead to the player being removed from its world and being
// added to a new world. This means that p cannot be assumed to be valid after
// a call to Respawn.
func (p *Player) Respawn() *world.EntityHandle {
	p.respawn(nil)
	return p.handle
}

// respawn heals the player and moves it to its spawn position. f, if
// non-nil, runs with the player once it is back in a world — normally the
// respawn destination, otherwise the world it died in — so a quit callback
// from close always completes the player's teardown.
//
// The player is in no world between leaving the one it died in and entering
// the next, even if they are the same: while a checkpoint holds transfers
// out of its world, respawn tries again a tick later. A player quitting
// respawns at once anyway: it is saved as it quits, not by the checkpoint.
func (p *Player) respawn(f func(p *Player)) {
	position, w, spawnObstructed, previousDimension, worldSpawn := p.spawnLocation()
	transfer, ok := world.BeginTransfer(p.tx)
	if !ok && f == nil {
		if p.Dead() && p.hasClient() {
			p.H().DoAfter(transferRetryDelay, func(_ *world.Tx, e world.Entity) {
				if p, ok := e.(*Player); ok {
					p.respawn(nil)
				}
			})
		}
		return
	}
	if spawnObstructed {
		switch previousDimension {
		case world.Nether:
			p.Messaget(chat.MessageRespawnAnchorNotValid)
		case world.Overworld:
			p.Messaget(chat.MessageBedNotValid)
		}
	}

	pos := position.Vec3Middle()

	if !p.Dead() || !p.hasClient() {
		transfer.Cancel()
		return
	}

	p.addHealth(p.MaxHealth())
	p.hunger.Reset()
	p.sendFood()
	p.Extinguish()
	p.ResetFallDistance()

	p.Handler().HandleRespawn(p, &pos, &w)

	// respawn only runs with a client.
	c := p.c
	src := p.tx.World()
	if p.deathPos != nil {
		entity.TriggerDeathEffects(p.tx, p, *p.deathPos, p.deathEffects)
	}
	p.deathEffects = nil
	handle := transfer.Remove(p.tx, p)
	// restore re-adds the player through tx and finishes with f or the normal
	// quit path; the fallback branches below share it.
	restore := func(tx *world.Tx) {
		np := tx.AddEntity(handle).(*Player)
		if f != nil {
			f(np)
			return
		}
		np.quit(text.Literal("respawn failed"))
	}
	task := w.Do(func(tx *world.Tx) {
		np := tx.AddEntity(handle).(*Player)
		if bl, ok := tx.Block(position).(block.RespawnBlock); ok {
			bl.RespawnOn(position, p, tx)
		}
		// Without a respawn block, PlayerSpawnFinder picks a safe position
		// within respawn_radius unless a handler chose another position.
		if worldSpawn && pos == position.Vec3Middle() {
			pos = tx.PlayerSpawnPosition(position)
		}
		np.Teleport(pos)
		np.client().SendRespawn(pos, p)
		np.SetVisible()
		np.startGrace(np.graceOf(func(g anticheat.Grace) int { return g.Respawn }))
		if f != nil {
			f(np)
		}
	})
	if errors.Is(task.Err(), world.ErrWorldClosed) {
		// The destination refused synchronously: re-add through the still-open
		// source context. This also keeps synchronous worlds fully inline.
		restore(p.tx)
		return
	}
	task.OnDone(func(err error) {
		// Only ErrWorldClosed means the entity was never re-added. A callback
		// panic is left alone: the entity is live.
		if !errors.Is(err, world.ErrWorldClosed) {
			return
		}
		// Fall back to the source world so the normal quit path still runs.
		src.Do(restore).OnDone(func(err error) {
			if err == nil || errors.Is(err, world.ErrTaskPanicked) {
				return
			}
			// The source world is gone too; the handle is orphaned. Close
			// it, free the connection and leave without a world, so that the
			// Left hook still runs.
			_ = handle.Close()
			c.Disconnect(text.Literal("respawn failed"))
			c.CloseConnection()
			p.leave(nil)
		})
	})
}

// spawnLocation returns the block the player respawns at. worldSpawn is true
// when no bed or respawn block applies and the world spawn is used instead.
func (p *Player) spawnLocation() (playerSpawn cube.Pos, w *world.World, spawnBlockBroken bool, previousDimension world.Dimension, worldSpawn bool) {
	tx := p.tx
	w = tx.World()
	previousDimension = w.Dimension()
	playerSpawn = w.PlayerSpawn(p.dataUUID)
	if b, ok := tx.Block(playerSpawn).(block.Bed); ok && b.CanRespawnOn() {
		pos, ok := b.SafeSpawn(playerSpawn, tx)
		if ok {
			return pos, w, false, previousDimension, false
		}
	}
	if b, ok := tx.Block(playerSpawn).(block.RespawnBlock); ok && b.CanRespawnOn() {
		return playerSpawn, w, false, previousDimension, false
	}

	w = w.DefaultWorld()
	spawn := w.Spawn()
	return spawn, w, playerSpawn != spawn, previousDimension, true
}
