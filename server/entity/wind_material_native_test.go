//go:build redstone_wind_material_audit

package entity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/world"
)

// The SDK exposes a redundant legacy direction beside cardinal_direction.
// An upper door's unused open bit is retained in the corpus; wire comparison
// follows the independently verified upper-half contract in block tests.
func windMaterialState(t *testing.T, b pistonEntityNativeBlock, wire bool) []byte {
	t.Helper()
	properties := map[string]any{}
	for k, v := range b.States {
		switch n := v.(type) {
		case uint8:
			v = n != 0
		case int32:
			v = float64(n)
		}
		properties[k] = v
	}
	if direction, ok := properties["minecraft:cardinal_direction"].(string); ok {
		if legacy, exists := properties["direction"]; exists {
			want := map[string]float64{"south": 0, "west": 1, "north": 2, "east": 3}[direction]
			if legacy != want {
				t.Fatalf("native cardinal/direction aliases disagree: %+v", b)
			}
			delete(properties, "direction")
		}
	}
	if b.ID == "minecraft:stone" {
		delete(properties, "stone_type")
	}
	if _, door := properties["door_hinge_bit"]; door && wire && properties["upper_block_bit"] == true {
		properties["open_bit"] = false
	}
	encoded, _ := json.Marshal(struct {
		ID     string
		States map[string]any
	}{b.ID, properties})
	return encoded
}

// TestWindBurstMaterialsMatchBDS is an unfinished, strict audit diagnostic.
// It currently reproduces powered-door/trapdoor/gate and candle differences;
// see docs/redstone-audit.md before enabling it. No mismatches are skipped.
func TestWindBurstMaterialsMatchBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_wind_materials.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind, Form       string
				Initial, Powered bool
				Phase            int
			}
			Base, Target cube.Pos
			Positions    []cube.Pos
			SetupTick    int64 `json:"setup_tick"`
			Initial      []pistonEntityNativeBlock
			Changes      []struct {
				Tick  int64
				Spawn pistonEntityNativeBody
			}
			Hits []struct {
				Tick     int64
				Position pistonEntityNativeVec
			}
			Samples []struct {
				Tick   int64
				Blocks []pistonEntityNativeBlock
			}
		}
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 176 {
		t.Fatal("incomplete native wind material corpus")
	}
	world.DefaultBlockRegistry.Finalize()
	byState := map[string]world.Block{}
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		id, states := b.EncodeBlock()
		key := windMaterialState(t, pistonEntityNativeBlock{ID: id, States: states}, false)
		byState[string(key)] = b
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/initial%t/powered%t/phase%d", c.Scenario.Kind, c.Scenario.Initial, c.Scenario.Powered, c.Scenario.Phase), func(t *testing.T) {
			if len(c.Positions) != 4 || len(c.Initial) != 4 || len(c.Samples) != 56 || len(c.Changes) != 1 || len(c.Hits) != 1 || c.Changes[0].Tick != c.SetupTick+20+int64(c.Scenario.Phase) || cube.PosFromVec3(c.Hits[0].Position.vec()) != c.Base {
				t.Fatal("invalid native wind material controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			_ = w.SetGameRule("fire_tick", false)
			decode := func(b pistonEntityNativeBlock) world.Block {
				t.Helper()
				key := windMaterialState(t, b, false)
				v, ok := byState[string(key)]
				if !ok {
					t.Fatalf("native material input unavailable: %s", key)
				}
				return v
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				stone := decode(pistonEntityNativeBlock{ID: "minecraft:stone", States: map[string]any{}})
				for x := c.Base[0] - 3; x <= c.Base[0]+4; x++ {
					for z := c.Base[2] - 3; z <= c.Base[2]+4; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, stone, nil)
					}
				}
				tx.SetBlock(c.Positions[3], decode(c.Initial[3]), nil)
				for i, pos := range c.Positions[:3] {
					tx.SetBlock(pos, decode(c.Initial[i]), nil)
				}
			})
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick || len(f.Blocks) != 4 {
						t.Fatal("native material clock/cell count differs")
					}
					for i, pos := range c.Positions {
						id, states := tx.Block(pos).EncodeBlock()
						got := windMaterialState(t, pistonEntityNativeBlock{ID: id, States: states}, true)
						want := windMaterialState(t, f.Blocks[i], true)
						if !bytes.Equal(got, want) {
							t.Errorf("tick %d at %v: got %s, native %s", f.Tick, pos, got, want)
						}
					}
					if f.Tick == c.Changes[0].Tick {
						conf := windChargeConf
						conf.Hit = func(e *Ent, tx *world.Tx, result trace.Result) {
							defer func() {
								if p := recover(); p != nil {
									t.Errorf("wind material hit panicked: %v", p)
									_ = e.CloseIn(tx)
								}
							}()
							windChargeHit(e, tx, result)
						}
						spawn := c.Changes[0].Spawn
						tx.AddEntity((world.EntitySpawnOpts{Position: spawn.Position.vec(), Velocity: spawn.Velocity.vec()}).New(WindChargeType, conf))
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
