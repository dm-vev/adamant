package player

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func doorSupportPlacementKey(t *testing.T, b tripwireBreakNativeBlock) string {
	t.Helper()
	b.States = maps.Clone(b.States)
	alias := func(name string, want any) {
		if got, exists := b.States[name]; exists {
			if got != want {
				t.Fatalf("unexpected native support alias %s=%v, want %v", name, got, want)
			}
			delete(b.States, name)
		}
	}
	if strings.HasPrefix(b.ID, "minecraft:oak_") {
		alias("wood_type", "oak")
	}
	if b.ID == "minecraft:oak_leaves" {
		alias("old_leaf_type", "oak")
	}
	if b.ID == "minecraft:cobblestone_wall" {
		alias("wall_block_type", "cobblestone")
	}
	if b.ID == "minecraft:white_carpet" {
		alias("color", "white")
	}
	if half, ok := b.States["minecraft:vertical_half"].(string); ok {
		alias("top_slot_bit", half == "top")
	}
	return barrierPlacementState(t, b)
}

func TestDoorPlayerSupportPlacementMatchesNative(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_door_support_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block   string
				Support struct {
					ID     string
					States map[string]any
				}
			}
			Base, Support  cube.Pos
			Positions      []cube.Pos
			Feet           mgl64.Vec3
			SetupTick      int64 `json:"setup_tick"`
			Initial, Final []tripwireBreakNativeBlock
			Accepted       bool
			PacketInput    struct {
				Cached   uint32 `json:"cached_runtime_id"`
				Sent     uint32 `json:"sent_runtime_id"`
				Position cube.Pos
			} `json:"packet_input"`
			FinalHeld struct {
				ID    string
				Count int
			} `json:"final_held"`
			Changes []struct {
				Tick         int64
				BeforeSample bool `json:"before_sample"`
				Held         string
				Count        int
				Rotation     struct{ X, Y float64 }
				Blocks       []tripwireBreakNativeBlock
			}
			Results []struct{ Blocks []tripwireBreakNativeBlock }
			Samples []struct {
				Tick   int64
				Blocks []tripwireBreakNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 66 {
		t.Fatal("incomplete native door support placement corpus")
	}
	world.DefaultBlockRegistry.Finalize()
	permutations := map[string]world.Block{}
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		id, states := b.EncodeBlock()
		permutations[doorSupportPlacementKey(t, tripwireBreakNativeBlock{ID: id, States: states})] = b
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%s/%v", c.Scenario.Block, c.Scenario.Support.ID, c.Scenario.Support.States), func(t *testing.T) {
			if len(c.Changes) != 1 || len(c.Positions) != 3 || len(c.Initial) != 3 || len(c.Results) != map[bool]int{false: 0, true: 1}[c.Accepted] || c.Changes[0].Held != "minecraft:"+c.Scenario.Block || c.Changes[0].Count != 2 {
				t.Fatal("native support placement controls differ")
			}
			if c.PacketInput.Position != c.Support || c.PacketInput.Cached != c.PacketInput.Sent {
				t.Fatal("native input did not use the cached server block hash")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", false)
			_ = w.SetGameRule("spawn_mobs", false)
			var handle *world.EntityHandle
			compare := func(tx *world.Tx, want []tripwireBreakNativeBlock) {
				t.Helper()
				if len(want) != 3 {
					t.Fatal("native support placement cells differ")
				}
				for i, pos := range c.Positions {
					id, states := tx.Block(pos).EncodeBlock()
					got, expected := doorSupportPlacementKey(t, tripwireBreakNativeBlock{ID: id, States: states}), doorSupportPlacementKey(t, want[i])
					if got != expected {
						t.Errorf("tick %d at %v: %s, native %s", tx.CurrentTick(), pos, got, expected)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 2; x <= c.Base[0]+2; x++ {
					for z := c.Base[2] - 2; z <= c.Base[2]+2; z++ {
						tx.SetBlockKnownShape(cube.Pos{x, 64, z}, block.Stone{})
					}
				}
				tx.SetBlockKnownShape(c.Support.Side(cube.FaceDown), block.Stone{})
				b, ok := permutations[doorSupportPlacementKey(t, c.Initial[0])]
				if !ok {
					t.Fatal("native support unavailable")
				}
				tx.SetBlockKnownShape(c.Support, b)
				compare(tx, c.Initial)
				change := c.Changes[0]
				handle = world.EntitySpawnOpts{Position: c.Feet}.New(Type, Config{Name: "RedstoneAudit", Position: c.Feet, Rotation: cube.Rotation{change.Rotation.Y, change.Rotation.X}, GameMode: world.GameModeSurvival})
				p := tx.AddEntity(handle).(*Player)
				held, ok := world.ItemByName(change.Held, 0)
				if !ok {
					t.Fatal("native door item unavailable")
				}
				p.SetHeldItems(item.NewStack(held, 2), item.Stack{})
			})
			applied := 0
			apply := func(tx *world.Tx) {
				compare(tx, c.Changes[0].Blocks)
				e, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("placement player unavailable")
				}
				p := e.(*Player)
				p.UseItemOnBlock(c.Support, cube.FaceUp, mgl64.Vec3{.5, 1, .5})
				if c.Accepted {
					compare(tx, c.Results[0].Blocks)
				}
				held, _ := p.HeldItems()
				if held.Count() != c.FinalHeld.Count {
					t.Errorf("held count=%d, native=%d", held.Count(), c.FinalHeld.Count)
				}
				applied++
			}
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native placement clock differs")
					}
					change := c.Changes[0]
					if change.Tick == f.Tick && change.BeforeSample {
						apply(tx)
					}
					compare(tx, f.Blocks)
					if change.Tick == f.Tick && !change.BeforeSample {
						apply(tx)
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Fatal("native placement input not replayed exactly once")
			}
		})
	}
}
