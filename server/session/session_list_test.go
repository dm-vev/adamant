package session

import (
	"io"
	"log/slog"
	"runtime"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestSessionListConcurrentAddRemove(t *testing.T) {
	l := new(sessionList)
	existing := testSession("existing")
	joining := testSession("joining")
	l.s = []*Session{existing}

	existing.entityMutex.Lock()
	addDone := make(chan struct{})
	go func() {
		l.Add(joining)
		close(addDone)
	}()
	waitForSessionCount(t, l, 2)
	if l.opMu.TryLock() {
		l.opMu.Unlock()
		t.Fatal("Add released operation ordering before publishing the session")
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

	if _, ok := existing.entityRuntimeIDs[joining.ent]; ok {
		t.Fatal("joining session runtime ID remains after removal")
	}
	if len(l.s) != 1 || l.s[0] != existing {
		t.Fatalf("session list contains removed session: %v", l.s)
	}

	first := (<-existing.packets).(*packet.PlayerList)
	second := (<-existing.packets).(*packet.PlayerList)
	if first.Entries[0].ActionType != protocol.PlayerListActionAdd || second.Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatalf("player list updates out of order: %v then %v", first.Entries[0].ActionType, second.Entries[0].ActionType)
	}
}

func waitForSessionCount(t *testing.T, l *sessionList, count int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		got := len(l.s)
		l.mu.Unlock()
		if got == count {
			return
		}
		runtime.Gosched()
	}
	t.Fatalf("session count did not reach %d", count)
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
	handle := &world.EntityHandle{}
	return &Session{
		conn:                   testConn{identity: login.IdentityData{Identity: uuid.NewString(), DisplayName: name}},
		conf:                   Config{Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		packets:                make(chan packet.Packet, 8),
		closeBackground:        make(chan struct{}),
		ent:                    handle,
		currentEntityRuntimeID: selfEntityRuntimeID,
		entityRuntimeIDs:       map[*world.EntityHandle]uint64{handle: selfEntityRuntimeID},
		entities:               map[uint64]*world.EntityHandle{selfEntityRuntimeID: handle},
	}
}

type testConn struct {
	Conn
	identity login.IdentityData
}

func (c testConn) IdentityData() login.IdentityData { return c.identity }
