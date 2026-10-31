package session

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type barrierClockMarker struct {
	Tick  int64
	Stage string
}
type barrierClockPacket struct {
	Marker *barrierClockMarker     `json:"marker,omitempty"`
	Update *packet.UpdateBlock     `json:"block_update,omitempty"`
	Sound  *packet.LevelSoundEvent `json:"sound,omitempty"`
}

func TestBarrierPacketsRespectNativeScriptPhase(t *testing.T) {
	for _, kind := range []string{"redstone", "wind"} {
		for _, split := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/split%t", kind, split), func(t *testing.T) { testBarrierClockNative(t, kind, split) })
		}
	}
}

func testBarrierClockNative(t *testing.T, kind string, split bool) {
	t.Helper()
	file := "bedrock_1_26_51_barrier_clock.json"
	if kind == "wind" {
		file = "bedrock_1_26_51_wind_barrier_clock.json"
	}
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Kind      string
		Scenarios []struct {
			Scenario struct {
				Kind, Form, Feed string
				Initial, Powered bool
				Phase            int
			}
			Base, Target       cube.Pos
			Positions, Sources []cube.Pos
			Initial            []windPacketNativeBlock
			InitialSources     []windPacketNativeBlock `json:"initial_sources"`
			SetupTick          int64                   `json:"setup_tick"`
			Changes            []struct {
				Tick     int64
				Position cube.Pos
				Block    windPacketNativeBlock
				Spawn    struct {
					ID                 string
					Position, Velocity struct{ X, Y, Z float64 }
				}
			}
			Samples []struct {
				Tick   int64
				Blocks []windPacketNativeBlock
			}
		}
		Transcript []struct {
			barrierClockPacket
			SubChunk *packet.UpdateSubChunkBlocks `json:"subchunk_update"`
			Sequence uint64                       `json:"packet_sequence"`
		}
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || native.Kind != kind || len(native.Scenarios) != 32 {
		t.Fatal("incomplete native clock corpus")
	}
	markers, last := 0, uint64(0)
	for _, p := range native.Transcript {
		if p.Sequence <= last {
			t.Fatal("native packet sequence is not monotonic")
		}
		last = p.Sequence
		if p.Marker != nil {
			markers++
		}
	}
	if markers != 216 {
		t.Fatal("native clock marker tail is incomplete")
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
			t.Fatalf("native clock block unavailable: %+v", b)
		}
		return v
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/initial%t/%s/phase%d", c.Scenario.Kind, c.Scenario.Initial, c.Scenario.Feed, c.Scenario.Phase), func(t *testing.T) {
			cells, changes := 2, 13
			focus := c.Base
			if kind == "wind" {
				cells, changes, focus = 4, 1, c.Target
			}
			if len(c.Positions) != cells || len(c.Initial) != cells || len(c.Samples) != 108 || len(c.Changes) != changes {
				t.Fatal("native clock input controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-barrier-clock")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 2048)
			viewer := &bellPacketViewer{session: s, split: split}
			loader := world.NewLoader(2, w, viewer)
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			_ = w.SetGameRule("fire_tick", false)
			inFocus := func(pos cube.Pos) bool { return pos == focus || pos == focus.Side(cube.FaceUp) }
			var got []barrierClockPacket
			marker := func(tick int64, stage string) {
				b, _ := json.Marshal(barrierClockMarker{Tick: tick, Stage: stage})
				s.writePacket(&packet.Text{Message: string(b)})
			}
			collect := func() {
				s.flushBlockUpdates()
				for len(s.packets) > 0 {
					switch p := (<-s.packets).(type) {
					case *packet.Text:
						var m barrierClockMarker
						if err := json.Unmarshal([]byte(p.Message), &m); err != nil {
							t.Fatal(err)
						}
						got = append(got, barrierClockPacket{Marker: &m})
					case *packet.UpdateBlock:
						if p.Layer == 0 && inFocus(cube.Pos{int(p.Position[0]), int(p.Position[1]), int(p.Position[2])}) {
							got = append(got, barrierClockPacket{Update: p})
						}
					case *packet.LevelSoundEvent:
						if inFocus(cube.PosFromVec3(mgl64.Vec3{float64(p.Position[0]), float64(p.Position[1]), float64(p.Position[2])})) {
							got = append(got, barrierClockPacket{Sound: p})
						}
					}
				}
			}
			frame := 0
			// A stationary observer registers the Script-like deferred callback
			// before block actors/circuits/late projectiles. The actual simulation
			// and real wind entity remain in their ordinary world tick phases.
			observer := entity.StationaryBehaviourConfig{Tick: func(_ *entity.Ent, tx *world.Tx) {
				tx.Defer(func(tx *world.Tx) {
					if frame >= len(c.Samples) {
						t.Error("extra clock observer tick")
						return
					}
					f := c.Samples[frame]
					frame++
					if tx.CurrentTick() != f.Tick {
						t.Errorf("clock tick %d, native %d", tx.CurrentTick(), f.Tick)
						return
					}
					// This attempted flush detects state publication before the native
					// Script interval even without a split block-update viewer.
					s.flushBlockUpdates()
					for i, pos := range c.Positions {
						id, states := tx.Block(pos).EncodeBlock()
						if g, v := windPacketBlockKey(t, windPacketNativeBlock{ID: id, States: states}), windPacketBlockKey(t, f.Blocks[i]); g != v {
							t.Errorf("tick %d state differs at %v: %s, native %s", f.Tick, pos, g, v)
						}
					}
					marker(f.Tick, "before_inputs")
					for _, change := range c.Changes {
						if change.Tick == f.Tick {
							if kind == "wind" {
								p, v := change.Spawn.Position, change.Spawn.Velocity
								tx.AddEntity(entity.NewWindCharge(world.EntitySpawnOpts{Position: mgl64.Vec3{p.X, p.Y, p.Z}, Velocity: mgl64.Vec3{v.X, v.Y, v.Z}}, nil))
							} else {
								tx.SetBlock(change.Position, decode(change.Block), &world.SetOpts{CommandFill: true})
							}
						}
					}
					marker(f.Tick, "after_inputs")
				})
			}}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 3; x <= c.Base[0]+4; x++ {
					for z := c.Base[2] - 3; z <= c.Base[2]+4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				if kind == "wind" {
					tx.SetBlock(c.Positions[3], decode(c.Initial[3]), nil)
				}
				for i, pos := range c.Sources {
					tx.SetBlock(pos, decode(c.InitialSources[i]), nil)
				}
				for i, pos := range c.Positions {
					tx.SetBlock(pos, decode(c.Initial[i]), nil)
				}
				tx.AddEntity((world.EntitySpawnOpts{Position: c.Base.Vec3().Add(mgl64.Vec3{3, 4, -3})}).New(entity.TextType, observer))
			})
			collect()
			got = nil
			for range c.Samples {
				w.AdvanceTick()
				collect()
				if t.Failed() {
					break
				}
			}
			// Ignore only the explicitly warmed setup window on both streams.
			trim := func(items []barrierClockPacket) []barrierClockPacket {
				active := false
				out := []barrierClockPacket{}
				for _, p := range items {
					if p.Marker != nil {
						active = p.Marker.Tick >= c.SetupTick+16
					}
					if active {
						out = append(out, p)
					}
				}
				return out
			}
			var want []barrierClockPacket
			for _, p := range native.Transcript {
				if p.Marker != nil {
					want = append(want, p.barrierClockPacket)
					continue
				}
				if p.Update != nil && inFocus(cube.Pos{int(p.Update.Position[0]), int(p.Update.Position[1]), int(p.Update.Position[2])}) {
					want = append(want, barrierClockPacket{Update: p.Update})
				}
				if p.Sound != nil && inFocus(cube.PosFromVec3(mgl64.Vec3{float64(p.Sound.Position[0]), float64(p.Sound.Position[1]), float64(p.Sound.Position[2])})) {
					want = append(want, barrierClockPacket{Sound: p.Sound})
				}
				if p.SubChunk != nil {
					for _, u := range p.SubChunk.Blocks {
						pos := u.BlockPos
						if inFocus(cube.Pos{int(pos[0]), int(pos[1]), int(pos[2])}) {
							want = append(want, barrierClockPacket{Update: &packet.UpdateBlock{Position: pos, NewBlockRuntimeID: u.BlockRuntimeID, Flags: u.Flags}})
						}
					}
				}
			}
			got, want = trim(got), trim(want)
			if len(got) != len(want) {
				t.Errorf("clock packet count %d, native %d", len(got), len(want))
			}
			for i := 0; i < len(got) && i < len(want); i++ {
				g, _ := json.Marshal(got[i])
				v, _ := json.Marshal(want[i])
				if !bytes.Equal(g, v) {
					t.Fatalf("clock packet %d: got %s, native %s", i, g, v)
				}
			}
		})
	}
}
