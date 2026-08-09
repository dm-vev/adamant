package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestHideSessionlessControllableTwice(t *testing.T) {
	s := testSession("viewer")
	handle := &world.EntityHandle{}
	entity := testControllable{Controllable: nil, handle: handle}
	const runtimeID = 2
	s.entityRuntimeIDs[handle] = runtimeID
	s.entities[runtimeID] = handle

	s.HideEntity(entity)
	s.HideEntity(entity)

	if _, ok := s.entityRuntimeIDs[handle]; ok {
		t.Fatal("runtime ID remains after hiding sessionless controllable")
	}
	if _, ok := s.entities[runtimeID]; ok {
		t.Fatal("entity remains after hiding sessionless controllable")
	}
	if got := len(s.packets); got != 1 {
		t.Fatalf("got %d remove packets, want 1", got)
	}
	removed := (<-s.packets).(*packet.RemoveActor)
	if removed.EntityUniqueID != runtimeID {
		t.Fatalf("removed runtime ID %d, want %d", removed.EntityUniqueID, runtimeID)
	}
}

type testControllable struct {
	Controllable
	handle *world.EntityHandle
}

func (c testControllable) H() *world.EntityHandle { return c.handle }
