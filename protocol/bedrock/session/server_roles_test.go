package session_test

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
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

// roleServer returns a server of the program's lists in a directory of the
// test, whose players are saved nowhere.
func roleServer(t *testing.T) (*teardownServer, string) {
	t.Helper()
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
	return startTeardownServer(t, conf, nil), dir
}

// requestConn is a permissionTestConn whose client sends the packets of
// in.
type requestConn struct {
	*permissionTestConn
	in chan proto.Packet
}

func (c *requestConn) ReadPacket() (proto.Packet, error) {
	select {
	case pk := <-c.in:
		return pk, nil
	case err := <-c.readErrors:
		return nil, err
	case <-c.closed:
		return nil, io.EOF
	}
}

// joinRolePlayer joins a client that logs in as name with the XUID passed,
// and returns it and the handle of its player, once spawned.
func joinRolePlayer(t *testing.T, ts *teardownServer, name, xuid string) (*requestConn, *world.EntityHandle) {
	t.Helper()
	conn := &requestConn{permissionTestConn: &permissionTestConn{teardownConn: newTeardownConn(), updates: make(chan struct{}, 1)},
		in: make(chan proto.Packet, 1)}
	conn.identity.DisplayName, conn.identity.XUID = name, xuid
	ts.l.conns <- conn
	<-conn.started
	return conn, ts.awaitSpawn(t)
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
	ts, dir := roleServer(t)
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

// TestRequestPermissionsPacket checks the roles an operator gives from the
// list of players of its client: an operator of level 3 (RoleLevel) makes
// a member a visitor, written to permissions.json, and an operator of
// level 2 a custom player of the abilities asked, no longer an operator;
// an operator of level 2 is ignored; a request for a player at or above
// the level of the one who asks is refused; an operator of level 4 makes
// a visitor an operator, its role of permissions.json taken off. A request
// not granted shows the player again as it is.
func TestRequestPermissionsPacket(t *testing.T) {
	ts, dir := roleServer(t)
	srv := ts.srv
	for name, level := range map[string]cmd.Level{"Owner": cmd.LevelOwner, "Admin": cmd.LevelAdmin, "Mod": cmd.LevelGameMaster} {
		if _, err := srv.SetOperatorLevel(name, level); err != nil {
			t.Fatal(err)
		}
	}
	conns, handles := map[string]*requestConn{}, map[string]*world.EntityHandle{}
	for i, name := range []string{"Owner", "Admin", "Mod", "Member"} {
		conns[name], handles[name] = joinRolePlayer(t, ts, name, fmt.Sprint("25354000000001", i))
	}
	visitor := shownPermissions{mc.PermissionLevelVisitor, mc.CommandPermissionLevelAny, 0}
	for _, s := range []struct {
		heading, by, target string
		level               int32
		bits                uint16
		want                shownPermissions
	}{
		{"level 3 makes a member a visitor", "Admin", "Member", int32(mc.PermissionLevelVisitor), 0, visitor},
		{"level 2 is ignored", "Mod", "Member", int32(mc.PermissionLevelMember), 0, visitor},
		{"level 3 makes an operator of level 2 custom", "Admin", "Mod", int32(mc.PermissionLevelCustom),
			mc.ActionPermissionBuild | mc.ActionPermissionMine,
			shownPermissions{mc.PermissionLevelCustom, mc.CommandPermissionLevelAny, mc.AbilityBuild | mc.AbilityMine}},
		{"level 3 cannot change level 4", "Admin", "Owner", int32(mc.PermissionLevelVisitor), 0,
			shownPermissions{mc.PermissionLevelOperator, mc.CommandPermissionLevelGameDirectors, roleBits}},
		{"no one changes their own role", "Admin", "Admin", int32(mc.PermissionLevelVisitor), 0,
			shownPermissions{mc.PermissionLevelOperator, mc.CommandPermissionLevelGameDirectors, roleBits}},
		{"level 4 makes a visitor an operator", "Owner", "Member", int32(mc.PermissionLevelOperator), 0,
			shownPermissions{mc.PermissionLevelOperator, mc.CommandPermissionLevelGameDirectors, roleBits}},
	} {
		target := conns[s.target]
		before := target.mark()
		id := handles[s.target].UniqueID()
		if s.by == s.target {
			id = 1
		}
		conns[s.by].in <- &mc.RequestPermissionsPacket{EntityUniqueID: id, PermissionLevel: s.level, RequestedPermissions: s.bits}
		target.waitShown(t, before, 1, s.want)
		if t.Failed() {
			t.Fatalf("%s: not shown", s.heading)
		}
	}
	roles, err := srv.RoleEntries()
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"mod: custom [build mine]"}; !slices.Equal(roles, want) {
		t.Errorf("permissions.json: %q, want %q", roles, want)
	}
	for name, want := range map[string]cmd.Level{"Owner": cmd.LevelOwner, "Admin": cmd.LevelAdmin, "Member": srv.OpLevel()} {
		if l, ok := srv.OperatorLevel(name); !ok || l != want {
			t.Errorf("%s: level %v, %v, want an operator of %v", name, l, ok, want)
		}
	}
	if _, ok := srv.OperatorLevel("Mod"); ok {
		t.Error("Mod is still an operator")
	}

	// Each request is in the audit log, as it ended.
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "audit.log"))
	if err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r audit.Record
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatal(err)
		}
		if r.Action == "role" {
			results = append(results, r.Actor.Name+" "+r.Target+" "+string(r.Result))
		}
	}
	// A refusal is shown as the player is, which an update of the step
	// before may stand for: the records of two steps may come in either
	// order.
	want := []string{"Admin Admin refused", "Admin Member ok", "Admin Mod ok", "Admin Owner refused", "Mod Member denied", "Owner Member ok"}
	if slices.Sort(results); !slices.Equal(results, want) {
		t.Errorf("audit records of roles: %q, want %q", results, want)
	}
}
