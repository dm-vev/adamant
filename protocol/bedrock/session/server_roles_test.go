package session_test

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// shownPermissions are the permissions a client is shown of its player:
// the permission level of StartGame and UpdateAbilities, the command
// permission level, and the ability bits of the role.
type shownPermissions struct {
	player, command uint8
	abilities       uint32
}

// roleBits are the ability bits of UpdateAbilities that the role decides.
const roleBits = mc.AbilityBuild | mc.AbilityMine | mc.AbilityDoorsAndSwitches | mc.AbilityOpenContainers |
	mc.AbilityAttackPlayers | mc.AbilityAttackMobs | mc.AbilityOperatorCommands | mc.AbilityTeleport

// memberBits are the bits of a member in survival.
const memberBits = mc.AbilityBuild | mc.AbilityMine | mc.AbilityDoorsAndSwitches | mc.AbilityOpenContainers |
	mc.AbilityAttackPlayers | mc.AbilityAttackMobs

// waitShown waits for the abilities of the player of uniqueID sent after
// the packet after, as want has them, and fails if they do not come.
func (c *permissionTestConn) waitShown(t *testing.T, after int, uniqueID int64, want shownPermissions) {
	t.Helper()
	c.wait(t, after, func(pk proto.Packet) bool {
		update, ok := pk.(*mc.UpdateAbilitiesPacket)
		if !ok || update.AbilityData.EntityUniqueID != uniqueID {
			return false
		}
		d := update.AbilityData
		return (shownPermissions{d.PlayerPermissions, d.CommandPermissions, d.Layers[0].Values & roleBits}) == want
	})
}

// TestClientSeesLevelAndRole checks, on a server keyed by name, what
// clients are shown of the level
// and the role of their players: the permission level of StartGame and of
// UpdateAbilities is the role's (visitor, member, operator, custom), the
// command permission level is Any at level 0 and GameDirectors from level
// 1 on, never higher, and the ability bits are the role's: none for a
// visitor, those listed for a custom role, the operator bits from level 2
// on. A change of a role, of a level and a reload of permissions.json are
// shown to the player within the 2 seconds of waitShown.
func TestClientSeesLevelAndRole(t *testing.T) {
	dir := t.TempDir()
	uc := server.DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Players.MaximumChunkRadius = 1
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	conf.Log = slog.New(slog.DiscardHandler)
	conf.PlayerProvider = player.NopProvider{}
	ts := startTeardownServer(t, conf, nil)
	srv := ts.srv
	if _, err := srv.Op("Owner"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.SetOperatorLevel("Moderator", cmd.LevelModerator); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.SetRole("Guest", client.RoleVisitor, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.SetRole("Builder", client.RoleCustom, client.AbilityBuild|client.AbilityMine); err != nil {
		t.Fatal(err)
	}

	conns := map[string]*permissionTestConn{}
	for i, s := range []struct {
		name string
		want shownPermissions
	}{
		{"Member", shownPermissions{mc.PermissionLevelMember, mc.CommandPermissionLevelAny, memberBits}},
		{"Guest", shownPermissions{mc.PermissionLevelVisitor, mc.CommandPermissionLevelAny, 0}},
		{"Builder", shownPermissions{mc.PermissionLevelCustom, mc.CommandPermissionLevelAny, mc.AbilityBuild | mc.AbilityMine}},
		{"Moderator", shownPermissions{mc.PermissionLevelOperator, mc.CommandPermissionLevelGameDirectors, memberBits}},
		{"Owner", shownPermissions{mc.PermissionLevelOperator, mc.CommandPermissionLevelGameDirectors, roleBits}},
	} {
		conn := &permissionTestConn{teardownConn: newTeardownConn(), updates: make(chan struct{}, 1)}
		conn.identity.DisplayName, conn.identity.XUID = s.name, fmt.Sprint("25354000000000", i+1)
		ts.l.conns <- conn
		data := <-conn.started
		ts.awaitSpawn(t)
		if data.PlayerPermissions != s.want.player {
			t.Errorf("%s: StartGame permission level %d, want %d", s.name, data.PlayerPermissions, s.want.player)
		}
		conn.waitShown(t, 0, 1, s.want)
		conns[s.name] = conn
	}

	// A role given to a member, a level to it, then the role taken off.
	member := conns["Member"]
	for _, s := range []struct {
		change func() (bool, error)
		want   shownPermissions
	}{
		{func() (bool, error) { return srv.SetRole("Member", client.RoleVisitor, 0) },
			shownPermissions{mc.PermissionLevelVisitor, mc.CommandPermissionLevelAny, 0}},
		{func() (bool, error) { return srv.SetOperatorLevel("Member", cmd.LevelGameMaster) },
			shownPermissions{mc.PermissionLevelVisitor, mc.CommandPermissionLevelGameDirectors, 0}},
		{func() (bool, error) { return srv.ClearRole("Member") },
			shownPermissions{mc.PermissionLevelOperator, mc.CommandPermissionLevelGameDirectors, roleBits}},
	} {
		before := member.mark()
		if changed, err := s.change(); err != nil || !changed {
			t.Fatalf("change = %v, %v", changed, err)
		}
		member.waitShown(t, before, 1, s.want)
	}

	// /permission reload: the visitor of permissions.json becomes custom.
	before := conns["Guest"].mark()
	const roles = `[{"permission": "custom", "name": "guest", "abilities": ["build"]}]`
	if err := os.WriteFile(filepath.Join(dir, "permissions.json"), []byte(roles), 0644); err != nil {
		t.Fatal(err)
	}
	if err := srv.ReloadPermissions(); err != nil {
		t.Fatal(err)
	}
	conns["Guest"].waitShown(t, before, 1, shownPermissions{mc.PermissionLevelCustom, mc.CommandPermissionLevelAny, mc.AbilityBuild})
}
