package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/dm-vev/adamant/net/bedrock/proto"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type bellPacketViewer struct {
	world.NopViewer
	session *Session
	split   bool
}

func (v *bellPacketViewer) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	v.session.ViewBlockUpdate(pos, b, layer)
	if v.split {
		v.session.flushBlockUpdates()
	}
}
func (v *bellPacketViewer) ViewBlockAction(pos cube.Pos, a world.BlockAction) {
	v.session.ViewBlockAction(pos, a)
}
func (v *bellPacketViewer) ViewSound(pos mgl64.Vec3, s world.Sound) { v.session.ViewSound(pos, s) }

func TestBellEffectsMatchNativePackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBellEffectsNativePackets(t, split) })
	}
}

func testBellEffectsNativePackets(t *testing.T, split bool) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_bell_packets.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Readback  struct{ Snapshots int } `json:"actor_readback"`
		Scenarios []struct {
			Scenario struct {
				Direction, Phase int
				Conducted        bool
				Edges            []int
			}
			Base, Source, Support cube.Pos
			SetupTick             int64 `json:"setup_tick"`
			Changes               []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct{ Tick int64 }
			Packets []struct {
				Update *v2193.UpdateBlockPacket    `json:"block_update"`
				Actor  *v2193.BlockActorDataPacket `json:"block_actor"`
				Sound  *v2193.LevelSoundEventPacket
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 32 || native.Readback.Snapshots != 3584 {
		t.Fatal("incomplete native bell packet corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("direction%d/conducted%t/phase%d/%v", c.Direction, c.Conducted, c.Phase, c.Edges), func(t *testing.T) {
			if len(trace.Samples) != 112 || len(trace.Changes) != len(c.Edges) || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native bell packet controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			settings.WeatherCycle = false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-bell")
			s.br = world.DefaultBlockRegistry
			s.heldPackets = nil
			loader := world.NewLoader(2, w, &bellPacketViewer{session: s, split: split})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			var got []proto.Packet
			position := v2193.BlockPos{int32(trace.Base[0]), int32(trace.Base[1]), int32(trace.Base[2])}
			collect := func() {
				s.flushBlockUpdates()
				for len(s.heldPackets) > 0 {
					p := s.testPacket()
					switch v := p.(type) {
					case *v2193.UpdateBlockPacket:
						if v.Position == position && v.Layer == 0 {
							got = append(got, p)
						}
					case *v2193.BlockActorDataPacket:
						if v.Position == position {
							got = append(got, p)
						}
					case *v2193.LevelSoundEventPacket:
						got = append(got, p)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(trace.Base.Side(cube.FaceDown), block.Stone{}, nil)
				bell, ok := tx.World().BlockRegistry().BlockByName("minecraft:bell", map[string]any{"attachment": "standing", "direction": int32(c.Direction), "toggle_bit": uint8(0)})
				if !ok {
					t.Fatal("native bell permutation missing")
				}
				tx.SetBlock(trace.Base, bell, nil)
				if c.Conducted {
					tx.SetBlock(trace.Support, block.Stone{}, nil)
				}
			})
			collect()
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native bell packet clock differs")
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if c.Conducted {
								tx.SetBlock(trace.Source, nil, nil)
								source = block.Button{Type: block.StoneButton(), Facing: cube.FaceWest, Pressed: change.Powered}
							} else if change.Powered {
								source = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, source, nil)
						}
					}
				})
				collect()
			}
			var want []proto.Packet
			for _, p := range trace.Packets {
				switch {
				case p.Update != nil:
					want = append(want, p.Update)
				case p.Actor != nil:
					want = append(want, p.Actor)
				case p.Sound != nil:
					want = append(want, p.Sound)
				default:
					t.Fatal("invalid native bell packet")
				}
			}
			// JSON normalises NBT byte/int tags from Go and independent JSON
			// readback while retaining every field, packet, duplicate and order.
			g, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			v, err := json.Marshal(want)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(g, v) {
				t.Fatalf("bell packets differ\ngot: %s\nnative: %s", g, v)
			}
		})
	}
}
