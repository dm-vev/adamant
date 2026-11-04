package session

import (
	"context"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
)

// startGame starts the game g for conn, as BDS does: it sends StartGame,
// with the content c defines and the emotes of the chat muted if
// emoteChatMuted is set, then the ItemRegistry of the custom items. It
// returns the error of StartGame, after which nothing is written.
func startGame(ctx context.Context, conn Conn, g client.Game, c *startContent, emoteChatMuted bool) error {
	if err := conn.StartGameContext(ctx, gameData(g, c, protocolOf(conn), emoteChatMuted)); err != nil {
		return err
	}
	_ = conn.WritePacket(&mc.ItemRegistryPacket{Items: c.ItemRegistry()})
	return nil
}

// gameData returns the netconn.GameData that shows a client of a protocol
// the game g, with the content c defines, on the version of the game the
// protocol tells its clients.
func gameData(g client.Game, c *startContent, p *versions.Protocol, emoteChatMuted bool) netconn.GameData {
	mode := GameTypeFromMode(g.Level.DefaultGameMode())
	difficulty, _ := world.DifficultyID(g.Level.Difficulty())
	dim, _ := world.DimensionID(g.World.Dimension())
	return netconn.GameData{
		// Entity runtime/unique ID for the player itself is always 1 in df.
		EntityUniqueID:  1,
		EntityRuntimeID: 1,

		WorldName:       g.Name,
		WorldSeed:       g.Level.Seed(),
		BaseGameVersion: p.Version,

		Time:       int64(g.Level.Time()),
		Difficulty: int32(difficulty),

		WorldGameMode:     mode,
		PlayerGameMode:    mode,
		PlayerPermissions: permissionLevel(g.Permissions.Role),
		PlayerPosition:    vec64To32(g.Position).Add(mgl32.Vec3{0, float32(player.Type.NetworkOffset())}),
		Dimension:         int32(dim),
		Yaw:               float32(g.Rotation.Yaw()),
		Pitch:             float32(g.Rotation.Pitch()),
		EmoteChatMuted:    emoteChatMuted,

		Items:  c.Items(),
		Blocks: c.Blocks(),
		GameRules: append([]mc.GameRule{
			{Name: "naturalregeneration", Value: false},
			{Name: "locatorBar", Value: false},
			{Name: "falldamage", Value: g.Level.FallDamage()},
		}, ClientGameRules(g.Level)...),

		ServerAuthoritativeInventory: true,
		// Like BDS, name blocks by network block hash so clients resolve every
		// state by its contents instead of by its index in their own palette.
		UseBlockNetworkIDHashes: true,
		PlayerMovementSettings: mc.PlayerMovementSettings{
			ServerAuthoritativeBlockBreaking: true,
		},
		Dimensions: c.Dimensions(),
	}
}

// permissionLevel returns the permission level a client is shown for the
// role of its player.
func permissionLevel(r client.Role) uint8 {
	switch r {
	case client.RoleVisitor:
		return mc.PermissionLevelVisitor
	case client.RoleOperator:
		return mc.PermissionLevelOperator
	case client.RoleCustom:
		return mc.PermissionLevelCustom
	default:
		return mc.PermissionLevelMember
	}
}

// commandLevel returns the command permission level a client is shown for
// the level of the commands of its player: game directors from level 1 on,
// as BDS shows its operators; Admin, Host and Owner are never sent, as BDS
// sends none of them.
func commandLevel(level uint8) uint8 {
	if level > 0 {
		return mc.CommandPermissionLevelGameDirectors
	}
	return mc.CommandPermissionLevelAny
}

// roleAbilities returns the ability bits a client is shown for its player
// in the game mode passed with the permissions passed: what the game mode
// allows of building, mining and interacting, as far as the role allows
// it, and the bits of operator commands and teleport, which the role
// gives alone.
func roleAbilities(mode world.GameMode, perms client.Permissions) uint32 {
	var abilities uint32
	for _, a := range []struct {
		allowed bool
		role    client.Abilities
		bit     uint32
	}{
		{mode.AllowsEditing(), client.AbilityBuild, mc.AbilityBuild},
		{mode.AllowsEditing(), client.AbilityMine, mc.AbilityMine},
		{mode.AllowsInteraction(), client.AbilityDoorsAndSwitches, mc.AbilityDoorsAndSwitches},
		{mode.AllowsInteraction(), client.AbilityOpenContainers, mc.AbilityOpenContainers},
		{mode.AllowsInteraction(), client.AbilityAttackPlayers, mc.AbilityAttackPlayers},
		{mode.AllowsInteraction(), client.AbilityAttackMobs, mc.AbilityAttackMobs},
		{true, client.AbilityOperatorCommands, mc.AbilityOperatorCommands},
		{true, client.AbilityTeleport, mc.AbilityTeleport},
	} {
		if a.allowed && perms.Allows(a.role) {
			abilities |= a.bit
		}
	}
	return abilities
}
