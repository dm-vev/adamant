package player

import (
	"log/slog"
	"net"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// identityState is who the player is: the identifiers it is kept by, its locale
// and skin, the client that plays it, the handler and the logger.
type identityState struct {
	// dataUUID is the UUID the record and the spawn points of the player
	// are kept by.
	dataUUID uuid.UUID
	xuid     string
	locale   language.Tag
	skin     skin.Skin
	c        client.Client // The client of the player, nil if it has none.
	h        Handler
	// log is the logger of the player, never nil.
	log *slog.Logger
	// permissions is the Permissions of the Config of the player.
	permissions func(p *Player) client.Permissions
}

// Name returns the username of the player. If the player is controlled by a client, it is the username of
// the client. (Typically the XBOX Live name)
func (p *Player) Name() string {
	// TODO: This isn't correct, this will change if the nametag changes.
	return p.data.Name
}

// UUID returns the UUID of the player, as its client sent it. For a player that signed in with Xbox Live, it
// follows from the account and will, unlike the name of the player, never change. A client that joined without
// signing in chose it, and may send another at its next login, or the UUID of another player.
func (p *Player) UUID() uuid.UUID {
	return p.handle.UUID()
}

// XUID returns the Xbox Live user ID the login of the player named, in decimal: a number that fits a uint64. For
// a player that signed in with Xbox Live, it is the ID of its account, which does not change in the lifetime of
// the account. A client that joined without signing in, on a server with Xbox Live sign-in off, claimed it: it
// may be empty, or the XUID of anyone, and must not be trusted as the account of the player. The server keys such
// players by name (see identity.Mode). The XUID is empty for a player no client plays.
func (p *Player) XUID() string {
	return p.xuid
}

// DeviceID returns the device ID of the player. If the Player is not connected to a network session, an empty string is
// returned. Otherwise, the device ID its client told when it joined is returned.
func (p *Player) DeviceID() string {
	return p.client().Profile().DeviceID
}

// DeviceModel returns the device model of the player. If the Player is not connected to a network session, an empty
// string is returned. Otherwise, the device model its client told when it joined is returned.
func (p *Player) DeviceModel() string {
	return p.client().Profile().DeviceModel
}

// SelfSignedID returns the self-signed ID of the player. If the Player is not connected to a network session, an empty
// string is returned. Otherwise, the self-signed ID its client told when it joined is returned.
func (p *Player) SelfSignedID() string {
	return p.client().Profile().SelfSignedID
}

// Addr returns the net.Addr of the Player. If the Player is not connected to a network session, nil is returned.
func (p *Player) Addr() net.Addr {
	return p.client().Addr()
}

// Skin returns the skin that a player is currently using. This skin will be visible to other players
// that the player is shown to.
// If the player was not connected to a network session, a default skin will be set.
func (p *Player) Skin() skin.Skin {
	return p.skin
}

// SetSkin changes the skin of the player. This skin will be visible to other players that the player
// is shown to.
func (p *Player) SetSkin(skin skin.Skin) {
	ctx := NewEventContext(p.tx, p)
	if p.Handler().HandleSkinChange(ctx, &skin); ctx.Cancelled() {
		p.client().ViewSkin(p)
		return
	}
	p.skin = skin
	if p.hasClient() {
		// Every client lists the player, and sees its skin from there.
		p.c.ShowSkin(skin)
		return
	}
	viewers, release := p.viewers()
	// viewer() returns a pooled slice sourced from the world; recycling it after the broadcast keeps rapid skin
	// changes (e.g. via plugins) from allocating new memory every time.
	for _, v := range viewers {
		v.ViewSkin(p)
	}
	releaseBorrowedViewers(p.tx, viewers, release)
}

// Locale returns the language and locale of the Player, as selected in the Player's settings.
func (p *Player) Locale() language.Tag {
	return p.locale
}

// Handle changes the current Handler of the player. As a result, events called by the player will call
// handlers of the Handler passed.
// Handle sets the player's Handler to NopHandler if nil is passed.
func (p *Player) Handle(h Handler) {
	if h == nil {
		h = NopHandler{}
	}
	p.h = h
}

// PermissionLevel returns the level of the player, as the server says: the
// level of its entry of ops.json for an operator, the level of players who
// are not operators otherwise. It is the level of the commands the player
// may run.
func (p *Player) PermissionLevel() cmd.Level { return cmd.PlayerLevel(p.UUID()) }

// RefreshPermissions updates the client's operator abilities and available
// commands after the server's permission lookup changes.
func (p *Player) RefreshPermissions() {
	p.client().SendPermissions(p)
}

// Latency returns a rolling average of latency between the sending and the receiving end of the connection of
// the player.
// The latency returned is updated continuously and is half the round trip time (RTT).
// If the Player does not have a session associated with it, Latency returns 0.
func (p *Player) Latency() time.Duration {
	return p.client().Latency()
}

// NetStats returns the state of the connection of the player, false for a
// player without one.
func (p *Player) NetStats() (client.NetStats, bool) {
	if !p.hasClient() {
		return client.NetStats{}, false
	}
	return p.c.NetStats(), true
}

// Data returns the player data that needs to be saved. This is used when the player
// gets disconnected and the player provider needs to save the data.
func (p *Player) Data() Config {
	hunger := p.hunger.snapshot()
	raidOmenPos := cube.Pos{}
	hasRaidOmenPos := p.raidOmenPosition != nil
	if hasRaidOmenPos {
		raidOmenPos = *p.raidOmenPosition
	}
	return Config{
		Client:              p.c,
		Log:                 p.log,
		Left:                p.leftHook,
		QuitMessage:         p.quitMessage,
		AntiCheat:           p.acConf,
		AntiCheatExempt:     p.acExempt,
		Permissions:         p.permissions,
		Skin:                p.skin,
		XUID:                p.xuid,
		UUID:                p.UUID(),
		DataUUID:            p.dataUUID,
		Name:                p.nameTag,
		Locale:              p.locale,
		GameMode:            p.gameMode,
		Position:            p.Position(),
		Rotation:            p.Rotation(),
		Velocity:            p.Velocity(),
		Health:              p.Health(),
		MaxHealth:           p.MaxHealth(),
		FoodTick:            hunger.foodTick,
		Food:                hunger.foodLevel,
		Exhaustion:          hunger.exhaustionLevel,
		Saturation:          hunger.saturationLevel,
		AirSupply:           p.airSupplyTicks,
		MaxAirSupply:        p.maxAirSupplyTicks,
		TimeSinceRest:       p.timeSinceRest,
		EnchantmentSeed:     p.enchantSeed,
		Experience:          p.experience.Experience(),
		HeldSlot:            int(atomic.LoadUint32(p.heldSlot)),
		Inventory:           p.inv,
		OffHand:             p.offHand,
		Armour:              p.armour,
		EnderChestInventory: p.enderChest,
		FireTicks:           p.fireTicks,
		FallDistance:        p.fallDistance,
		RespawnDimension:    p.respawnDimension,
		RespawnPosition:     p.respawnPosition,
		RespawnRotation:     p.respawnRotation,
		HasRespawnPoint:     p.hasRespawnPoint,
		Effects:             p.Effects(),
		UnlockedRecipes:     p.unlockedRecipeIDs(),
		RaidOmenPosition:    raidOmenPos,
		ShoulderLeft:        p.shoulderLeft,
		ShoulderRight:       p.shoulderRight,
		JavaState:           encodePlayerJavaState(p),
		HasRaidOmenPosition: hasRaidOmenPos,
	}
}

// Snapshot returns the data of p as Data does, detached from p: its
// inventory, off hand, armour and ender chest are copies, so that p changing
// after Snapshot returned changes nothing in it, and a provider may encode
// it on another goroutine after the transaction ended. The rest of what Data
// returns is copied already or never changed in place: the shoulder entities
// are replaced, never changed, and the client, logger, Left hook and skin are
// not saved.
func (p *Player) Snapshot() Config {
	conf := p.Data()
	conf.Inventory = conf.Inventory.Clone(nil)
	conf.OffHand = conf.OffHand.Clone(nil)
	conf.Armour = conf.Armour.Clone()
	conf.EnderChestInventory = conf.EnderChestInventory.Clone(nil)
	return conf
}
