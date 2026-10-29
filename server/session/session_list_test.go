package session

import (
	"io"
	"log/slog"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/bedrock/login"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

func TestSessionListConcurrentAddRemove(t *testing.T) {
	l := new(sessionList)
	existing := testSession("existing")
	joining := testSession("joining")
	l.s = []*Session{existing}
	existing.entityRuntimeIDs[joining.ent] = joining.ent.RuntimeID()
	existing.entities[joining.ent.RuntimeID()] = joining.ent

	existing.entityMutex.Lock()
	addDone := make(chan struct{})
	go func() {
		l.Add(joining)
		close(addDone)
	}()
	waitForSessionOperation(t, l)
	if _, ok := l.Lookup(joining.ent.UUID()); ok {
		existing.entityMutex.Unlock()
		waitDone(t, addDone)
		t.Fatal("Add published session before installing runtime IDs")
	}

	removeStarted := make(chan struct{})
	removeDone := make(chan struct{})
	go func() {
		close(removeStarted)
		l.Remove(joining, nil)
		close(removeDone)
	}()
	<-removeStarted
	existing.entityMutex.Unlock()

	waitDone(t, addDone)
	waitDone(t, removeDone)

	existing.entityMutex.Lock()
	_, mapped := existing.entityRuntimeIDs[joining.ent]
	reverseCount := len(existing.entities)
	existing.entityMutex.Unlock()
	if mapped {
		t.Fatal("joining session runtime ID remains after removal")
	}
	if reverseCount != 1 {
		t.Fatalf("runtime ID reverse map contains %d entries after removal, want 1", reverseCount)
	}
	l.mu.Lock()
	remaining := slices.Clone(l.s)
	l.mu.Unlock()
	if len(remaining) != 1 || remaining[0] != existing {
		t.Fatalf("session list contains removed session: %v", remaining)
	}

	first := packetOf(existing.testPacket()).(*v2193.PlayerListPacket)
	second := packetOf(existing.testPacket()).(*v2193.PlayerListPacket)
	if first.Entries[0].ActionType != protocol.PlayerListActionAdd || second.Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("player list updates out of order: %v then %v", first.Entries[0].ActionType, second.Entries[0].ActionType)
	}
}

func waitForSessionOperation(t *testing.T, l *sessionList) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !l.opMu.TryLock() {
			return
		}
		l.opMu.Unlock()
		runtime.Gosched()
	}
	t.Fatal("session list operation did not start")
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("concurrent session list operation did not finish")
	}
}

func testSession(name string) *Session {
	handle := entity.NewText(name, mgl64.Vec3{})
	return &Session{
		conn:             testConn{identity: login.Identity{Identity: uuid.NewString(), DisplayName: name}},
		conf:             Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		holdPackets:      1,
		ent:              handle,
		entityRuntimeIDs: map[*world.EntityHandle]uint64{handle: selfEntityRuntimeID},
		entities:         map[uint64]*world.EntityHandle{selfEntityRuntimeID: handle},
	}
}

// testPacket takes the next packet queued for a session of a test, which
// holds what it is given for the test to take: nil if none was queued.
func (s *Session) testPacket() proto.Packet {
	s.packetsMu.Lock()
	defer s.packetsMu.Unlock()
	for _, queue := range []*[]proto.Packet{&s.heldPackets, &s.pendingPackets} {
		if len(*queue) > 0 {
			pk := (*queue)[0]
			*queue = (*queue)[1:]
			return pk
		}
	}
	return nil
}

type testConn struct {
	Conn
	identity login.Identity
}

func (c testConn) IdentityData() login.Identity { return c.identity }

// packetOf returns the packet a session was given, unwrapping one shared
// with other sessions.
func packetOf(pk proto.Packet) proto.Packet {
	if s, ok := pk.(*sharedPacket); ok {
		return s.Packet
	}
	return pk
}

// TestPlayerListEntryIsShared adds a session to a list: every other session
// is given the same PlayerList packet of it, encoded once, and the new one
// the entries of the others.
func TestPlayerListEntryIsShared(t *testing.T) {
	l := new(sessionList)
	a, b, c := testSession("a"), testSession("b"), testSession("c")
	sk := skin.New(64, 64)
	sk.PlayFabID = "c-skin"
	c.listEntry.Store(c.newListEntry(sk))
	l.s = []*Session{a, b}
	l.Add(c)
	pa, pb := a.testPacket(), b.testPacket()
	if pa != pb || pa != proto.Packet(c.listEntry.Load()) {
		t.Fatal("the sessions were not given the same entry of the new session")
	}
	if e := packetOf(pa).(*v2193.PlayerListPacket).Entries[0]; e.UUID != c.ent.UUID() || e.Skin.PlayFabID != "c-skin" {
		t.Fatalf("entry %+v", e)
	}
	seen := map[uuid.UUID]bool{}
	for range 3 {
		seen[packetOf(c.testPacket()).(*v2193.PlayerListPacket).Entries[0].UUID] = true
	}
	if !seen[a.ent.UUID()] || !seen[b.ent.UUID()] || !seen[c.ent.UUID()] {
		t.Fatalf("the new session listed %v", seen)
	}
}

// TestShowSkinReachesEverySession changes the skin of a player: every
// session is given one PlayerSkin of it, whether it sees the player or
// not, and sessions that join later list the player with the new skin.
func TestShowSkinReachesEverySession(t *testing.T) {
	a, b, c := testSession("a"), testSession("b"), testSession("c")
	sessions.mu.Lock()
	old := sessions.s
	sessions.s = []*Session{a, b, c}
	sessions.mu.Unlock()
	t.Cleanup(func() {
		sessions.mu.Lock()
		sessions.s = old
		sessions.mu.Unlock()
	})
	sk := skin.New(64, 64)
	sk.PlayFabID = "new-skin"
	a.ShowSkin(sk)
	first := a.testPacket()
	for _, s := range []*Session{b, c} {
		if pk := s.testPacket(); pk != first {
			t.Fatal("the sessions were not given the same PlayerSkin")
		}
	}
	if pk, ok := packetOf(first).(*v2193.PlayerSkinPacket); !ok || pk.UUID != a.ent.UUID() || pk.Skin.PlayFabID != "new-skin" {
		t.Fatalf("skin packet %+v", packetOf(first))
	}
	if e := a.playerListEntry().Packet.(*v2193.PlayerListPacket).Entries[0]; e.Skin.PlayFabID != "new-skin" {
		t.Fatalf("later sessions would list the player with %q", e.Skin.PlayFabID)
	}
}
