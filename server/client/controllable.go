package client

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/debug"
	"github.com/df-mc/dragonfly/server/player/dialogue"
	"github.com/df-mc/dragonfly/server/player/form"
	"github.com/df-mc/dragonfly/server/player/hud"
	"github.com/df-mc/dragonfly/server/player/input"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"golang.org/x/text/language"
)

// Controllable is a player as its client drives it: what the client of a
// player, such as a Bedrock Edition session, may ask of the player as it
// handles what the client sends. *player.Player implements it. Methods are
// added as clients need them.
//
// The game checks what a client asks: ClientMove and DestroyBlock may
// refuse, and the other actions refuse as they refuse a plugin out of
// reach.
//
// The role interfaces embedded in it, Mover, Breaker, ItemUser, Editor,
// MenuUser, Speaker and Respawner, group the methods of one concern. What
// stays declared in Controllable has no such concern in common: the identity,
// the game mode, the vitals, the inventories and the diagnostics of the
// player, and Leave.
type Controllable interface {
	Name() string
	world.Entity
	item.User
	dialogue.Submitter
	form.Submitter
	cmd.Source
	chat.Subscriber
	hud.Renderer
	debug.Renderer
	input.Restricter

	Mover
	Breaker
	ItemUser
	Editor
	MenuUser
	Speaker
	Respawner

	Locale() language.Tag

	GameMode() world.GameMode
	SetGameMode(mode world.GameMode)
	Effects() []effect.Effect

	Health() float64
	MaxHealth() float64
	Absorption() float64
	Food() int
	Exhaust(points float64)

	ExperienceLevel() int
	ExperienceProgress() float64

	EnderChestInventory() *inventory.Inventory
	Inventory() *inventory.Inventory

	// UUID returns the UUID of the controllable. It must be unique for all controllable entities present in
	// the server.
	UUID() uuid.UUID
	// XUID returns the XBOX Live User ID of the controllable. Every controllable must have one of these if
	// they are authenticated via XBOX Live, as they must be connected to an XBOX Live account.
	XUID() string
	// Skin returns the skin of the controllable. Each controllable must have a skin, as it defines how the
	// entity looks in the world.
	Skin() skin.Skin
	SetSkin(skin.Skin)

	UpdateDiagnostics(Diagnostics)

	// Leave ends the session of the player with its client in tx, the
	// transaction of the world it is in, nil if it is in none, unless it has
	// ended: the player tears itself down as quitting does after its quit
	// handler ran, and closes the client. An adapter calls it when its client
	// went away and the player did not quit at once.
	Leave(tx *world.Tx)
}
