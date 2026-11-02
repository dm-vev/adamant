package session_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type barrierPlacementPacketViewer struct {
	world.NopViewer
	s     *session.Session
	split bool
}

func (v barrierPlacementPacketViewer) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	v.s.ViewBlockUpdate(pos, b, layer)
	if v.split {
		session.FlushNativeTestBlockUpdates(v.s)
	}
}
func (v barrierPlacementPacketViewer) ViewSound(pos mgl64.Vec3, s world.Sound) { v.s.ViewSound(pos, s) }

type barrierPlacementWirePacket struct {
	Update *packet.UpdateBlock `json:"block_update"`
	Sound  *packet.LevelSoundEvent
	Clock  *barrierPlacementPhaseMarker `json:"clock"`
}

func TestBarrierPlacementPacketsMatchBDS(t *testing.T) {
	for _, observer := range []bool{false, true} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("observer%t/split%t", observer, split), func(t *testing.T) { testBarrierPlacementPackets(t, observer, split, false) })
		}
	}
}

func testBarrierPlacementPackets(t *testing.T, observer, split, clock bool) {
	file, count, effects := "bedrock_1_26_51_barrier_placement.json", 112, 544
	if observer {
		file, count, effects = "bedrock_1_26_51_barrier_placement_observer.json", 28, 136
	}
	if clock {
		file = "bedrock_1_26_51_barrier_placement_clock.json"
	}
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete      bool
		PlacerClock   []barrierPlacementClockPacket `json:"placer_clock_packets"`
		ObserverClock []barrierPlacementClockPacket `json:"observer_clock_packets"`
		Scenarios     []struct {
			Scenario struct {
				Block     string
				Direction int
				Powered   bool
			}
			Base, Support cube.Pos
			Positions     []cube.Pos
			Feet          mgl64.Vec3
			SetupTick     int64 `json:"setup_tick"`
			Changes       []struct {
				GameTick int64 `json:"game_tick"`
				Rotation struct{ X, Y float64 }
			}
			Samples []struct {
				Tick     int64
				GameTick int64 `json:"game_tick"`
				Blocks   []barrierPlacementNativeState
			}
			Effects         []barrierPlacementWirePacket `json:"effect_packets"`
			ObserverEffects []barrierPlacementWirePacket `json:"observer_effect_packets"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native placement packet corpus")
	}
	if clock {
		validatePlacementPhaseClock(t, native.PlacerClock)
		validatePlacementPhaseClock(t, native.ObserverClock)
	}
	packetCount, observerCount := 0, 0
	for index, c := range native.Scenarios {
		packetCount += len(c.Effects)
		observerCount += len(c.ObserverEffects)
		t.Run(fmt.Sprintf("%s/direction%d/powered%t/tick%d", c.Scenario.Block, c.Scenario.Direction, c.Scenario.Powered, c.SetupTick), func(t *testing.T) {
			if len(c.Changes) != 1 || len(c.Samples) < 36 {
				t.Fatal("native actual placement clock controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			log := slog.New(slog.NewTextHandler(io.Discard, nil))
			connections := []*packetConn{{packets: make(chan packet.Packet, 4096)}}
			sessions := []*session.Session{session.Config{Log: log, MaxChunkRadius: 2}.New(connections[0])}
			if observer {
				connections = append(connections, &packetConn{packets: make(chan packet.Packet, 4096)})
				sessions = append(sessions, session.Config{Log: log, MaxChunkRadius: 2}.New(connections[1]))
			}
			var loaders []*world.Loader
			defer func() {
				for _, s := range sessions {
					session.CloseNativeTestSession(s)
				}
				<-w.Exec(func(tx *world.Tx) {
					for _, l := range loaders {
						l.Close(tx)
					}
				})
				_ = w.Close()
			}()
			var handle *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				for _, s := range sessions {
					l := world.NewLoader(2, w, barrierPlacementPacketViewer{s: s, split: split})
					loaders = append(loaders, l)
					l.Move(tx, c.Base.Vec3Centre())
					l.Load(tx, 25)
				}
				for x := c.Base[0] - 2; x <= c.Base[0]+2; x++ {
					for z := c.Base[2] - 2; z <= c.Base[2]+2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				if c.Scenario.Powered {
					tx.SetBlock(c.Support, block.RedstoneBlock{}, nil)
				}
				change := c.Changes[0]
				cfg := player.Config{Name: "RedstoneAudit", UUID: uuid.New(), Position: c.Feet, Rotation: cube.Rotation{change.Rotation.Y, change.Rotation.X}, GameMode: world.GameModeSurvival, Session: sessions[0]}
				handle = world.EntitySpawnOpts{Position: c.Feet, ID: cfg.UUID}.New(player.Type, cfg)
				sessions[0].SetHandle(handle, cfg.Skin)
				p := tx.AddEntity(handle).(*player.Player)
				held, ok := world.ItemByName("minecraft:"+c.Scenario.Block, 0)
				if !ok {
					t.Fatal("native placement item missing")
				}
				p.SetHeldItems(item.NewStack(held, 2), item.Stack{})
			})
			got := make([][]packet.Packet, len(sessions))
			collect := func(tick int64) {
				t.Helper()
				for i, s := range sessions {
					session.FlushNativeTestBlockUpdates(s)
					marker := int32(-1000000 - tick)
					s.ViewTime(int(marker))
					timer := time.NewTimer(5 * time.Second)
					done := false
					for !done {
						select {
						case p := <-connections[i].packets:
							var pos cube.Pos
							switch p := p.(type) {
							case *packet.SetTime:
								if p.Time == marker {
									done = true
								}
								continue
							case *packet.Text:
								if clock {
									got[i] = append(got[i], p)
								}
								continue
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
									got[i] = append(got[i], p)
									break
								}
							}
						case <-timer.C:
							t.Fatal("actual placement session sender did not drain to marker")
						}
					}
					timer.Stop()
				}
			}
			collect(c.SetupTick)
			for i := range got {
				got[i] = nil
			}
			if c.Scenario.Block == "wooden_door" && c.Scenario.Direction == 0 && !c.Scenario.Powered {
				<-w.Exec(func(tx *world.Tx) {
					e, _ := handle.Entity(tx)
					if err := session.ReplayNativeTestBlockUse(sessions[0], tx, e.(session.Controllable), c.Support, 1); err != nil {
						t.Error(err)
					}
					if _, air := tx.Block(c.Base).(block.Air); !air {
						t.Error("mismatched client-held stack placed a block")
					}
				})
				collect(c.SetupTick)
				for _, packets := range got {
					if len(packets) != 0 {
						t.Fatal("mismatched client-held stack published placement effects")
					}
				}
			}
			applied := 0
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Error("native placement replay clock differs")
						return
					}
					if len(f.Blocks) != len(c.Positions) {
						t.Error("native placement frame cells differ")
						return
					}
					for n, pos := range c.Positions {
						id, states := tx.Block(pos).EncodeBlock()
						g, v := placementPacketState(t, barrierPlacementNativeState{ID: id, States: states}), placementPacketState(t, f.Blocks[n])
						if g != v {
							t.Errorf("tick %d at %v: state %s, native %s", f.Tick, pos, g, v)
						}
					}
					if clock {
						// Network input arrives between samples. Flush before the
						// next sample to expose any publication on its wrong side.
						for _, s := range sessions {
							session.FlushNativeTestBlockUpdates(s)
							for _, stage := range []string{"before_sample", "after_inputs"} {
								session.NativeTestPhaseMarker(s, barrierPlacementPhaseMarker{Index: index, Tick: f.Tick, GameTick: f.GameTick, Stage: stage})
							}
						}
					}
					if c.Changes[0].GameTick == f.GameTick {
						e, ok := handle.Entity(tx)
						if !ok {
							t.Error("native placement player disappeared")
							return
						}
						if err := session.ReplayNativeTestBlockUse(sessions[0], tx, e.(session.Controllable), c.Support); err != nil {
							t.Errorf("native authenticated placement rejected: %v", err)
						}
						applied++
					}
				})
				collect(f.Tick)
				if f.Tick <= c.SetupTick+12 {
					for i := range got {
						got[i] = nil
					}
				}
				if t.Failed() {
					return
				}
			}
			if applied != 1 {
				t.Fatal("native placement input not replayed once")
			}
			for i, role := range []string{"placer", "observer"} {
				if i >= len(sessions) {
					break
				}
				packets := c.Effects
				if i == 1 {
					packets = c.ObserverEffects
				}
				var want []packet.Packet
				if clock {
					packets = nil
					transcript := native.PlacerClock
					if i == 1 {
						transcript = native.ObserverClock
					}
					want = scopedPlacementPhasePackets(transcript, index, c.SetupTick, c.Positions)
				}
				for _, p := range packets {
					if p.Update != nil {
						want = append(want, p.Update)
					} else if p.Sound != nil {
						want = append(want, p.Sound)
					} else {
						t.Fatal("unexpected native placement effect")
					}
				}
				g, _ := json.Marshal(got[i])
				v, _ := json.Marshal(want)
				if !bytes.Equal(g, v) {
					t.Errorf("%s native placement packets differ\ngot: %s\nnative: %s", role, g, v)
				}
			}
		})
	}
	if packetCount != effects || observer && (observerCount != 68) {
		t.Fatal("native placement packet count differs")
	}
}
