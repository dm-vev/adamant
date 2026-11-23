package player

import (
	"net"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/menu"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/scoreboard"
	"github.com/df-mc/dragonfly/server/player/title"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Movement returns a zero-allocation facet exposing movement, position, and
// locomotion state operations for p.
func (p *Player) Movement() MovementFacet { return MovementFacet{p: p} }

// Combat returns a zero-allocation facet exposing health, damage, effects,
// armour, and death/respawn operations for p.
func (p *Player) Combat() CombatFacet { return CombatFacet{p: p} }

// UI returns a zero-allocation facet exposing chat, HUD popups, titles,
// forms, dialogues, boss bars, scoreboards, and menus for p.
func (p *Player) UI() UIFacet { return UIFacet{p: p} }

// Net returns a zero-allocation facet exposing network connection, latency,
// transfer, profile, and view-distance operations for p.
func (p *Player) Net() NetFacet { return NetFacet{p: p} }

// MovementFacet groups movement and locomotion operations on a *Player.
type MovementFacet struct {
	p *Player
}

// Position returns the current position of the player.
func (f MovementFacet) Position() mgl64.Vec3 { return f.p.Position() }

// Velocity returns the current velocity of the player.
func (f MovementFacet) Velocity() mgl64.Vec3 { return f.p.Velocity() }

// SetVelocity updates the player's velocity.
func (f MovementFacet) SetVelocity(vel mgl64.Vec3) { f.p.SetVelocity(vel) }

// Rotation returns the yaw and pitch of the player.
func (f MovementFacet) Rotation() cube.Rotation { return f.p.Rotation() }

// Move moves and rotates the player by the deltas passed.
func (f MovementFacet) Move(deltaPos mgl64.Vec3, deltaYaw, deltaPitch float64) {
	f.p.Move(deltaPos, deltaYaw, deltaPitch)
}

// Teleport teleports the player to pos.
func (f MovementFacet) Teleport(pos mgl64.Vec3) { f.p.Teleport(pos) }

// Sprinting reports whether the player is currently sprinting.
func (f MovementFacet) Sprinting() bool { return f.p.Sprinting() }

// StartSprinting makes the player start sprinting.
func (f MovementFacet) StartSprinting() { f.p.StartSprinting() }

// StopSprinting makes the player stop sprinting.
func (f MovementFacet) StopSprinting() { f.p.StopSprinting() }

// Sprint toggles the player's sprinting state.
func (f MovementFacet) Sprint(sprinting bool) {
	if sprinting {
		f.p.StartSprinting()
		return
	}
	f.p.StopSprinting()
}

// Sneaking reports whether the player is currently sneaking.
func (f MovementFacet) Sneaking() bool { return f.p.Sneaking() }

// StartSneaking makes the player start sneaking.
func (f MovementFacet) StartSneaking() { f.p.StartSneaking() }

// StopSneaking makes the player stop sneaking.
func (f MovementFacet) StopSneaking() { f.p.StopSneaking() }

// Sneak toggles the player's sneaking state.
func (f MovementFacet) Sneak(sneaking bool) {
	if sneaking {
		f.p.StartSneaking()
		return
	}
	f.p.StopSneaking()
}

// Swimming reports whether the player is currently swimming.
func (f MovementFacet) Swimming() bool { return f.p.Swimming() }

// StartSwimming makes the player start swimming.
func (f MovementFacet) StartSwimming() { f.p.StartSwimming() }

// StopSwimming makes the player stop swimming.
func (f MovementFacet) StopSwimming() { f.p.StopSwimming() }

// Swim toggles the player's swimming state.
func (f MovementFacet) Swim(swimming bool) {
	if swimming {
		f.p.StartSwimming()
		return
	}
	f.p.StopSwimming()
}

// Gliding reports whether the player is currently gliding.
func (f MovementFacet) Gliding() bool { return f.p.Gliding() }

// StartGliding makes the player start gliding.
func (f MovementFacet) StartGliding() { f.p.StartGliding() }

// StopGliding makes the player stop gliding.
func (f MovementFacet) StopGliding() { f.p.StopGliding() }

// Glide toggles the player's gliding state.
func (f MovementFacet) Glide(gliding bool) {
	if gliding {
		f.p.StartGliding()
		return
	}
	f.p.StopGliding()
}

// Flying reports whether the player is currently flying.
func (f MovementFacet) Flying() bool { return f.p.Flying() }

// StartFlying makes the player start flying.
func (f MovementFacet) StartFlying() { f.p.StartFlying() }

// StopFlying makes the player stop flying.
func (f MovementFacet) StopFlying() { f.p.StopFlying() }

// Fly toggles the player's flying state.
func (f MovementFacet) Fly(flying bool) {
	if flying {
		f.p.StartFlying()
		return
	}
	f.p.StopFlying()
}

// OnGround reports whether the player is on the ground.
func (f MovementFacet) OnGround() bool { return f.p.OnGround() }

// Speed returns the movement speed of the player.
func (f MovementFacet) Speed() float64 { return f.p.Speed() }

// SetSpeed updates the movement speed of the player.
func (f MovementFacet) SetSpeed(speed float64) { f.p.SetSpeed(speed) }

// CombatFacet groups health, damage, effects, armour, and death operations on a *Player.
type CombatFacet struct {
	p *Player
}

// Health returns the current health of the player.
func (f CombatFacet) Health() float64 { return f.p.Health() }

// MaxHealth returns the maximum health of the player.
func (f CombatFacet) MaxHealth() float64 { return f.p.MaxHealth() }

// SetMaxHealth sets the maximum health of the player.
func (f CombatFacet) SetMaxHealth(health float64) { f.p.SetMaxHealth(health) }

// Hurt deals dmg damage to the player from src.
func (f CombatFacet) Hurt(dmg float64, src world.DamageSource) (float64, bool) {
	return f.p.Hurt(dmg, src)
}

// Heal heals the player for health from src.
func (f CombatFacet) Heal(health float64, src world.HealingSource) float64 {
	return f.p.Heal(health, src)
}

// AttackEntity attacks entity e with the player's held item.
func (f CombatFacet) AttackEntity(e world.Entity) bool { return f.p.AttackEntity(e) }

// Effects returns all active status effects on the player.
func (f CombatFacet) Effects() []effect.Effect { return f.p.Effects() }

// Effect returns the active effect of type t if present.
func (f CombatFacet) Effect(t effect.Type) (effect.Effect, bool) { return f.p.Effect(t) }

// AddEffect applies status effect e to the player.
func (f CombatFacet) AddEffect(e effect.Effect) { f.p.AddEffect(e) }

// RemoveEffect removes status effect t from the player.
func (f CombatFacet) RemoveEffect(t effect.Type) { f.p.RemoveEffect(t) }

// Armour returns the player's equipped armour inventory.
func (f CombatFacet) Armour() *inventory.Armour { return f.p.Armour() }

// Dead reports whether the player is dead.
func (f CombatFacet) Dead() bool { return f.p.Dead() }

// Kill immediately kills the player with damage source src (defaulting to
// entity.VoidDamageSource{} when nil).
func (f CombatFacet) Kill(src world.DamageSource) {
	if f.p.Dead() {
		return
	}
	if src == nil {
		src = entity.VoidDamageSource{}
	}
	f.p.kill(src)
}

// Respawn respawns the dead player and returns its entity handle.
func (f CombatFacet) Respawn() *world.EntityHandle { return f.p.Respawn() }

// UIFacet groups messaging, overlays, forms, dialogues, and menus on a *Player.
type UIFacet struct {
	p *Player
}

// Message sends a formatted message to the player.
func (f UIFacet) Message(a ...any) { f.p.Message(a...) }

// Messagef sends a fmt.Sprintf formatted message to the player.
func (f UIFacet) Messagef(format string, a ...any) { f.p.Messagef(format, a...) }

// Messaget sends a translated message to the player.
func (f UIFacet) Messaget(t chat.Translation, a ...any) { f.p.Messaget(t, a...) }

// Tip sends a tip to the middle of the player's screen.
func (f UIFacet) Tip(a ...any) { f.p.SendTip(a...) }

// Popup sends a popup above the player's hotbar.
func (f UIFacet) Popup(a ...any) { f.p.SendPopup(a...) }

// JukeboxPopup sends a jukebox popup above the player's hotbar.
func (f UIFacet) JukeboxPopup(a ...any) { f.p.SendJukeboxPopup(a...) }

// SendToast displays a toast notification to the player.
func (f UIFacet) SendToast(title, message any) { f.p.SendToast(title, message) }

// SendTitle displays a title to the player.
func (f UIFacet) SendTitle(t title.Title) { f.p.SendTitle(t) }

// SendForm displays a form to the player.
func (f UIFacet) SendForm(fm form.Form) { f.p.SendForm(fm) }

// CloseForm closes any form currently open for the player.
func (f UIFacet) CloseForm() { f.p.CloseForm() }

// SendDialogue displays an NPC dialogue to the player.
func (f UIFacet) SendDialogue(d dialogue.Dialogue, e world.Entity) { f.p.SendDialogue(d, e) }

// CloseDialogue closes any dialogue currently open for the player.
func (f UIFacet) CloseDialogue() { f.p.CloseDialogue() }

// SendBossBar displays a boss bar to the player.
func (f UIFacet) SendBossBar(bar bossbar.BossBar) { f.p.SendBossBar(bar) }

// RemoveBossBar removes the default boss bar from the player.
func (f UIFacet) RemoveBossBar() { f.p.RemoveBossBar() }

// RemoveBossBarID removes the boss bar with the given ID from the player.
func (f UIFacet) RemoveBossBarID(id string) { f.p.RemoveBossBarID(id) }

// SendScoreboard displays a scoreboard in the player's sidebar.
func (f UIFacet) SendScoreboard(sb *scoreboard.Scoreboard) { f.p.SendScoreboard(sb) }

// RemoveScoreboard removes the player's active scoreboard.
func (f UIFacet) RemoveScoreboard() { f.p.RemoveScoreboard() }

// OpenMenu opens the block container or screen menu at pos in tx (or the
// player's current transaction when tx is nil).
func (f UIFacet) OpenMenu(pos cube.Pos, tx *world.Tx) {
	if tx == nil {
		tx = f.p.tx
	}
	if tx == nil || tx.Closed() {
		return
	}
	f.p.OpenBlockContainer(pos, tx)
}

// CloseMenu closes any menu currently open for the player.
func (f UIFacet) CloseMenu(clientRequested bool) { f.p.CloseMenu(clientRequested) }

// Menu returns the menu currently open for the player, or nil if none.
func (f UIFacet) Menu() *menu.Menu { return f.p.Menu() }

// NetFacet groups connection and session operations on a *Player.
type NetFacet struct {
	p *Player
}

// Addr returns the remote network address of the player, or nil if simulated.
func (f NetFacet) Addr() net.Addr { return f.p.Addr() }

// Latency returns half the round-trip time of the player's connection.
func (f NetFacet) Latency() time.Duration { return f.p.Latency() }

// NetStats returns connection statistics for the player, or false if simulated.
func (f NetFacet) NetStats() (client.NetStats, bool) { return f.p.NetStats() }

// Transfer transfers the player to another server address.
func (f NetFacet) Transfer(address string) error { return f.p.Transfer(address) }

// Disconnect closes the player's connection with a formatted disconnect message.
func (f NetFacet) Disconnect(msg ...any) { f.p.Disconnect(msg...) }

// Profile returns the client device profile reported at login.
func (f NetFacet) Profile() client.Profile { return f.p.client().Profile() }

// ChunkRadius returns the chunk view radius of the player's client.
func (f NetFacet) ChunkRadius() int32 { return f.p.client().ChunkRadius() }
