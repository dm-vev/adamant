package block

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type doorSupportNativeBlock struct {
	ID     string
	States map[string]any
}

func TestDoorSupportUsesTypedSnowPermutations(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		tx.SetBlockKnownShape(pos.Side(cube.FaceDown), Stone{})
		seen := map[uint32]bool{}
		for layers := uint8(1); layers <= 8; layers++ {
			for _, covered := range []bool{false, true} {
				want := SnowLayer{Layers: layers, Covered: covered}
				id, states := want.EncodeBlock()
				b, ok := world.BlockByName(id, states)
				if s, typed := b.(SnowLayer); !ok || !typed || s.Layers != layers || s.Covered != covered {
					t.Fatalf("snow permutation was not registered as typed snow: %v %v", id, states)
				}
				rid := world.BlockRuntimeID(b)
				if seen[rid] {
					t.Fatal("snow permutations share a runtime ID")
				}
				seen[rid] = true
				tx.SetBlockKnownShape(pos, b)
				if got := doorSupportedBy(pos, tx); got != (layers == 8) {
					t.Errorf("snow layers=%d covered=%t support=%t", layers, covered, got)
				}
			}
		}
	})
}

func doorSupportNativeDecode(t *testing.T, b doorSupportNativeBlock) world.Block {
	t.Helper()
	states := map[string]any{}
	for k, v := range b.States {
		states[k] = v
	}
	if direction, ok := states["minecraft:cardinal_direction"].(string); ok {
		if alias, exists := states["direction"]; exists {
			if alias != map[string]float64{"south": 0, "west": 1, "north": 2, "east": 3}[direction] {
				t.Fatal("native door direction aliases differ")
			}
			delete(states, "direction")
		}
	}
	if b.ID == "minecraft:stone" {
		if alias, exists := states["stone_type"]; exists {
			if alias != "stone" {
				t.Fatal("unexpected native stone alias")
			}
			delete(states, "stone_type")
		}
	}
	alias := func(name string, want any) {
		if got, exists := states[name]; exists {
			if got != want {
				t.Fatalf("native %s alias %s=%v, want %v", b.ID, name, got, want)
			}
			delete(states, name)
		}
	}
	if strings.HasPrefix(b.ID, "minecraft:oak_") {
		alias("wood_type", "oak")
	}
	if b.ID == "minecraft:oak_leaves" {
		alias("old_leaf_type", "oak")
	}
	if b.ID == "minecraft:oak_log" {
		alias("old_log_type", "oak")
	}
	if b.ID == "minecraft:spruce_log" {
		alias("old_log_type", "spruce")
	}
	if b.ID == "minecraft:cobblestone_wall" {
		alias("wall_block_type", "cobblestone")
	}
	if b.ID == "minecraft:white_carpet" {
		alias("color", "white")
	}
	if half, ok := states["minecraft:vertical_half"].(string); ok {
		alias("top_slot_bit", half == "top")
	}
	for name, value := range states {
		if number, ok := value.(float64); ok {
			if float64(int32(number)) != number {
				t.Fatalf("non-integer native state %s=%v", name, number)
			}
			states[name] = int32(number)
		}
	}
	v, ok := world.BlockByName(b.ID, states)
	if !ok {
		t.Fatalf("native door support state unavailable: %+v", b)
	}
	return v
}

func TestDoorSurvivalMatchesNativeNeighbourPhases(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_door_support.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Action, Replacement string
				Phase                      int
				Top                        bool
				Drops, Foundation          bool
				Support                    struct {
					ID     string
					States map[string]any
				}
			}
			Base           cube.Pos
			Positions      []cube.Pos
			SetupTick      int64 `json:"setup_tick"`
			ChangeTick     int64 `json:"change_tick"`
			Initial, After []doorSupportNativeBlock
			SupportInput   doorSupportNativeBlock `json:"support_input"`
			Samples        []struct {
				Tick   int64
				Blocks []doorSupportNativeBlock
				Items  []struct {
					ID    string
					Count int
				}
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 48 {
		t.Fatal("incomplete native door survival corpus")
	}
	api, err := os.ReadFile("testdata/bedrock_1_26_51_copper_door_api.json")
	if err != nil {
		t.Fatal(err)
	}
	extra := native
	extra.Scenarios, extra.Complete = nil, false
	if err := json.Unmarshal(api, &extra); err != nil {
		t.Fatal(err)
	}
	if !extra.Complete || len(extra.Scenarios) != 6 {
		t.Fatal("incomplete native copper API replacement corpus")
	}
	native.Scenarios = append(native.Scenarios, extra.Scenarios...)
	for _, corpus := range []struct {
		name  string
		count int
	}{{"drops", 96}, {"support_surfaces", 132}} {
		data, err := os.ReadFile("testdata/bedrock_1_26_51_door_" + corpus.name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		extra := native
		extra.Scenarios, extra.Complete = nil, false
		if err := json.Unmarshal(data, &extra); err != nil {
			t.Fatal(err)
		}
		if !extra.Complete || len(extra.Scenarios) != corpus.count {
			t.Fatalf("incomplete native %s corpus", corpus.name)
		}
		native.Scenarios = append(native.Scenarios, extra.Scenarios...)
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%s/%s/%v/drops%t/phase%d", c.Scenario.Block, c.Scenario.Action, c.Scenario.Support.ID, c.Scenario.Support.States, c.Scenario.Drops, c.Scenario.Phase), func(t *testing.T) {
			if len(c.Positions) != 4 || len(c.Initial) != 4 || len(c.After) != 4 || c.ChangeTick&1 != int64(c.Scenario.Phase) || len(c.Samples) < 16 {
				t.Fatal("native survival controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			dropCounts := map[string]int{}
			entities := world.EntityRegistryConfig{Item: func(opts world.EntitySpawnOpts, value any) *world.EntityHandle {
				stack := value.(item.Stack)
				id, _ := stack.Item().EncodeItem()
				dropCounts[id] += stack.Count()
				// This test compares native drop identities/counts and their
				// simulation tick, independently of item physics and merging.
				return opts.New(redstoneTNTTestEntityType{}, redstoneTNTTestEntityConfig{})
			}}.New([]world.EntityType{redstoneTNTTestEntityType{}})
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}, Entities: entities}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", c.Scenario.Drops)
			compare := func(tx *world.Tx, expected []doorSupportNativeBlock) {
				t.Helper()
				if len(expected) != 4 {
					t.Fatal("native survival frame cells differ")
				}
				for i, pos := range c.Positions {
					want := doorSupportNativeDecode(t, expected[i])
					if world.BlockRuntimeID(tx.Block(pos)) != world.BlockRuntimeID(want) {
						id, states := tx.Block(pos).EncodeBlock()
						t.Errorf("tick %d at %v: %s %v, native %+v", tx.CurrentTick(), pos, id, states, expected[i])
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				if tx.CurrentTick() != c.SetupTick {
					t.Fatal("native survival setup clock differs")
				}
				// The native setup uses /setblock for the lower half. Its fill
				// callback creates the upper half with flags 2, without notifying
				// the lower half as a separate API upper placement would.
				if c.Scenario.Foundation {
					tx.SetBlockKnownShape(c.Positions[0].Side(cube.FaceDown), Stone{})
				}
				tx.SetBlockKnownShape(c.Positions[0], doorSupportNativeDecode(t, c.Initial[0]))
				tx.SetBlockFromCommand(c.Base, doorSupportNativeDecode(t, c.Initial[1]), world.BlockState{})
				compare(tx, c.Initial)
			})
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native survival sample clock differs")
					}
					compare(tx, f.Blocks)
					expectedDrops := map[string]int{}
					for _, it := range f.Items {
						expectedDrops[it.ID] += it.Count
					}
					if !maps.Equal(dropCounts, expectedDrops) {
						t.Errorf("tick %d drops=%v, native=%v", tx.CurrentTick(), dropCounts, expectedDrops)
					}
					if f.Tick != c.ChangeTick {
						return
					}
					below, lower, upper := c.Positions[0], c.Positions[1], c.Positions[2]
					switch c.Scenario.Action {
					case "support_remove", "support_restore":
						tx.SetBlockKnownShape(below, Air{})
						if c.Scenario.Action == "support_restore" {
							tx.SetBlockKnownShape(below, Stone{})
						}
					case "upper_remove":
						tx.SetBlockKnownShape(upper, Air{})
					case "lower_remove":
						tx.SetBlockKnownShape(lower, Air{})
					case "other_material":
						b := c.Initial[2]
						b.ID = "minecraft:spruce_door"
						tx.SetBlockKnownShape(upper, doorSupportNativeDecode(t, b))
					case "two_lowers":
						tx.SetBlockKnownShape(upper, doorSupportNativeDecode(t, c.Initial[1]))
					case "two_uppers":
						tx.SetBlockKnownShape(lower, doorSupportNativeDecode(t, c.Initial[2]))
					case "side_floating":
						tx.SetBlockKnownShape(c.Base.Side(cube.FaceEast), Stone{})
					case "variant":
						i := 1
						if c.Scenario.Top {
							i = 2
						}
						b := c.Initial[i]
						b.ID = "minecraft:" + c.Scenario.Replacement
						tx.SetBlockKnownShape(c.Positions[i], doorSupportNativeDecode(t, b))
					case "surface":
						if c.SupportInput.ID != "minecraft:"+c.Scenario.Support.ID {
							t.Fatal("native requested support input missing")
						}
						tx.SetBlockKnownShape(below, doorSupportNativeDecode(t, c.SupportInput))
					default:
						t.Fatal("unknown native survival action")
					}
					compare(tx, c.After)
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
