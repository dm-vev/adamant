package session_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestGoldenServerJoin records a member joining a server.Server: the game
// the server starts, the items it writes after, what the session it makes
// sends as the player spawns through Accept, and what the first ticks send
// until the chunks around the spawn are in. The server then tells of the
// player online.
func TestGoldenServerJoin(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_join", serverConfig{})
	s.join("Golden joins as a member", goldenIdentity)
	s.server()
	s.snapshot()
}

// TestGoldenServerOperator records an operator of the operators list
// joining: the game starts with the permissions of an operator, and the
// abilities and commands the session sends are an operator's. The operator
// is then removed from the list and added again while online.
func TestGoldenServerOperator(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_operator", serverConfig{brief: true})
	if _, err := s.srv.Op(goldenName); err != nil {
		t.Fatal(err)
	}
	g := s.join("Golden joins as an operator", goldenIdentity)
	s.server()
	s.permissions("Golden is removed from the operators", g, func() {
		if _, err := s.srv.Deop(goldenName); err != nil {
			t.Fatal(err)
		}
	})
	s.permissions("Golden is made an operator again", g, func() {
		if _, err := s.srv.Op(goldenName); err != nil {
			t.Fatal(err)
		}
	})
	s.snapshot()
}

// permissions runs f, which changes the operators list, and records what
// the client is sent as the server refreshes the permissions of its player
// on a goroutine of its own: the abilities, then, in the next tick, the
// commands, which the step settles for.
func (s *serverScenario) permissions(heading string, c *serverClient, f func()) {
	s.t.Helper()
	fmt.Fprintf(&s.out, "== %s\n", heading)
	f()
	c.readUntil(func(pk proto.Packet) bool {
		_, ok := pk.(*mc.UpdateAbilitiesPacket)
		return ok
	})
	s.endStep(s.online(), false)
}

// TestGoldenServerReconnect records a client logging in as a player that
// is still online, as the server handles it by default: the player online
// is disconnected and saved, and the new client joins in its place, with
// the data the server saved of the other.
func TestGoldenServerReconnect(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_reconnect", serverConfig{brief: true})
	g := s.join("Golden joins", goldenIdentity)
	s.step("Golden moves and gains experience", func(tx *world.Tx) {
		p := g.player(tx)
		p.Teleport(p.Position().Add(mgl64.Vec3{3, 0, 2}))
		p.SetExperienceLevel(3)
	})

	var moved mgl64.Vec3
	<-s.srv.World().Exec(func(tx *world.Tx) { moved = g.player(tx).Position() })

	heading := "another client logs in as Golden while Golden is online"
	fmt.Fprintf(&s.out, "== %s\n", heading)
	again := s.connect(goldenIdentity)
	s.out.WriteString("-- Golden, the client online\n")
	g.readClosed()
	g.write(g.take(), true)
	g.left()
	s.awaitSave(goldenName)
	if h := s.awaitSpawn(heading); h != again.handle() {
		t.Fatal("the server spawned another player than the client logging in again")
	}
	// The new client plays on from where the other left.
	<-s.srv.World().Exec(func(tx *world.Tx) {
		p := again.player(tx)
		if p.Position() != moved || p.ExperienceLevel() != 3 {
			t.Errorf("the player of the new client is at %v with experience level %d, want %v and 3, as the other was saved", p.Position(), p.ExperienceLevel(), moved)
		}
	})
	s.joined(heading, again, nil)
	s.server()
	s.snapshot()
}

// TestGoldenServerReconnectKickJoining records a client logging in as a
// player still online on a server that keeps the player online: the
// listener disconnects the client, which is sent nothing, and the player
// stays.
func TestGoldenServerReconnectKickJoining(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_reconnect_kick_joining", serverConfig{brief: true, user: func(uc *server.UserConfig) {
		uc.Server.ReconnectPolicy = "kick-joining"
	}})
	g := s.join("Golden joins", goldenIdentity)
	s.out.WriteString("== another client logs in as Golden while Golden is online\n")
	again := s.connect(goldenIdentity)
	s.out.WriteString("-- the client logging in\n")
	again.readClosed()
	again.write(again.take(), false)
	again.left()
	s.endStep([]*serverClient{g}, false)
	s.providerLog()
	s.server()
	s.snapshot()
}

// TestGoldenServerStartFails records a client whose game does not start,
// as one that went away before it answered StartGame: the listener
// disconnects it with "Connection timeout.", it is sent nothing, and no
// player of it is online. The player then joins from another client as if
// the first had never logged in.
func TestGoldenServerStartFails(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_start_fails", serverConfig{brief: true})
	s.out.WriteString("== the game of Golden does not start\n")
	c := s.connectConn(&identityConn{goldenConn: newGoldenConn(), who: goldenIdentity, startErr: errors.New("the client went away")})
	s.out.WriteString("-- Golden\n")
	c.readClosed()
	c.write(c.take(), false)
	c.left()
	s.providerLog()
	s.server()

	s.join("Golden joins from another client", goldenIdentity)
	s.server()
	s.snapshot()
}

// TestGoldenServerQuit records a player's client disconnecting while
// another player sees it: the other is told it left, the server saves it
// and lets it go, and once it joins again it gets back what was saved.
func TestGoldenServerQuit(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_quit", serverConfig{brief: true})
	g := s.join("Golden joins", goldenIdentity)
	v := s.join("Visitor joins", visitorIdentity)
	s.server()
	s.step("Visitor moves and gains experience", func(tx *world.Tx) {
		p := v.player(tx)
		p.Teleport(p.Position().Add(mgl64.Vec3{3, 0, 2}))
		p.SetExperienceLevel(2)
	})

	// The session reads EOF, and the next tick of its world closes it.
	s.out.WriteString("== the client of Visitor disconnects\n")
	_ = v.conn.Close()
	v.left()
	s.awaitSave(visitorIdentity.name)
	s.endStep([]*serverClient{g}, false)
	s.providerLog()
	s.server()

	s.join("Visitor joins again", visitorIdentity)
	s.server()
	s.snapshot()
}

// TestGoldenServerClose records a server closing with two players online:
// each is disconnected with the message of the server and saved, the
// player provider is closed, and Accept ends.
func TestGoldenServerClose(t *testing.T) {
	// Each runs in a process of its own, which waits on the clock of its
	// server most of the time.
	t.Parallel()
	if isolated(t) {
		return
	}
	s := newServerScenario(t, "server_close", serverConfig{brief: true})
	g := s.join("Golden joins", goldenIdentity)
	v := s.join("Visitor joins", visitorIdentity)

	s.out.WriteString("== the server closes\n")
	s.closed = true
	if err := s.srv.Close(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []*serverClient{g, v} {
		fmt.Fprintf(&s.out, "-- %s\n", c.who.name)
		c.readClosed()
		c.write(withoutDepartures(c.take()), true)
		c.left()
	}
	<-s.acceptDone
	s.out.WriteString("-- Accept ended\n")
	// The players are saved in the order the server disconnects them in,
	// that of a map.
	log := s.players.take()
	slices.Sort(log)
	s.out.WriteString("-- player provider, sorted\n")
	for _, line := range log {
		fmt.Fprintf(&s.out, "    %s\n", line)
	}
	s.server()
	s.snapshot()
}

// withoutDepartures returns what a client was sent as the server closed
// without what told it of another player leaving: a removal from the player
// list, the removal of an actor, a message of a player leaving.
func withoutDepartures(pks []proto.Packet) []proto.Packet {
	return slices.DeleteFunc(pks, func(pk proto.Packet) bool {
		switch pk := pk.(type) {
		case *mc.PlayerListPacket:
			return len(pk.Entries) > 0 && pk.Entries[0].ActionType == mc.PlayerListActionRemove
		case *mc.RemoveActorPacket:
			return true
		case *mc.TextPacket:
			return strings.Contains(pk.Message, "multiplayer.player.left")
		}
		return false
	})
}
