package server

import (
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/google/uuid"
)

// bdsPermissions is a permissions.json as Bedrock Dedicated Server writes
// it, with a custom role and a name of this server.
const bdsPermissions = `[
  {"permission": "operator", "xuid": "2535412345678901"},
  {"permission": "visitor", "xuid": "2535498765432109"},
  {"permission": "custom", "xuid": "2535400000000003", "abilities": ["build", "mine"]},
  {"permission": "member", "name": "Guest"}
]`

// roleServer returns a server keyed by XUID whose lists are in dir, with
// the DefaultRole passed.
func roleServer(t *testing.T, dir, defaultRole string) *Server {
	t.Helper()
	uc := DefaultConfig()
	uc.World.SaveData, uc.Players.SaveData = false, false
	uc.Server.IdentityKey = "xuid"
	uc.Whitelist.File = filepath.Join(dir, "white-list.txt")
	uc.Permissions.DefaultRole = defaultRole
	conf, err := uc.Config(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	return adminServer(t, conf)
}

// roleAccount returns the identity of an account Xbox Live vouched for.
func roleAccount(name, xuid string) client.Identity {
	return client.Identity{Name: name, XUID: xuid, UUID: uuid.New(), Authenticated: true}
}

// TestPermissionsJSONFromBDS checks that the permissions.json of BDS reads:
// its operator moves to ops.json, with no level of its own, and the file
// before is kept as permissions.json.before-roles; the visitor and the
// custom role have their abilities; the name held binds to the account
// that joins with it; a player without an entry has the DefaultRole.
func TestPermissionsJSONFromBDS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	if err := os.WriteFile(path, []byte(bdsPermissions), 0644); err != nil {
		t.Fatal(err)
	}
	srv := roleServer(t, dir, "")
	if l, ok := srv.OperatorLevel("xuid:2535412345678901"); !ok || l != cmd.LevelOwner {
		t.Errorf("the operator of permissions.json: level %v, %v", l, ok)
	}
	if got := string(readTestFile(t, path+".before-roles")); got != bdsPermissions {
		t.Errorf("permissions.json.before-roles:\n%s", got)
	}
	const rewritten = "[\n  {\n    \"permission\": \"visitor\",\n    \"xuid\": \"2535498765432109\"\n  },\n  {\n    \"permission\": \"custom\",\n    \"xuid\": \"2535400000000003\",\n    \"abilities\": [\n      \"build\",\n      \"mine\"\n    ]\n  },\n  {\n    \"permission\": \"member\",\n    \"name\": \"guest\"\n  }\n]\n"
	if got := string(readTestFile(t, path)); got != rewritten {
		t.Errorf("permissions.json without its operator:\n%s\nwant\n%s", got, rewritten)
	}

	for _, tc := range []struct {
		id    client.Identity
		role  client.Role
		abils client.Abilities
		level uint8
	}{
		{roleAccount("Op", "2535412345678901"), client.RoleOperator, client.AbilitiesOfOperators, 4},
		{roleAccount("Visitor", "2535498765432109"), client.RoleVisitor, 0, 0},
		{roleAccount("Custom", "2535400000000003"), client.RoleCustom, client.AbilityBuild | client.AbilityMine, 0},
		{roleAccount("Stranger", "2535400000000004"), client.RoleMember, client.AbilitiesOfMembers, 0},
	} {
		got := srv.permissionsOf(tc.id)
		if got.Role != tc.role || got.Abilities != tc.abils || got.Level != tc.level {
			t.Errorf("%s: %+v, want role %v abilities %v level %d", tc.id.Name, got, tc.role, tc.abils.Names(), tc.level)
		}
	}

	// The name held binds to the first account that joins with it.
	guest := roleAccount("Guest", "2535400000000005")
	if _, ok := srv.conf.Allower.Allow(&net.UDPAddr{IP: net.IPv4(203, 0, 113, 1)}, guest, client.Profile{}); !ok {
		t.Fatal("the guest was refused")
	}
	if en, ok := srv.roleEntry(guest); !ok || en.role.role != client.RoleMember {
		t.Errorf("the guest after joining: %+v, %v", en, ok)
	}

	// SetRole and ClearRole.
	if changed, err := srv.SetRole("xuid:2535400000000004", client.RoleVisitor, 0); err != nil || !changed {
		t.Fatalf("SetRole = %v, %v", changed, err)
	}
	if got := srv.permissionsOf(roleAccount("Stranger", "2535400000000004")); got.Role != client.RoleVisitor || got.Abilities != 0 {
		t.Errorf("the stranger made a visitor: %+v", got)
	}
	if _, err := srv.SetRole("xuid:2535400000000004", client.RoleOperator, 0); err == nil {
		t.Error("SetRole made an operator")
	}
	if removed, err := srv.ClearRole("xuid:2535400000000004"); err != nil || !removed {
		t.Fatalf("ClearRole = %v, %v", removed, err)
	}
	entries, _ := srv.RoleEntries()
	want := []string{"xuid:2535498765432109: visitor", "xuid:2535400000000003: custom [build mine]", "Guest (xuid:2535400000000005): member"}
	if len(entries) != len(want) {
		t.Fatalf("RoleEntries() = %q, want %q", entries, want)
	}
	for i := range want {
		if entries[i] != want[i] {
			t.Errorf("RoleEntries()[%d] = %q, want %q", i, entries[i], want[i])
		}
	}

	// An operator whose role is set explicitly has the abilities of the
	// role, and the commands of its level.
	if _, err := srv.SetRole("xuid:2535412345678901", client.RoleVisitor, 0); err != nil {
		t.Fatal(err)
	}
	if got := srv.permissionsOf(roleAccount("Op", "2535412345678901")); got.Role != client.RoleVisitor || got.Abilities != 0 || got.Level != 4 || !got.Operator {
		t.Errorf("an operator made a visitor: %+v", got)
	}

	// DefaultRole = "visitor".
	visitorsDir := t.TempDir()
	visitors := roleServer(t, visitorsDir, "visitor")
	if got := visitors.permissionsOf(roleAccount("Stranger", "2535400000000004")); got.Role != client.RoleVisitor || got.Abilities != 0 {
		t.Errorf("a stranger of a server of visitors: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(visitorsDir, "permissions.json")); err == nil {
		t.Error("a server made permissions.json where it had none")
	}
}

// readTestFile returns the contents of the file at path.
func readTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
