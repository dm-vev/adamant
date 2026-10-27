package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestMovingActorWaitsForParentThroughBlockBacklog(t *testing.T) {
	s := testSession("moving-backlog")
	s.br = world.DefaultBlockRegistry
	s.packets = make(chan packet.Packet, 2048)
	parent, child := cube.Pos{1, 65, 1}, cube.Pos{2, 65, 1}
	for i := 0; i < 400; i++ {
		s.enqueueBlockUpdate(cube.Pos{i, 70, 0}, block.Stone{}, 0)
	}
	s.enqueueBlockUpdate(parent, block.Piston{}, 0)
	s.enqueueBlockUpdate(child, block.MovingBlock{Moving: block.Stone{}}, 0)
	data := map[string]any{"id": "MovingBlock"}
	s.enqueueBlockActorUpdate(child, parent, data)
	s.enqueueBlockActorUpdate(child, parent, data)
	var parentSent, childSent bool
	actors, blocks := 0, 0
	for len(s.blockUpdates) != 0 {
		s.flushBlockUpdates()
		batch := 0
		for len(s.packets) > 0 {
			switch pk := (<-s.packets).(type) {
			case *packet.UpdateBlock:
				batch++
				blocks++
				if pk.Position == (protocol.BlockPos{1, 65, 1}) {
					parentSent = true
				}
				if pk.Position == (protocol.BlockPos{2, 65, 1}) {
					childSent = true
				}
			case *packet.BlockActorData:
				if pk.NBTData["id"] == "MovingBlock" {
					if !parentSent || !childSent {
						t.Fatal("moving actor arrived before its parent or containing block")
					}
					actors++
				}
			}
		}
		if batch > maxBlockUpdatesPerTick {
			t.Fatalf("block update batch exceeds limit: %d", batch)
		}
	}
	if actors != 2 || blocks != 402 {
		t.Fatalf("received %d moving actors and %d blocks, want 2 and 402", actors, blocks)
	}
}

func TestMovingActorReplacementSurvivesFullBacklog(t *testing.T) {
	s := testSession("moving-replaced")
	s.br = world.DefaultBlockRegistry
	pos := cube.Pos{1, 65, 1}
	s.enqueueBlockUpdate(pos, block.MovingBlock{Moving: block.Stone{}}, 0)
	s.enqueueBlockActorUpdate(pos, pos.Side(cube.FaceWest), map[string]any{"id": "MovingBlock"})
	for i := 0; i < maxPendingBlockUpdates-1; i++ {
		s.enqueueBlockUpdate(cube.Pos{i, 70, 0}, block.Stone{}, 0)
	}
	s.enqueueBlockUpdate(pos, block.Air{}, 0)
	update := s.blockUpdates[blockUpdateKey{pos: pos}]
	if len(update.actors) != 0 || update.runtimeID != networkBlockID(s.br, block.Air{}) || s.blockUpdateDrops.Load() != 0 {
		t.Fatal("replacement retained a stale moving actor or was dropped at the backlog limit")
	}
}

func TestMovingActorNotificationDoesNotResendBlock(t *testing.T) {
	s := testSession("moving-actor-only")
	s.enqueueBlockActorUpdate(cube.Pos{2, 65, 1}, cube.Pos{1, 65, 1}, map[string]any{"id": "MovingBlock"})
	s.flushBlockUpdates()
	if len(s.packets) != 1 {
		t.Fatalf("actor-only publication sent %d packets, want 1", len(s.packets))
	}
	if _, ok := (<-s.packets).(*packet.BlockActorData); !ok {
		t.Fatal("actor-only publication sent a block replacement")
	}
}
