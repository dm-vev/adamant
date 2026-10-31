package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestBellSupportSequenceCrossesBatchBoundary(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprintf("remove%t", remove), func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			s := testSession("bell-support-batch")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 2048)
			pos := cube.Pos{4, 65, 4}
			loader := world.NewLoader(2, w, &bellPacketViewer{session: s})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			bell := (block.Bell{Attach: block.SideBellAttachment(), Facing: cube.South}).DecodeNBT(map[string]any{"Direction": int32(255)}).(block.Bell)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, pos.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(pos.Side(cube.FaceNorth), block.Stone{}, nil)
				tx.SetBlock(pos, bell, nil)
			})
			s.flushBlockUpdates()
			for len(s.packets) > 0 {
				<-s.packets
			}
			const changes = 140
			<-w.Exec(func(tx *world.Tx) {
				for i := range changes {
					var support world.Block = block.Stone{}
					if i%2 == 1 {
						support = block.Air{}
					}
					tx.SetBlock(pos.Side(cube.FaceSouth), support, nil)
				}
				if remove {
					tx.SetBlock(pos, nil, nil)
				}
			})
			states, actors, batches := 0, 0, 0
			position := protocol.BlockPos{4, 65, 4}
			for len(s.blockUpdates) > 0 {
				batches++
				if batches > 4 {
					t.Fatal("support sequence did not drain")
				}
				s.flushBlockUpdates()
				blocks := 0
				for len(s.packets) > 0 {
					switch p := (<-s.packets).(type) {
					case *packet.UpdateBlock:
						blocks++
						if p.Position != position {
							continue
						}
						want := bell
						if states%2 == 0 {
							want.Attach = block.MultipleBellAttachment()
						}
						id := networkBlockID(s.br, want)
						if states == changes && remove {
							id = networkBlockID(s.br, block.Air{})
							if actors != changes {
								t.Fatal("removal preceded support actor notifications")
							}
						} else if actors != 0 {
							t.Fatal("support actor preceded an intermediate state")
						}
						if p.Flags != 3 || p.NewBlockRuntimeID != id {
							t.Fatalf("state %d: flags=%d runtimeID=%d, want 3/%d", states, p.Flags, p.NewBlockRuntimeID, id)
						}
						states++
					case *packet.BlockActorData:
						if p.Position == position {
							if states != changes || p.NBTData["Ticks"] != int32(0) || p.NBTData["Direction"] != int32(255) {
								t.Fatal("support actor lost its clock, direction or position in the sequence")
							}
							actors++
						}
					}
				}
				if blocks > maxBlockUpdatesPerTick {
					t.Fatalf("batch contains %d block updates", blocks)
				}
			}
			wantStates := changes
			if remove {
				wantStates++
			}
			if states != wantStates || actors != changes || batches < 2 || s.blockUpdateDrops.Load() != 0 {
				t.Fatalf("support sequence states=%d actors=%d batches=%d drops=%d", states, actors, batches, s.blockUpdateDrops.Load())
			}
		})
	}
}

func TestBellRemovalMarkerSurvivesInterleavedFlush(t *testing.T) {
	s := testSession("bell-removal-marker")
	s.br = world.DefaultBlockRegistry
	pos := cube.Pos{4, 65, 4}
	s.ViewBlockAction(pos, block.BellRemovalAction{})
	s.flushBlockUpdates()
	if len(s.packets) != 0 || len(s.blockUpdates) != 1 {
		t.Fatal("flush consumed the removal marker before the foreground replacement")
	}
	s.ViewBlockUpdate(pos, block.Air{}, 0)
	s.flushBlockUpdates()
	if len(s.packets) != 1 {
		t.Fatal("removal did not publish exactly one state")
	}
	p, ok := (<-s.packets).(*packet.UpdateBlock)
	if !ok || p.Flags != 3 || p.NewBlockRuntimeID != networkBlockID(s.br, block.Air{}) || len(s.blockUpdates) != 0 {
		t.Fatal("interleaved flush lost the bell removal flags")
	}
}

func TestBellRepeatedUsesRetainSoundAndAnimationThroughBacklog(t *testing.T) {
	s := testSession("bell-use-backlog")
	s.br = world.DefaultBlockRegistry
	s.packets = make(chan packet.Packet, 2048)
	for i := 0; i < 400; i++ {
		s.enqueueBlockUpdate(cube.Pos{i, 70, 0}, block.Stone{}, 0)
	}
	pos := cube.Pos{4, 65, 4}
	bell := (block.Bell{Attach: block.StandingBellAttachment(), Facing: cube.South, Powered: true}).DecodeNBT(map[string]any{"Ringing": uint8(1), "Ticks": int32(0), "Direction": int32(0)}).(block.Bell)
	for range 2 {
		s.ViewBlockUpdate(pos, bell, 0)
		s.ViewSound(pos.Vec3(), sound.BellRing{AfterBlock: true})
		bell.Powered = !bell.Powered
	}
	// A subsequent rejected use confirms that same latest permutation. It
	// must not erase effects or animation still waiting behind the backlog.
	bell.Powered = !bell.Powered
	s.ViewBlockAction(pos, block.BellUseRejectedAction{Bell: bell})
	state, actors, sounds := false, 0, 0
	for len(s.blockUpdates) > 0 {
		s.flushBlockUpdates()
		for len(s.packets) > 0 {
			switch p := (<-s.packets).(type) {
			case *packet.UpdateBlock:
				if p.Position == (protocol.BlockPos{4, 65, 4}) {
					if p.Flags != 3 || p.NewBlockRuntimeID != networkBlockID(s.br, bell) {
						t.Fatal("bell confirmation lost the latest state")
					}
					state = true
				}
			case *packet.BlockActorData:
				if p.Position == (protocol.BlockPos{4, 65, 4}) {
					if !state || p.NBTData["Ringing"] != uint8(1) || p.NBTData["Ticks"] != int32(0) {
						t.Fatal("bell animation lost its state/animation origin")
					}
					actors++
				}
			case *packet.LevelSoundEvent:
				if !state || actors != 1 || p.SoundType != packet.SoundEventBell {
					t.Fatal("bell sound preceded its state/actor publication")
				}
				sounds++
			}
		}
	}
	if !state || actors != 1 || sounds != 2 {
		t.Fatalf("pending bell effects: state=%t actors=%d sounds=%d, want true/1/2", state, actors, sounds)
	}
}

func TestBellActorPairSurvivesBlockBacklog(t *testing.T) {
	s := testSession("bell-backlog")
	s.br = world.DefaultBlockRegistry
	s.packets = make(chan packet.Packet, 2048)
	for i := 0; i < 400; i++ {
		s.enqueueBlockUpdate(cube.Pos{i, 70, 0}, block.Stone{}, 0)
	}
	pos := cube.Pos{4, 65, 4}
	bell := (block.Bell{Attach: block.StandingBellAttachment(), Facing: cube.South, Powered: true}).DecodeNBT(map[string]any{"Ringing": uint8(1), "Ticks": int32(0), "Direction": int32(0)}).(block.Bell)
	s.enqueueBlockUpdate(pos, bell, 0)
	s.enqueueBellRingUpdate(pos, bell)
	var flags []uint32
	actors := 0
	for len(s.blockUpdates) > 0 {
		s.flushBlockUpdates()
		for len(s.packets) > 0 {
			switch p := (<-s.packets).(type) {
			case *packet.UpdateBlock:
				if p.Position == (protocol.BlockPos{4, 65, 4}) {
					flags = append(flags, p.Flags)
				}
			case *packet.BlockActorData:
				if p.Position == (protocol.BlockPos{4, 65, 4}) {
					if len(flags) != 2 || p.NBTData["Ticks"] != int32(0) || p.NBTData["Ringing"] != uint8(1) {
						t.Fatal("bell actor preceded its state pair or lost animation origin")
					}
					actors++
				}
			}
		}
	}
	if len(flags) != 2 || flags[0] != 3 || flags[1] != 1 || actors != 2 {
		t.Fatalf("bell backlog updates=%v, actors=%d", flags, actors)
	}
}

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
