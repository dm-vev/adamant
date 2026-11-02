package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// These traces contain real BDS player transactions, rather than scripted
// changes to open_bit. The SDK's input game time and interval game time are
// both retained: the Script event tick alone is not the simulation clock.
func TestBarrierPlayerUseMatchesNativeStatesAndPackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBarrierPlayerUseNative(t, split) })
	}
}

func testBarrierPlayerUseNative(t *testing.T, split bool) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_barrier_use.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Form, Feed string
				Top, Initial      bool
				Approach          int
			}
			Base, Target       cube.Pos
			Positions, Sources []cube.Pos
			Initial            []windPacketNativeBlock
			InitialSources     []windPacketNativeBlock `json:"initial_sources"`
			SetupTick          int64                   `json:"setup_tick"`
			Requests           []struct{ Tick int64 }
			Changes            []struct {
				GameTick int64 `json:"game_tick"`
				Blocks   []windPacketNativeBlock
				Rotation struct{ X, Y float64 }
			}
			Results []struct {
				GameTick int64 `json:"game_tick"`
				Blocks   []windPacketNativeBlock
			}
			Samples []struct {
				Tick            int64
				GameTick        int64 `json:"game_tick"`
				Blocks, Sources []windPacketNativeBlock
			}
			Checkpoint struct {
				Sequence uint64 `json:"packet_sequence"`
				Marker   struct{ Tick int64 }
			}
			Effects []struct {
				Update   *packet.UpdateBlock `json:"block_update"`
				Sound    *packet.LevelSoundEvent
				Sequence uint64 `json:"packet_sequence"`
			} `json:"effect_packets"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 136 {
		t.Fatal("incomplete native barrier player-use corpus")
	}
	world.DefaultBlockRegistry.Finalize()
	blocks := map[string]world.Block{}
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		id, states := b.EncodeBlock()
		blocks[windPacketBlockKey(t, windPacketNativeBlock{ID: id, States: states})] = b
	}
	decode := func(b windPacketNativeBlock) world.Block {
		t.Helper()
		v, ok := blocks[windPacketBlockKey(t, b)]
		if !ok {
			t.Fatalf("native player-use permutation unavailable: %+v", b)
		}
		return v
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/top%t/initial%t/%s/approach%d/tick%d", c.Scenario.Block, c.Scenario.Top, c.Scenario.Initial, c.Scenario.Feed, c.Scenario.Approach, c.SetupTick), func(t *testing.T) {
			iron := strings.HasPrefix(c.Scenario.Block, "iron_")
			accepted := 2
			if iron {
				accepted = 0
			}
			if len(c.Changes) != 2 || len(c.Requests) != 2 || len(c.Results) != accepted || len(c.Samples) < 38 || len(c.Positions) != len(c.Initial) || len(c.Sources) != 2 || len(c.InitialSources) != 2 || c.Checkpoint.Marker.Tick != c.SetupTick+12 || c.Checkpoint.Sequence == 0 {
				t.Fatal("native player-use controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-barrier-use")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 1024)
			viewer := &barrierPacketViewer{bellPacketViewer: &bellPacketViewer{session: s, split: split}, t: t, base: c.Base}
			loader := world.NewLoader(2, w, viewer)
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			compare := func(tx *world.Tx, want []windPacketNativeBlock) {
				t.Helper()
				if len(want) != len(c.Positions) {
					t.Fatal("native player-use frame size differs")
				}
				for i, pos := range c.Positions {
					id, states := tx.Block(pos).EncodeBlock()
					g, v := windPacketBlockKey(t, windPacketNativeBlock{ID: id, States: states}), windPacketBlockKey(t, want[i])
					if g != v {
						t.Errorf("tick %d at %v: got %s, native %s", tx.CurrentTick(), pos, g, v)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 2; x <= c.Base[0]+2; x++ {
					for z := c.Base[2] - 2; z <= c.Base[2]+2; z++ {
						for y := 63; y <= 64; y++ {
							tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, nil)
						}
					}
				}
				for i, pos := range c.Sources {
					tx.SetBlock(pos, decode(c.InitialSources[i]), nil)
				}
				for i, pos := range c.Positions {
					tx.SetBlock(pos, decode(c.Initial[i]), nil)
				}
			})
			var got []packet.Packet
			collect := func() {
				s.flushBlockUpdates()
				for len(s.packets) > 0 {
					p := <-s.packets
					var pos cube.Pos
					switch p := p.(type) {
					case *packet.UpdateBlock:
						if p.Layer != 0 {
							continue
						}
						pos = cube.Pos{int(p.Position[0]), int(p.Position[1]), int(p.Position[2])}
					case *packet.LevelSoundEvent:
						pos = cube.PosFromVec3(mgl64.Vec3{float64(p.Position[0]), float64(p.Position[1]), float64(p.Position[2])})
					default:
						continue
					}
					for _, cell := range c.Positions {
						if pos == cell {
							got = append(got, p)
							break
						}
					}
				}
			}
			collect()
			got = nil
			applied := 0
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native player-use clock differs")
					}
					compare(tx, f.Blocks)
					for _, change := range c.Changes {
						if change.GameTick != f.GameTick {
							continue
						}
						compare(tx, change.Blocks)
						act, ok := tx.Block(c.Target).(block.Activatable)
						if ok == iron {
							t.Fatal("native iron/manual activation eligibility differs")
						}
						if ok {
							face := cube.FaceSouth
							if c.Scenario.Approach == 2 {
								face = cube.FaceNorth
							}
							user := bellNativeUseUser{rotation: cube.Rotation{change.Rotation.Y, change.Rotation.X}}
							var activated bool
							tx.WithBlockInteraction(func() { activated = act.Activate(c.Target, face, tx, user, &item.UseContext{}) })
							if !activated {
								t.Fatal("native accepted player use was rejected")
							}
							compare(tx, c.Results[applied].Blocks)
						} else {
							rejected := tx.Block(c.Target).(interface {
								RejectedUseUpdates(cube.Pos, *world.Tx, func(cube.Pos, world.Block))
							})
							rejected.RejectedUseUpdates(c.Target, tx, func(pos cube.Pos, b world.Block) {
								s.ViewBlockUpdate(pos, b, 0)
								if split {
									s.flushBlockUpdates()
								}
							})
							compare(tx, change.Blocks)
						}
						applied++
					}
				})
				collect()
				if f.Tick <= c.Checkpoint.Marker.Tick {
					got = nil
				}
				if t.Failed() {
					break
				}
			}
			if t.Failed() {
				return
			}
			if applied != 2 {
				t.Fatal("native player-use inputs were not replayed exactly once")
			}
			var want []packet.Packet
			for _, p := range c.Effects {
				if p.Sequence <= c.Checkpoint.Sequence {
					t.Fatal("native setup packet entered interaction scope")
				}
				if p.Update != nil {
					want = append(want, p.Update)
				} else if p.Sound != nil {
					want = append(want, p.Sound)
				} else {
					t.Fatal("unknown native player-use packet")
				}
			}
			g, _ := json.Marshal(got)
			v, _ := json.Marshal(want)
			if !bytes.Equal(g, v) {
				t.Fatalf("native player-use packets differ\ngot: %s\nnative: %s", g, v)
			}
		})
	}
}
