package player

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SDK legacy aliases are checked before removing them; all actual permutation
// properties, including both unused upper-door bits, remain in the comparison.
func barrierPlacementState(t *testing.T, b tripwireBreakNativeBlock) string {
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
				t.Fatal("native placement direction aliases disagree")
			}
			delete(states, "direction")
		}
	}
	if b.ID == "minecraft:stone" {
		if v, ok := states["stone_type"]; ok {
			if v != "stone" {
				t.Fatal("unexpected SDK stone alias")
			}
			delete(states, "stone_type")
		}
	}
	b.States = states
	data, _ := json.Marshal(b)
	return string(data)
}

func TestBarrierPlayerPlacementMatchesNativeStates(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_barrier_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Kind      string
		Scenarios []struct {
			Scenario struct {
				Block, Form string
				Direction   int
				Powered     bool
			}
			Base, Support  cube.Pos
			Positions      []cube.Pos
			Feet           mgl64.Vec3
			SetupTick      int64 `json:"setup_tick"`
			Initial        []tripwireBreakNativeBlock
			InitialSupport tripwireBreakNativeBlock `json:"initial_support"`
			Changes        []struct {
				GameTick int64 `json:"game_tick"`
				Rotation struct{ X, Y float64 }
				Held     string
				Count    int
				Blocks   []tripwireBreakNativeBlock
			}
			Results []struct{ Blocks []tripwireBreakNativeBlock }
			Samples []struct {
				Tick     int64
				GameTick int64 `json:"game_tick"`
				Blocks   []tripwireBreakNativeBlock
				Support  tripwireBreakNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || native.Kind != "actual_player_placement" || len(native.Scenarios) != 112 {
		t.Fatal("incomplete actual barrier placement corpus")
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/direction%d/powered%t/tick%d", c.Scenario.Block, c.Scenario.Direction, c.Scenario.Powered, c.SetupTick), func(t *testing.T) {
			cells := 1
			if c.Scenario.Form == "door" {
				cells = 2
			}
			if len(c.Changes) != 1 || len(c.Results) != 1 || len(c.Samples) < 36 || len(c.Positions) != cells || len(c.Initial) != cells || c.Changes[0].Held != "minecraft:"+c.Scenario.Block || c.Changes[0].Count != 2 {
				t.Fatal("native player placement controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			var handle *world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 2; x <= c.Base[0]+2; x++ {
					for z := c.Base[2] - 2; z <= c.Base[2]+2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
				if c.Scenario.Powered {
					tx.SetBlock(c.Support, block.RedstoneBlock{}, nil)
				}
				change := c.Changes[0]
				cfg := Config{Name: "RedstoneAudit", Position: c.Feet, Rotation: cube.Rotation{change.Rotation.Y, change.Rotation.X}, GameMode: world.GameModeSurvival}
				handle = world.EntitySpawnOpts{Position: c.Feet}.New(Type, cfg)
				p := tx.AddEntity(handle).(*Player)
				held, ok := world.ItemByName(change.Held, 0)
				if !ok {
					t.Fatal("native placement item unavailable")
				}
				p.SetHeldItems(item.NewStack(held, 2), item.Stack{})
			})
			compare := func(tx *world.Tx, want []tripwireBreakNativeBlock) {
				t.Helper()
				if len(want) != cells {
					t.Fatal("native placement frame size differs")
				}
				for i, pos := range c.Positions {
					id, states := tx.Block(pos).EncodeBlock()
					g, v := barrierPlacementState(t, tripwireBreakNativeBlock{ID: id, States: states}), barrierPlacementState(t, want[i])
					if g != v {
						t.Errorf("tick %d at %v: got %s, native %s", tx.CurrentTick(), pos, g, v)
					}
				}
			}
			applied := 0
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native placement game clock differs")
					}
					compare(tx, f.Blocks)
					id, states := tx.Block(c.Support).EncodeBlock()
					if barrierPlacementState(t, tripwireBreakNativeBlock{ID: id, States: states}) != barrierPlacementState(t, f.Support) {
						t.Fatal("native placement support differs")
					}
					if c.Changes[0].GameTick == f.GameTick {
						compare(tx, c.Changes[0].Blocks)
						e, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("actual placement player missing")
						}
						p := e.(*Player)
						p.UseItemOnBlock(c.Support, cube.FaceUp, mgl64.Vec3{.5, 1, .5})
						compare(tx, c.Results[0].Blocks)
						held, _ := p.HeldItems()
						if held.Count() != 1 {
							t.Error("one native accepted placement did not consume one item")
						}
						applied++
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Fatal("actual native placement was not replayed exactly once")
			}
		})
	}
}
