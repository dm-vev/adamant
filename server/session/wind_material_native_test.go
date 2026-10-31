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

type windPacketNativeBlock struct {
	ID     string
	States map[string]any
}

func windPacketBlockKey(t *testing.T, b windPacketNativeBlock) string {
	t.Helper()
	states := map[string]any{}
	for k, v := range b.States {
		switch n := v.(type) {
		case uint8:
			v = n != 0
		case int32:
			v = float64(n)
		}
		states[k] = v
	}
	if d, ok := states["minecraft:cardinal_direction"].(string); ok {
		if v, exists := states["direction"]; exists {
			if v != map[string]float64{"south": 0, "west": 1, "north": 2, "east": 3}[d] {
				t.Fatal("native direction aliases differ")
			}
			delete(states, "direction")
		}
	}
	if b.ID == "minecraft:stone" {
		delete(states, "stone_type")
	}

	data, _ := json.Marshal(windPacketNativeBlock{ID: b.ID, States: states})
	return string(data)
}

// Campfire actor refresh timing is outside this activating-attack comparison.
// Its raw packets remain in the corpus; entity replay checks its state controls.
func TestWindMaterialActivationMatchesNativePackets(t *testing.T) {
	for _, split := range []bool{false, true} {
		t.Run(fmt.Sprintf("split%t", split), func(t *testing.T) { testWindMaterialPackets(t, split) })
	}
}

func testWindMaterialPackets(t *testing.T, split bool) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_wind_material_effects.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Before    struct{ Tick int64 } `json:"before_burst"`
		Scenarios []struct {
			Scenario struct {
				Kind, Form       string
				Initial, Powered bool
				Phase            int
			}
			Base, Target cube.Pos
			Positions    []cube.Pos
			Initial      []windPacketNativeBlock
			SetupTick    int64 `json:"setup_tick"`
			Changes      []struct {
				Tick  int64
				Spawn struct {
					ID                 string
					Position, Velocity struct{ X, Y, Z float64 }
				}
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
	if !native.Complete || len(native.Scenarios) != 176 {
		t.Fatal("incomplete native wind material packet corpus")
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
			t.Fatalf("native packet material unavailable: %+v", b)
		}
		return v
	}
	packetCount, sceneCount := 0, 0
	for _, c := range native.Scenarios {
		if c.Scenario.Form == "campfire" {
			continue
		}
		sceneCount++
		packetCount += len(c.Effects)
		t.Run(fmt.Sprintf("%s/initial%t/powered%t/phase%d", c.Scenario.Kind, c.Scenario.Initial, c.Scenario.Powered, c.Scenario.Phase), func(t *testing.T) {
			if len(c.Initial) != 4 || len(c.Positions) != 4 || len(c.Changes) != 1 || len(c.Samples) != 56 || native.Before.Tick != c.SetupTick+10 {
				t.Fatal("native wind packet input controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			s := testSession("native-wind-material")
			s.br = world.DefaultBlockRegistry
			s.packets = make(chan packet.Packet, 1024)
			loader := world.NewLoader(2, w, &bellPacketViewer{session: s, split: split})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			_ = w.SetGameRule("fire_tick", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 3; x <= c.Base[0]+4; x++ {
					for z := c.Base[2] - 3; z <= c.Base[2]+4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				tx.SetBlock(c.Positions[3], decode(c.Initial[3]), nil)
				for i, pos := range c.Positions[:3] {
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
					if tx.CurrentTick() != f.Tick || len(f.Blocks) != 4 {
						t.Fatal("native wind packet frame controls differ")
					}
					for i, pos := range c.Positions {
						id, states := tx.Block(pos).EncodeBlock()
						if g, v := windPacketBlockKey(t, windPacketNativeBlock{ID: id, States: states}), windPacketBlockKey(t, f.Blocks[i]); g != v {
							t.Errorf("tick %d at %v: got %s, native %s", f.Tick, pos, g, v)
						}
					}
					if f.Tick == c.Changes[0].Tick {
						spawn := c.Changes[0].Spawn
						if spawn.ID != "minecraft:wind_charge_projectile" {
							t.Fatal("native wind packet identity differs")
						}
						tx.AddEntity(entity.NewWindCharge(world.EntitySpawnOpts{Position: mgl64.Vec3{spawn.Position.X, spawn.Position.Y, spawn.Position.Z}, Velocity: mgl64.Vec3{spawn.Velocity.X, spawn.Velocity.Y, spawn.Velocity.Z}}, nil))
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
					t.Fatal("unknown native wind material effect")
				}
			}
			g, _ := json.Marshal(got)
			v, _ := json.Marshal(want)
			if !bytes.Equal(g, v) {
				t.Fatalf("native wind material packets differ\ngot: %s\nnative: %s", g, v)
			}
		})
	}
	if packetCount != 168 || sceneCount != 160 {
		t.Fatal("native wind positive/negative control counts differ")
	}
}
