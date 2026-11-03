package session_test

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/protocol/bedrock/netconn"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/google/uuid"
)

func TestServerOperatorPermissionsAtLoginAndAfterChanges(t *testing.T) {
	// As the program makes it: the operators list is the ops.json beside
	// the whitelist.
	dir := t.TempDir()
	uc := server.DefaultConfig()
	uc.World.SaveData = false
	uc.World.DisableNether, uc.World.DisableEnd = true, true
	uc.Players.SaveData = false
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
	if _, err := srv.Op("Alice"); err != nil {
		t.Fatal(err)
	}
	commandName := "permission-test-" + uuid.NewString()
	cmd.Register(cmd.New(commandName, "Permission regression", nil, permissionTestCommand{srv: srv}))

	alice, conn, data := joinPermissionPlayer(t, ts, "Alice")
	if data.PlayerPermissions != v2193.PermissionLevelOperator {
		t.Errorf("StartGame permissions = %d, want operator", data.PlayerPermissions)
	}
	conn.waitPermission(t, 0, 1, true)
	conn.waitCommand(t, 0, commandName, true)

	// A joining client must see the existing operator's permissions even
	// before receiving the player's actor or loading its chunk.
	_, observer, observerData := joinPermissionPlayer(t, ts, "Bob")
	if observerData.PlayerPermissions != v2193.PermissionLevelMember {
		t.Errorf("member StartGame permissions = %d", observerData.PlayerPermissions)
	}
	observer.waitPermission(t, 0, alice.UniqueID(), true)
	observer.waitPermission(t, 0, 1, false)

	for _, operator := range []bool{false, true, false} {
		before, observerBefore := conn.mark(), observer.mark()
		// Run from the same world owner as the target, just like /deop self.
		// Name matching must follow the case-insensitive operator list.
		err := player.Do(alice, func(_ *world.Tx, _ *player.Player) {
			var changed bool
			var err error
			if operator {
				changed, err = srv.Op("aLiCe")
			} else {
				changed, err = srv.Deop("aLiCe")
			}
			if err != nil || !changed {
				t.Errorf("operator change = %t, %v", changed, err)
			}
		}).Wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		conn.waitPermission(t, before, 1, operator)
		observer.waitPermission(t, observerBefore, alice.UniqueID(), operator)
		conn.waitCommand(t, before, commandName, operator)
	}

	// A later ability resend (for example after a game mode change) must
	// consult current permissions rather than restore the login status.
	before := conn.mark()
	if err := player.Do(alice, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	abilities := conn.waitPermission(t, before, 1, false)
	if abilities.Layers[0].Values&v2193.AbilityMayFly == 0 {
		t.Error("permission update lost creative flight")
	}
}

type permissionTestCommand struct{ srv *server.Server }

func (c permissionTestCommand) Allow(src cmd.Source) bool {
	p, ok := src.(*player.Player)
	return ok && c.srv.IsOperator(p.Name())
}
func (permissionTestCommand) Run(cmd.Source, *cmd.Output, *world.Tx) {}

// joinPermissionPlayer joins a client that logs in as name to the server
// and returns the handle of its player, once spawned, its connection and
// the game it was started with.
func joinPermissionPlayer(t *testing.T, ts *teardownServer, name string) (*world.EntityHandle, *permissionTestConn, netconn.GameData) {
	t.Helper()
	conn := &permissionTestConn{teardownConn: newTeardownConn(), updates: make(chan struct{}, 1)}
	conn.identity.DisplayName = name
	ts.l.conns <- conn
	data := <-conn.started
	return ts.awaitSpawn(t), conn, data
}

type permissionTestConn struct {
	*teardownConn
	mu      sync.Mutex
	packets []proto.Packet
	updates chan struct{}
}

func (c *permissionTestConn) WritePacket(pk proto.Packet) error {
	c.mu.Lock()
	c.packets = append(c.packets, pk)
	c.mu.Unlock()
	select {
	case c.updates <- struct{}{}:
	default:
	}
	return nil
}
func (c *permissionTestConn) mark() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.packets)
}
func (c *permissionTestConn) wait(t *testing.T, after int, matches func(proto.Packet) bool) proto.Packet {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		c.mu.Lock()
		var found proto.Packet
		for _, pk := range c.packets[after:] {
			if matches(pk) {
				found = pk
				break
			}
		}
		c.mu.Unlock()
		if found != nil {
			return found
		}
		select {
		case <-c.updates:
		case <-timer.C:
			t.Fatal("timed out waiting for permission/command update")
		}
	}
}
func (c *permissionTestConn) waitPermission(t *testing.T, after int, uniqueID int64, operator bool) v2193.AbilityData {
	t.Helper()
	pk := c.wait(t, after, func(pk proto.Packet) bool {
		update, ok := pk.(*v2193.UpdateAbilitiesPacket)
		if !ok || update.AbilityData.EntityUniqueID != uniqueID {
			return false
		}
		return (update.AbilityData.PlayerPermissions == v2193.PermissionLevelOperator) == operator
	}).(*v2193.UpdateAbilitiesPacket)
	data := pk.AbilityData
	wantPlayer, wantCommand := byte(v2193.PermissionLevelMember), byte(v2193.CommandPermissionLevelAny)
	wantFlags := uint32(0)
	if operator {
		wantPlayer, wantCommand = v2193.PermissionLevelOperator, v2193.CommandPermissionLevelGameDirectors
		wantFlags = v2193.AbilityOperatorCommands | v2193.AbilityTeleport
	}
	if data.PlayerPermissions != wantPlayer || data.CommandPermissions != wantCommand {
		t.Errorf("permissions = %d/%d, want %d/%d", data.PlayerPermissions, data.CommandPermissions, wantPlayer, wantCommand)
	}
	if flags := data.Layers[0].Values & (v2193.AbilityOperatorCommands | v2193.AbilityTeleport); flags != wantFlags {
		t.Errorf("operator ability flags = %#x, want %#x", flags, wantFlags)
	}
	return data
}
func (c *permissionTestConn) waitCommand(t *testing.T, after int, name string, allowed bool) {
	t.Helper()
	c.wait(t, after, func(pk proto.Packet) bool {
		commands, ok := pk.(*v2193.AvailableCommandsPacket)
		if !ok {
			return false
		}
		for _, command := range commands.Commands {
			if command.Name == name {
				return allowed
			}
		}
		return !allowed
	})
}
