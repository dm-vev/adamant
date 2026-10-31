package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
)

func TestWindBurstSwitchGridsMatchBDS(t *testing.T) {
	testWindBurstSwitchGridsNative(t, false)
}

func TestBreezeWindBurstSwitchGridsMatchBDS(t *testing.T) {
	testWindBurstSwitchGridsNative(t, true)
}

func testWindBurstSwitchGridsNative(t *testing.T, breeze bool) {
	t.Helper()
	file, count := "bedrock_1_26_51_wind_switch_grids.json", 64
	if breeze {
		file, count = "bedrock_1_26_51_breeze_switch_grids.json", 16
	}
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind             string
				Direction, Phase int
				Wall             bool
			}
			Base      cube.Pos
			SetupTick int64 `json:"setup_tick"`
			Cells     []struct {
				Position cube.Pos
				Block    pistonEntityNativeBlock
			}
			Changes []struct {
				Tick  int64
				Spawn pistonEntityNativeBody
			}
			Hits []struct {
				Tick     int64
				Position pistonEntityNativeVec
			}
			RandomInput struct{ Words []uint32 } `json:"random_input"`
			Samples     []struct {
				Tick   int64
				Values []int
			}
		}
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native wind switch grid corpus")
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/direction%d/wall%t/phase%d", c.Scenario.Kind, c.Scenario.Direction, c.Scenario.Wall, c.Scenario.Phase), func(t *testing.T) {
			// The held interval also includes unrelated world draws, whose
			// count varies. The burst itself must consume exactly its 1352
			// scoped inputs; that invariant is checked after replay below.
			if len(c.Cells) != 49 || len(c.Samples) != 56 || len(c.Changes) != 1 || len(c.Hits) != 1 || len(c.RandomInput.Words) < 1432 || c.Changes[0].Tick != c.SetupTick+20+int64(c.Scenario.Phase) || c.Hits[0].Tick != c.Changes[0].Tick+2 || cube.PosFromVec3(c.Hits[0].Position.vec()) != c.Base {
				t.Fatal("invalid native wind grid controls")
			}
			random := &nativeContainerRandom{}
			for _, word := range c.RandomInput.Words[80:1432] {
				random.words = append(random.words, uint64(word)<<32)
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, RandSource: random, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := 3; x <= 13; x++ {
					for z := 3; z <= 13; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				if c.Scenario.Wall {
					v := [4]cube.Pos{{0, 0, 1}, {-1, 0, 0}, {0, 0, -1}, {1, 0, 0}}[c.Scenario.Direction]
					for n := -3; n <= 3; n++ {
						for y := 65; y <= 67; y++ {
							tx.SetBlock(cube.Pos{8 - v[0] + v[2]*n, y, 8 - v[2] - v[0]*n}, block.Stone{}, nil)
						}
					}
				}
				for _, cell := range c.Cells {
					var b world.Block = block.Stone{}
					if cell.Block.ID != "minecraft:stone" {
						properties := map[string]any{}
						for key, value := range cell.Block.States {
							switch v := value.(type) {
							case float64:
								value = int32(v)
							case bool:
								if key == "button_pressed_bit" {
									value = uint8(0)
									if v {
										value = uint8(1)
									}
								}
							}
							properties[key] = value
						}
						var ok bool
						b, ok = tx.World().BlockRegistry().BlockByName(cell.Block.ID, properties)
						if !ok {
							t.Fatalf("native grid permutation missing: %s %v", cell.Block.ID, properties)
						}
					}
					tx.SetBlock(cell.Position, b, nil)
				}
			})
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick || len(f.Values) != len(c.Cells) {
						t.Fatal("native wind grid frame clock or count differs")
					}
					for i, cell := range c.Cells {
						b := tx.Block(cell.Position)
						id, _ := b.EncodeBlock()
						value := -1
						switch v := b.(type) {
						case block.Lever:
							value = int(boolByte(v.Powered))
						case block.Button:
							value = int(boolByte(v.Pressed))
						}
						if id != cell.Block.ID || value != f.Values[i] {
							t.Errorf("tick %d at %v: %s/%d, native %s/%d", f.Tick, cell.Position, id, value, cell.Block.ID, f.Values[i])
						}
					}
					if f.Tick == c.Changes[0].Tick {
						conf := windChargeConf
						entityType := world.EntityType(WindChargeType)
						if breeze {
							conf = breezeWindChargeConfig(nil)
							entityType = BreezeWindChargeType
						}
						hit := conf.Hit
						conf.Hit = func(e *Ent, tx *world.Tx, result trace.Result) {
							random.active = true
							defer func() { random.active = false }()
							hit(e, tx, result)
						}
						spawn := c.Changes[0].Spawn
						tx.AddEntity((world.EntitySpawnOpts{Position: spawn.Position.vec(), Velocity: spawn.Velocity.vec()}).New(entityType, conf))
					}
				})
				if t.Failed() {
					break
				}
			}
			if random.used != 1352 {
				t.Errorf("wind burst consumed %d ray words, native 1352", random.used)
			}
		})
	}
}
