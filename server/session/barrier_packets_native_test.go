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
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type barrierPacketViewer struct {
	*bellPacketViewer
	t          *testing.T
	base       cube.Pos
	open, seen bool
}

func (v *barrierPacketViewer) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	if pos == v.base && layer == 0 {
		_, properties := b.EncodeBlock()
		v.open, v.seen = properties["open_bit"].(bool)
	}
	v.bellPacketViewer.ViewBlockUpdate(pos, b, layer)
}

func (v *barrierPacketViewer) ViewSound(pos mgl64.Vec3, s world.Sound) {
	if event, ok := s.(sound.NativeEvent); ok && (strings.HasPrefix(event.SoundType, "door.") || strings.HasPrefix(event.SoundType, "trapdoor.") || strings.HasPrefix(event.SoundType, "fence_gate.")) {
		// Native _setOpen mutates the world before emitting its sound. Wire
		// ordering must not be implemented by moving that sound before mutation.
		if !v.seen || v.open != strings.HasSuffix(event.SoundType, ".open") {
			v.t.Error("sound callback observed the old barrier permutation")
		}
	}
	v.bellPacketViewer.ViewSound(pos, s)
}

func TestBarrierConsumerEffectsMatchNativePackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testBarrierPackets(t, split) })
	}
}

func testBarrierPackets(t *testing.T, split bool) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_barrier_packets.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Before    struct{ Tick int64 } `json:"before_pulse"`
		Scenarios []struct {
			Scenario struct {
				Kind, Form, Feed string
				Initial, Powered bool
				Phase            int
			}
			Base               cube.Pos
			Positions, Sources []cube.Pos
			Initial            []windPacketNativeBlock
			InitialSources     []windPacketNativeBlock `json:"initial_sources"`
			SetupTick          int64                   `json:"setup_tick"`
			Changes            []struct {
				Tick     int64
				Position cube.Pos
				Block    windPacketNativeBlock
			}
			Samples []struct {
				Tick   int64
				Blocks []windPacketNativeBlock
			}
			Effects []struct {
				Update     *packet.UpdateBlock     `json:"block_update"`
				Sound      *packet.LevelSoundEvent `json:"sound"`
				Event      *packet.LevelEvent      `json:"level_event"`
				BlockEvent *packet.BlockEvent      `json:"block_event"`
			} `json:"effect_packets"`
		}
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 192 {
		t.Fatal("incomplete native barrier packet corpus")
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
			t.Fatalf("native barrier packet input unavailable: %+v", b)
		}
		return v
	}
	packetCount := 0
	for _, c := range native.Scenarios {
		packetCount += len(c.Effects)
		t.Run(fmt.Sprintf("%s/initial%t/powered%t/%s/phase%d", c.Scenario.Kind, c.Scenario.Initial, c.Scenario.Powered, c.Scenario.Feed, c.Scenario.Phase), func(t *testing.T) {
			if len(c.Positions) != 2 || len(c.Initial) != 2 || len(c.Sources) != len(c.InitialSources) || len(c.Samples) != 108 || len(c.Changes) != 13 || c.Changes[0].Tick%2 != int64(c.Scenario.Phase) || native.Before.Tick != c.SetupTick+10 {
				t.Fatal("native barrier packet controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-barrier-packets")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 1024)
			viewer := &barrierPacketViewer{bellPacketViewer: &bellPacketViewer{session: s, split: split}, t: t, base: c.Base}
			loader := world.NewLoader(2, w, viewer)
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 3; x <= c.Base[0]+3; x++ {
					for z := c.Base[2] - 3; z <= c.Base[2]+3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
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
					case *packet.LevelEvent:
						pos = cube.PosFromVec3(mgl64.Vec3{float64(p.Position[0]), float64(p.Position[1]), float64(p.Position[2])})
					case *packet.BlockEvent:
						pos = cube.Pos{int(p.Position[0]), int(p.Position[1]), int(p.Position[2])}
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
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick || len(f.Blocks) != 2 {
						t.Fatal("native barrier packet frame controls differ")
					}
					for i, pos := range c.Positions {
						id, states := tx.Block(pos).EncodeBlock()
						g, v := windPacketBlockKey(t, windPacketNativeBlock{ID: id, States: states}), windPacketBlockKey(t, f.Blocks[i])
						if g != v {
							t.Errorf("tick %d at %v: got %s, native %s", f.Tick, pos, g, v)
						}
					}
					for _, change := range c.Changes {
						if change.Tick == f.Tick {
							tx.SetBlock(change.Position, decode(change.Block), &world.SetOpts{CommandFill: true})
						}
					}
				})
				collect()
				if f.Tick <= native.Before.Tick {
					got = nil
				}
				if t.Failed() {
					break
				}
			}
			var want []packet.Packet
			for _, p := range c.Effects {
				switch {
				case p.Update != nil:
					want = append(want, p.Update)
				case p.Sound != nil:
					want = append(want, p.Sound)
				case p.Event != nil:
					want = append(want, p.Event)
				case p.BlockEvent != nil:
					want = append(want, p.BlockEvent)
				default:
					t.Fatal("unknown native barrier packet")
				}
			}
			g, _ := json.Marshal(got)
			v, _ := json.Marshal(want)
			if !bytes.Equal(g, v) {
				t.Fatalf("native barrier packets differ\ngot: %s\nnative: %s", g, v)
			}
		})
	}
	if packetCount != 3512 {
		t.Fatal("native barrier effect packet count differs")
	}
}
