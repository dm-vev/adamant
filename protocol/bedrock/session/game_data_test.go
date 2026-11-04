package session

import (
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/versions"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
)

// oldGameData is the game data the server made itself before the session
// made it from a client.Game: its defaultGameData, as finaliseConn then
// changed it for the player d in w. It is the oracle of
// TestGameDataFromGame.
func oldGameData(level *world.World, name string, content *startContent, pos mgl64.Vec3, rot cube.Rotation, w *world.World, muted, operator bool) netconn.GameData {
	mode := GameTypeFromMode(level.DefaultGameMode())
	difficulty, _ := world.DifficultyID(level.Difficulty())
	data := netconn.GameData{
		EntityUniqueID:  1,
		EntityRuntimeID: 1,

		WorldName:       name,
		WorldSeed:       level.Seed(),
		BaseGameVersion: versions.Default().Version,

		Time:       int64(level.Time()),
		Difficulty: int32(difficulty),

		WorldGameMode:     mode,
		PlayerGameMode:    mode,
		PlayerPermissions: mc.PermissionLevelMember,
		PlayerPosition:    vec64To32(level.Spawn().Vec3Centre().Add(mgl64.Vec3{0, player.Type.NetworkOffset()})),

		Items:  content.Items(),
		Blocks: content.Blocks(),
		GameRules: append([]mc.GameRule{
			{Name: "naturalregeneration", Value: false},
			{Name: "locatorBar", Value: false},
			{Name: "falldamage", Value: level.FallDamage()},
		}, ClientGameRules(level)...),

		ServerAuthoritativeInventory: true,
		UseBlockNetworkIDHashes:      true,
		PlayerMovementSettings: mc.PlayerMovementSettings{
			ServerAuthoritativeBlockBreaking: true,
		},
		Dimensions: content.Dimensions(),
	}

	data.PlayerPosition = vec64To32(pos).Add(mgl32.Vec3{0, float32(player.Type.NetworkOffset())})
	dim, _ := world.DimensionID(w.Dimension())
	data.Dimension = int32(dim)
	data.Yaw, data.Pitch = float32(rot.Yaw()), float32(rot.Pitch())

	data.EmoteChatMuted = muted
	if operator {
		data.PlayerPermissions = mc.PermissionLevelOperator
	}
	return data
}

// TestGameDataFromGame checks that the game data the session makes from a
// client.Game is the game data the server made itself.
func TestGameDataFromGame(t *testing.T) {
	newWorld := func(dim world.Dimension, mode world.GameMode, fallDamage bool) *world.World {
		settings := &world.Settings{Name: "Level", DefaultGameMode: mode, FallDamage: fallDamage, Difficulty: world.DifficultyHard, Time: 1234, Spawn: cube.Pos{8, 70, -3}}
		w := world.Config{Synchronous: true, Provider: world.NopProvider{Set: settings}, Dim: dim, Seed: 987654321}.New()
		t.Cleanup(func() { _ = w.Close() })
		return w
	}
	plain := newStartContent(tablesOf(nil, world.DefaultBlockRegistry), nil, nil)
	custom := newStartContent(tablesOf(nil, world.DefaultBlockRegistry), nil, []world.DimensionRegistration{{ID: 3, Name: "adamant:test", Dimension: world.Overworld}})

	survival := newWorld(world.Overworld, world.GameModeSurvival, true)
	nether := newWorld(world.Nether, world.GameModeSurvival, true)
	for name, tc := range map[string]struct {
		level, w *world.World
		content  *startContent
		operator bool
		muted    bool
	}{
		"survival":         {level: survival, w: survival, content: plain},
		"spectator":        {level: newWorld(world.Overworld, world.GameModeSpectator, true), content: plain},
		"creative":         {level: newWorld(world.Overworld, world.GameModeCreative, true), content: plain},
		"no fall damage":   {level: newWorld(world.Overworld, world.GameModeSurvival, false), content: plain},
		"operator":         {level: survival, w: survival, content: plain, operator: true},
		"emotes muted":     {level: survival, w: survival, content: plain, muted: true},
		"in the nether":    {level: survival, w: nether, content: plain},
		"custom dimension": {level: survival, w: survival, content: custom},
		"no content":       {level: survival, w: survival},
	} {
		t.Run(name, func(t *testing.T) {
			if tc.w == nil {
				tc.w = tc.level
			}
			pos, rot := mgl64.Vec3{12.25, -58.5, 3.75}, cube.Rotation{91.5, -12.25}
			perms := client.MemberPermissions()
			if tc.operator {
				perms = client.Permissions{Level: 4, Operator: true, Role: client.RoleOperator, Abilities: client.AbilitiesOfOperators}
			}
			g := client.Game{Name: "Golden Server", Level: tc.level, World: tc.w, Position: pos, Rotation: rot, Permissions: perms}
			got := gameData(g, tc.content, versions.Default(), tc.muted)
			want := oldGameData(tc.level, "Golden Server", tc.content, pos, rot, tc.w, tc.muted, tc.operator)
			for _, d := range []*netconn.GameData{&got, &want} {
				slices.SortFunc(d.Items, func(a, b mc.ItemEntry) int { return strings.Compare(a.Name, b.Name) })
				slices.SortStableFunc(d.GameRules, func(a, b mc.GameRule) int { return strings.Compare(a.Name, b.Name) })
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("game data differs from the oracle:\n got %+v\nwant %+v", withoutContent(got), withoutContent(want))
			}
			if got.PlayerGameMode != GameTypeFromMode(tc.level.DefaultGameMode()) {
				t.Fatalf("player game mode = %d", got.PlayerGameMode)
			}
		})
	}
}

// withoutContent returns d without its blocks and items, to be printed.
func withoutContent(d netconn.GameData) netconn.GameData {
	d.Blocks, d.Items = nil, nil
	return d
}
