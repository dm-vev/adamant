package session

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

type dispenserPacketViewer struct {
	world.NopViewer
	session *Session
}

func (v *dispenserPacketViewer) ViewSound(pos mgl64.Vec3, s world.Sound) {
	v.session.ViewSound(pos, s)
}
func (v *dispenserPacketViewer) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	v.session.ViewParticle(pos, p)
}

func TestDispenserEffectsMatchNativePackets(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_dispenser_sounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind, Destination string
				Face, Phase       int
			}
			Base, Source, Target cube.Pos
			SetupTick            int64 `json:"setup_tick"`
			Changes              []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick  int64
				Block struct {
					ID     string
					States map[string]any
				}
				SourceCount int `json:"source_count"`
				Outputs     int
			}
			Packets []struct {
				Sound *packet.LevelSoundEvent
				Event *packet.LevelEvent `json:"level_event"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 96 {
		t.Fatal("incomplete native dispenser packet corpus")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/face%d/%s/phase%d", c.Kind, c.Face, c.Destination, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 100 || len(trace.Changes) != 2 || trace.SetupTick%20 != 2 || trace.Changes[0].Tick%2 != int64(c.Phase) {
				t.Fatal("invalid native dispenser packet controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = trace.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("dispenser-packets")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 64)
			loader := world.NewLoader(2, w, &dispenserPacketViewer{session: s})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, trace.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := -2; x <= 2; x++ {
					for z := -2; z <= 2; z++ {
						tx.SetBlock(cube.Pos{trace.Base[0] + x, 63, trace.Base[2] + z}, block.Stone{}, nil)
					}
				}
				var b world.Block
				switch c.Kind {
				case "dropper":
					d := block.NewDropper()
					d.Facing = cube.Face(c.Face)
					b = d
				case "dispenser":
					d := block.NewDispenser()
					d.Facing = cube.Face(c.Face)
					b = d
				default:
					t.Fatal("unknown native dispenser kind")
				}
				if c.Destination != "empty" {
					_ = b.(block.Container).Inventory(tx, trace.Base).SetItem(4, item.NewStack(item.IronIngot{}, 3))
				}
				tx.SetBlock(trace.Base, b, nil)
				if c.Destination == "barrel" || c.Destination == "full" {
					barrel := block.NewBarrel()
					if c.Destination == "full" {
						for slot := range 27 {
							_ = barrel.Inventory(tx, trace.Target).SetItem(slot, item.NewStack(block.Stone{}, 64))
						}
					}
					tx.SetBlock(trace.Target, barrel, nil)
				}
			})
			var got []packet.Packet
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native dispenser packet clock differs")
					}
					name, states := tx.Block(trace.Base).EncodeBlock()
					if name != frame.Block.ID || int(states["facing_direction"].(int32)) != int(frame.Block.States["facing_direction"].(float64)) || (states["triggered_bit"].(uint8) != 0) != frame.Block.States["triggered_bit"].(bool) {
						t.Fatalf("tick %d source permutation differs", frame.Tick)
					}
					stack, _ := tx.Block(trace.Base).(block.Container).Inventory(tx, trace.Base).Item(4)
					if stack.Count() != frame.SourceCount {
						t.Fatalf("tick %d source count=%d, native=%d", frame.Tick, stack.Count(), frame.SourceCount)
					}
					if barrel, ok := tx.Block(trace.Target).(block.Barrel); ok {
						output, _ := barrel.Inventory(tx, trace.Target).Item(0)
						if output.Count() != frame.Outputs {
							t.Fatalf("tick %d destination count differs", frame.Tick)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick {
							var source world.Block
							if change.Powered {
								source = block.RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, source, nil)
						}
					}
				})
				for len(s.packets) > 0 {
					got = append(got, <-s.packets)
				}
				if t.Failed() {
					break
				}
			}
			var want []packet.Packet
			for _, p := range trace.Packets {
				if p.Sound != nil {
					want = append(want, p.Sound)
				} else if p.Event != nil {
					want = append(want, p.Event)
				} else {
					t.Fatal("invalid native packet record")
				}
			}
			if !reflect.DeepEqual(got, want) {
				gotJSON, _ := json.Marshal(got)
				wantJSON, _ := json.Marshal(want)
				t.Fatalf("packets differ\ngot: %s\nnative: %s", gotJSON, wantJSON)
			}
		})
	}
}
