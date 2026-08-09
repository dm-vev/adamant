package session

import (
	"io"
	"log/slog"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
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
	existing.currentEntityRuntimeID = 2
	existing.entityRuntimeIDs[joining.ent] = 2
	existing.entities[2] = joining.ent

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

	first := (<-existing.packets).(*packet.PlayerList)
	second := (<-existing.packets).(*packet.PlayerList)
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
