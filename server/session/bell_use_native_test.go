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
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type bellNativeUseUser struct {
	item.User
	rotation cube.Rotation
}

func (u bellNativeUseUser) Rotation() cube.Rotation { return u.rotation }

func TestBellPlayerUseMatchesNativeActorsAndPackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBellInteractionNative(t, split, false) })
	}
}

func TestBellArrowEffectsMatchNativePackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBellInteractionNative(t, split, true) })
	}
}

func testBellInteractionNative(t *testing.T, split, projectile bool) {
	t.Helper()
	file, snapshots, packets, successes := "bedrock_1_26_51_bell_use.json", 2928, 272, 40
	if projectile {
		file, snapshots, packets, successes = "bedrock_1_26_51_bell_projectile_packets.json", 5120, 320, 64
	}
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Readback  struct{ Snapshots int } `json:"actor_readback"`
		Scenarios []struct {
			Scenario struct {
				Attachment          string
				Direction, Approach int
			}
			Base      cube.Pos
			SetupTick int64 `json:"setup_tick"`
			Changes   []struct {
				Tick     int64
				GameTick int64 `json:"game_tick"`
				Rotation struct{ X, Y float64 }
			}
			Results []struct{ Tick int64 }
			Spawn   struct {
				ID                 string
				Position, Velocity struct{ X, Y, Z float64 }
			}
			Hits    []struct{ Tick int64 }
			Samples []struct {
				Tick          int64
				GameTick      int64 `json:"game_tick"`
				Source, Block struct {
					ID     string
					States map[string]any
				}
				Actor map[string]any
			}
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
	if !native.Complete || len(native.Scenarios) != 64 || native.Readback.Snapshots != snapshots {
		t.Fatal("incomplete native bell player corpus")
	}
	totalPackets, accepted := 0, 0
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		totalPackets += len(trace.Packets)
		accepted += len(trace.Results)
		if projectile {
			accepted += len(trace.Hits)
		}
		t.Run(fmt.Sprintf("%s/direction%d/approach%d", c.Attachment, c.Direction, c.Approach), func(t *testing.T) {
			if len(trace.Changes) != 1 || len(trace.Results) > 1 || len(trace.Samples) < 40 {
				t.Fatal("invalid native bell use controls")
			}
			if projectile && (len(trace.Hits) != 1 || trace.Spawn.ID != "minecraft:arrow" || len(trace.Samples) != 80) {
				t.Fatal("invalid native bell arrow packet controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = trace.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-bell-use")
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
						if v.Position == vec64To32(trace.Base.Vec3()) {
							got = append(got, p)
						}
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlock(trace.Base.Side(cube.FaceDown), block.Stone{}, nil)
				b, ok := tx.World().BlockRegistry().BlockByName("minecraft:bell", map[string]any{"attachment": c.Attachment, "direction": int32(c.Direction), "toggle_bit": uint8(0)})
				if !ok {
					t.Fatal("native bell use permutation missing")
				}
				bell := b.(block.Bell)
				if c.Attachment == "hanging" {
					tx.SetBlock(trace.Base.Side(cube.FaceUp), block.Stone{}, nil)
				}
				if c.Attachment == "side" || c.Attachment == "multiple" {
					tx.SetBlock(trace.Base.Side(bell.Facing.Face().Opposite()), block.Stone{}, nil)
					if c.Attachment == "multiple" {
						tx.SetBlock(trace.Base.Side(bell.Facing.Face()), block.Stone{}, nil)
					}
				}
				tx.SetBlock(trace.Base, bell, nil)
			})
			collect()
			applied := 0
			for _, f := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native bell player clock differs")
					}
					bell, ok := tx.Block(trace.Base).(block.Bell)
					if !ok {
						t.Fatal("bell lost its native support")
					}
					name, states := bell.EncodeBlock()
					// The native JSON uses booleans for byte-valued properties.
					states["toggle_bit"] = bell.Powered
					source := f.Source
					if projectile {
						source = f.Block
					}
					g, _ := json.Marshal(states)
					v, _ := json.Marshal(source.States)
					if name != source.ID || !bytes.Equal(g, v) {
						t.Fatalf("tick %d bell state=%s, native=%s", f.Tick, g, v)
					}
					g, _ = json.Marshal(clientActorNBT(bell.EncodeNBT(), trace.Base))
					v, _ = json.Marshal(f.Actor)
					if !bytes.Equal(g, v) {
						t.Fatalf("tick %d bell actor=%s, native=%s", f.Tick, g, v)
					}
					for _, change := range trace.Changes {
						if projectile {
							if change.Tick == f.Tick {
								opts := world.EntitySpawnOpts{Position: mgl64.Vec3{trace.Spawn.Position.X, trace.Spawn.Position.Y, trace.Spawn.Position.Z}, Velocity: mgl64.Vec3{trace.Spawn.Velocity.X, trace.Spawn.Velocity.Y, trace.Spawn.Velocity.Z}}
								conf := entity.ProjectileBehaviourConfig{Gravity: .05, Drag: .01, Damage: 2, Sound: sound.ArrowHit{}, SurviveBlockCollision: true, DisablePickup: true}
								tx.AddEntity(opts.New(entity.ArrowType, conf))
								applied++
							}
							continue
						}
						if change.GameTick == f.GameTick {
							user := bellNativeUseUser{rotation: cube.Rotation{change.Rotation.Y, change.Rotation.X}}
							faces := [4]cube.Face{cube.FaceSouth, cube.FaceWest, cube.FaceNorth, cube.FaceEast}
							result := false
							tx.WithBlockInteraction(func() { result = bell.Activate(trace.Base, faces[c.Approach], tx, user, &item.UseContext{}) })
							if result != (len(trace.Results) == 1) {
								t.Fatal("bell use acceptance differs from BDS")
							}
							if !result {
								s.ViewBlockAction(trace.Base, block.BellUseRejectedAction{Bell: bell})
							}
							applied++
						}
					}
				})
				collect()
				if t.Failed() {
					break
				}
			}
			if applied != 1 {
				t.Fatal("native bell player input was not replayed exactly once")
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
					t.Fatal("unknown native bell use packet")
				}
			}
			g, _ := json.Marshal(got)
			v, _ := json.Marshal(want)
			if !bytes.Equal(g, v) {
				t.Fatalf("native bell player packets differ\ngot: %s\nnative: %s", g, v)
			}
		})
	}
	if totalPackets != packets || accepted != successes {
		t.Fatal("native bell use positive/negative controls changed")
	}
}
