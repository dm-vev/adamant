package server

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestOperatorPermissionsAtLoginAndAfterChanges(t *testing.T) {
	srv := newSessionTestServer(t, player.NopProvider{})
	var err error
	srv.operators, err = loadOperators(filepath.Join(t.TempDir(), "ops.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Op("Alice"); err != nil {
		t.Fatal(err)
	}
	commandName := "permission-test-" + uuid.NewString()
	cmd.Register(cmd.New(commandName, "Permission regression", nil, permissionTestCommand{srv: srv}))

	alice, conn, data := joinPermissionPlayer(t, srv, "Alice")
	if data.PlayerPermissions != packet.PermissionLevelOperator {
		t.Errorf("StartGame permissions = %d, want operator", data.PlayerPermissions)
	}
	conn.waitPermission(t, 0, 1, true)
	conn.waitCommand(t, 0, commandName, true)

	// A joining client must see the existing operator's permissions even
	// before receiving the player's actor or loading its chunk.
	_, observer, observerData := joinPermissionPlayer(t, srv, "Bob")
	if observerData.PlayerPermissions != packet.PermissionLevelMember {
		t.Errorf("member StartGame permissions = %d", observerData.PlayerPermissions)
	}
	observer.waitPermission(t, 0, alice.p.handle.UniqueID(), true)
	observer.waitPermission(t, 0, 1, false)

	for _, operator := range []bool{false, true, false} {
		before, observerBefore := conn.mark(), observer.mark()
		// Run from the same world owner as the target, just like /deop self.
		// Name matching must follow the case-insensitive operator list.
		err := player.Do(alice.p.handle, func(_ *world.Tx, _ *player.Player) {
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
		observer.waitPermission(t, observerBefore, alice.p.handle.UniqueID(), operator)
		conn.waitCommand(t, before, commandName, operator)
	}

	// A later ability resend (for example after a game mode change) must
	// consult current permissions rather than restore the login status.
	before := conn.mark()
	if err := player.Do(alice.p.handle, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeCreative)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	abilities := conn.waitPermission(t, before, 1, false)
	if abilities.Layers[0].Values&protocol.AbilityMayFly == 0 {
		t.Error("permission update lost creative flight")
	}
}

type permissionTestCommand struct{ srv *Server }

func (c permissionTestCommand) Allow(src cmd.Source) bool {
	p, ok := src.(*player.Player)
	return ok && c.srv.IsOperator(p.Name())
}
func (permissionTestCommand) Run(cmd.Source, *cmd.Output, *world.Tx) {}

func joinPermissionPlayer(t *testing.T, srv *Server, name string) (incoming, *permissionTestConn, minecraft.GameData) {
	t.Helper()
	conn := &permissionTestConn{serverPanicConn: newServerPanicConn(), updates: make(chan struct{}, 1)}
	conn.identity.DisplayName = name
	done := make(chan struct{})
	go func() {
		srv.finaliseConn(context.Background(), conn, nil)
		close(done)
	}()
	data := <-conn.started
	inc := <-srv.incoming
	<-done
	id := uuid.MustParse(conn.identity.Identity)
	srv.pmu.Lock()
	srv.p[id] = inc.p
	srv.pmu.Unlock()
	t.Cleanup(func() {
		inc.s.CloseConnection()
		if !srv.waitForPlayerRemoval(id, 5*time.Second) {
			t.Error("permission test player did not disconnect")
		}
	})
	if err := inc.w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(inc.p.handle).(*player.Player)
		inc.s.Spawn(p, tx)
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	return inc, conn, data
}

type permissionTestConn struct {
	*serverPanicConn
	mu      sync.Mutex
	packets []packet.Packet
	updates chan struct{}
}

func (c *permissionTestConn) WritePacket(pk packet.Packet) error {
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
func (c *permissionTestConn) wait(t *testing.T, after int, matches func(packet.Packet) bool) packet.Packet {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		c.mu.Lock()
		var found packet.Packet
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
func (c *permissionTestConn) waitPermission(t *testing.T, after int, uniqueID int64, operator bool) protocol.AbilityData {
	t.Helper()
	pk := c.wait(t, after, func(pk packet.Packet) bool {
		update, ok := pk.(*packet.UpdateAbilities)
		if !ok || update.AbilityData.EntityUniqueID != uniqueID {
			return false
		}
		return (update.AbilityData.PlayerPermissions == packet.PermissionLevelOperator) == operator
	}).(*packet.UpdateAbilities)
	data := pk.AbilityData
	wantPlayer, wantCommand := byte(packet.PermissionLevelMember), byte(protocol.CommandPermissionLevelAny)
	wantFlags := uint32(0)
	if operator {
		wantPlayer, wantCommand = packet.PermissionLevelOperator, protocol.CommandPermissionLevelGameDirectors
		wantFlags = protocol.AbilityOperatorCommands | protocol.AbilityTeleport
	}
	if data.PlayerPermissions != wantPlayer || data.CommandPermissions != wantCommand {
		t.Errorf("permissions = %d/%d, want %d/%d", data.PlayerPermissions, data.CommandPermissions, wantPlayer, wantCommand)
	}
	if flags := data.Layers[0].Values & (protocol.AbilityOperatorCommands | protocol.AbilityTeleport); flags != wantFlags {
		t.Errorf("operator ability flags = %#x, want %#x", flags, wantFlags)
	}
	return data
}
func (c *permissionTestConn) waitCommand(t *testing.T, after int, name string, allowed bool) {
	t.Helper()
	c.wait(t, after, func(pk packet.Packet) bool {
		commands, ok := pk.(*packet.AvailableCommands)
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
