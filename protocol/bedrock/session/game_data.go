package session

import (
	"context"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
	_ = conn.WritePacket(&v2193.ItemRegistryPacket{Items: c.ItemRegistry()})
	return nil
}

// gameData returns the netconn.GameData that shows a client of a protocol
// the game g, with the content c defines, on the version of the game the
// protocol tells its clients.
func gameData(g client.Game, c *startContent, p *versions.Protocol, emoteChatMuted bool) netconn.GameData {
	mode := GameTypeFromMode(g.Level.DefaultGameMode())
	difficulty, _ := world.DifficultyID(g.Level.Difficulty())
	permissions := v2193.PermissionLevelMember
	if g.Operator {
		permissions = v2193.PermissionLevelOperator
	}
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
		PlayerPermissions: permissions,
		PlayerPosition:    vec64To32(g.Position).Add(mgl32.Vec3{0, float32(player.Type.NetworkOffset())}),
		Dimension:         int32(dim),
		Yaw:               float32(g.Rotation.Yaw()),
		Pitch:             float32(g.Rotation.Pitch()),
		EmoteChatMuted:    emoteChatMuted,

		Items:  c.Items(),
		Blocks: c.Blocks(),
		GameRules: append([]v2193.GameRule{
			{Name: "naturalregeneration", Value: false},
			{Name: "locatorBar", Value: false},
			{Name: "falldamage", Value: g.Level.FallDamage()},
		}, ClientGameRules(g.Level)...),

		ServerAuthoritativeInventory: true,
		// Like BDS, name blocks by network block hash so clients resolve every
		// state by its contents instead of by its index in their own palette.
		UseBlockNetworkIDHashes: true,
		PlayerMovementSettings: v2193.PlayerMovementSettings{
			ServerAuthoritativeBlockBreaking: true,
		},
		Dimensions: c.Dimensions(),
	}
}
