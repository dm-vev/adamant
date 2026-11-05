package player

import (
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player/anticheat"
	"github.com/df-mc/dragonfly/server/world"
)

// abilityState is the abilities of the player: its game mode, speeds, flight
// and visibility.
type abilityState struct {
	gameMode                    world.GameMode
	flying, invisible, immobile bool
	speed                       float64
	flightSpeed                 float64
	verticalFlightSpeed         float64
	// spectating is the entity the camera of the player follows, as Java's
	// ServerPlayer.camera when it is not the player itself.
	spectating *world.EntityHandle
}

// SetSpeed sets the speed of the player. The value passed is the blocks/tick speed that the player will then
// obtain.
func (p *Player) SetSpeed(speed float64) {
	p.speed = speed
	p.client().SendSpeed(speed)
}

// Speed returns the speed of the player, returning a value that indicates the blocks/tick speed. The default
// speed of a player is 0.1.
func (p *Player) Speed() float64 {
	return p.speed
}

// SetFlightSpeed sets the flight speed of the player. The value passed represents the base speed, which is
// multiplied by 10 to obtain the actual blocks/tick speed that the player will then obtain while flying.
func (p *Player) SetFlightSpeed(flightSpeed float64) {
	p.flightSpeed = flightSpeed
	p.client().SendAbilities(p)
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.GameMode }))
}

// FlightSpeed returns the flight speed of the player, with the value representing the base speed. The actual
// blocks/tick speed is this value multiplied by 10. The default flight speed of a player is 0.05, which
// corresponds to 0.5 blocks/tick.
func (p *Player) FlightSpeed() float64 {
	return p.flightSpeed
}

// SetVerticalFlightSpeed sets the flight speed of the player on the Y axis. The value passed represents the
// base speed, which is the blocks/tick speed that the player will obtain while flying.
func (p *Player) SetVerticalFlightSpeed(flightSpeed float64) {
	p.verticalFlightSpeed = flightSpeed
	p.client().SendAbilities(p)
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.GameMode }))
}

// VerticalFlightSpeed returns the flight speed of the player on the Y axis, with the value representing the
// base speed. The default vertical flight speed of a player is 1.0, which corresponds to 1 block/tick.
func (p *Player) VerticalFlightSpeed() float64 {
	return p.verticalFlightSpeed
}

// StartFlying makes the player start flying if they aren't already. It requires the player to be in a gamemode which
// allows flying. A client that asks to fly in a game mode without flight is shown its abilities again.
func (p *Player) StartFlying() {
	if !p.GameMode().AllowsFlying() {
		if p.clientInput() {
			p.log.Debug("client asked to fly in a game mode without flight")
			p.refuseFlightToggle()
			p.client().SendAbilities(p)
		}
		return
	}
	if p.Flying() {
		return
	}
	p.flying = true
	p.client().SendGameMode(p)
}

// Flying checks if the player is currently flying.
func (p *Player) Flying() bool {
	return p.flying
}

// StopFlying makes the player stop flying if it currently is. A client that asks to stop in a game mode without
// collision, which flies for good, is shown its abilities again.
func (p *Player) StopFlying() {
	if p.clientInput() && !p.GameMode().HasCollision() {
		p.client().SendAbilities(p)
		return
	}
	if !p.flying {
		return
	}
	p.flying = false
	p.client().SendGameMode(p)
}

// SetInvisible sets the player invisible, so that other players will not be able to see it.
func (p *Player) SetInvisible() {
	if p.Invisible() {
		return
	}
	p.invisible = true
	p.updateState()
}

// SetVisible sets the player visible again, so that other players can see it again. If the player was already
// visible, or if the player is in spectator mode, nothing happens.
func (p *Player) SetVisible() {
	if _, ok := p.Effect(effect.Invisibility); ok || !p.GameMode().Visible() || !p.invisible {
		return
	}
	p.invisible = false
	p.updateState()
}

// Invisible checks if the Player is currently invisible.
func (p *Player) Invisible() bool {
	return p.invisible
}

// SetImmobile prevents the player from moving around, but still allows them to look around.
func (p *Player) SetImmobile() {
	if p.Immobile() {
		return
	}
	p.immobile = true
	p.updateState()
}

// SetMobile allows the player to freely move around again after being immobile.
func (p *Player) SetMobile() {
	if !p.Immobile() {
		return
	}
	p.immobile = false
	p.updateState()
}

// Immobile checks if the Player is currently immobile.
func (p *Player) Immobile() bool {
	return p.immobile
}

// SetGameMode sets the game mode of a player. The game mode specifies the way that the player can interact
// with the world that it is in.
func (p *Player) SetGameMode(mode world.GameMode) {
	previous := p.GameMode()
	p.gameMode = mode
	if !mode.AllowsInteraction() {
		p.cancelItemUse()
	}

	if !mode.AllowsFlying() {
		p.StopFlying()
	}
	if !mode.Visible() {
		p.SetInvisible()
	} else if !previous.Visible() {
		p.SetVisible()
	}

	p.client().SendGameMode(p)
	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityGameMode(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
	if mode.AllowsTakingDamage() {
		p.client().SendHealth(p.Health(), p.MaxHealth(), p.absorptionHealth)
	}
	if mode != world.GameModeSpectator {
		p.Spectate(nil)
	}
	p.startGrace(p.graceOf(func(g anticheat.Grace) int { return g.GameMode }))
}

// Spectate makes the camera of the player follow an entity, ported from
// ServerPlayer.setCamera: the player moves to the entity and follows it until
// it sneaks, leaves spectator mode or the entity is gone. A nil entity
// returns the camera to the player.
func (p *Player) Spectate(e world.Entity) {
	var h *world.EntityHandle
	if e != nil {
		h = e.H()
	}
	if h == p.spectating {
		return
	}
	p.spectating = h
	if e != nil {
		p.Teleport(e.Position())
	}
	p.client().ViewSpectatedEntity(e)
}

// Spectating returns the entity the camera of the player follows, if any.
func (p *Player) Spectating() (world.Entity, bool) {
	if p.spectating == nil {
		return nil, false
	}
	return p.spectating.Entity(p.tx)
}

// tickSpectating ports the camera part of ServerPlayer.tick: the player keeps
// up with the entity it spectates and stops when it sneaks or the entity is
// gone.
func (p *Player) tickSpectating() {
	if p.spectating == nil {
		return
	}
	e, ok := p.spectating.Entity(p.tx)
	if mortal, isMortal := e.(interface{ Dead() bool }); !ok || isMortal && mortal.Dead() || p.Sneaking() {
		p.Spectate(nil)
		return
	}
	if e.Position().Sub(p.Position()).LenSqr() > 1e-4 {
		p.Teleport(e.Position())
	}
}

// GameMode returns the current game mode assigned to the player. If not changed, the game mode returned will
// be the same as that of the world that the player spawns in.
// The game mode may be changed using Player.SetGameMode().
func (p *Player) GameMode() world.GameMode {
	return p.gameMode
}
