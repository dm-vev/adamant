package client

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Game is what the server hands the adapter of a client as it starts the
// game for it: where the player spawns, the settings of the server that
// the client is shown, and the hooks of the server that the adapter calls
// for as long as the client plays. The adapter shows it in the terms of its
// protocol.
type Game struct {
	// Name is the name of the server.
	Name string
	// Level is the default world of the server. The client is shown its
	// settings as those of the game: seed, time, difficulty, default game
	// mode, fall damage and game rules.
	Level *world.World
	// World is the world the player spawns in. The client is shown its
	// dimension first, and its block registry encodes the blocks.
	World *world.World
	// Position is where the feet of the player are as it spawns, and
	// Rotation is where it looks.
	Position mgl64.Vec3
	Rotation cube.Rotation
	// Permissions are what the player may do as the server starts its
	// game: the level of its commands, its role and the abilities of the
	// role. PermissionsOf tells them later.
	Permissions Permissions
	// MaxChunkRadius bounds the radius, in chunks, of the world around the
	// player that the client is shown.
	MaxChunkRadius int
	// PermissionsOf returns what the player online of the UUID passed may
	// do now. Nil gives every player the permissions of a member of level
	// 0 (MemberPermissions). It may be called from any goroutine.
	PermissionsOf func(id uuid.UUID) Permissions
	// Roster is the list of the players of the server. Bind lists the
	// player on it and Close unlists it. Nil gives the client a Roster of
	// its own, on which it lists only its own player.
	Roster *Roster
}
