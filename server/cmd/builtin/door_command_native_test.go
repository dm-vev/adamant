package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

type doorCommandNativeBlock struct {
	ID     string
	States map[string]any
}

func doorCommandNativeKey(t *testing.T, b doorCommandNativeBlock) string {
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
		if legacy, exists := states["direction"]; exists {
			if legacy != map[string]float64{"south": 0, "west": 1, "north": 2, "east": 3}[d] {
				t.Fatal("native command direction aliases differ")
			}
			delete(states, "direction")
		}
	}
	if b.ID == "minecraft:stone" {
		if v, exists := states["stone_type"]; exists {
			if v != "stone" {
				t.Fatal("unexpected native stone alias")
			}
			delete(states, "stone_type")
		}
	}
	b.States = states
	data, _ := json.Marshal(b)
	return string(data)
}

func TestDoorSetBlockAndFillMatchNativeCommands(t *testing.T) {
	data, err := os.ReadFile("../../block/testdata/bedrock_1_26_51_door_commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Command, Context, Mode string
				Size                          cube.Pos
				Top                           bool
				Direction                     int
				Open, Hinge                   bool
			}
			Base           cube.Pos
			Positions      []cube.Pos
			SetupTick      int64 `json:"setup_tick"`
			Initial, After []doorCommandNativeBlock
			Requested      doorCommandNativeBlock
			SuccessCount   int `json:"success_count"`
			Samples        []struct {
				Tick   int64
				Blocks []doorCommandNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 384 {
		t.Fatal("incomplete native door command corpus")
	}
	for _, corpus := range []struct {
		name  string
		count int
	}{
		{"copper_variants", 16}, {"modes", 384},
	} {
		data, err := os.ReadFile("../../block/testdata/bedrock_1_26_51_door_command_" + corpus.name + ".json")
		if err != nil {
			t.Fatal(err)
		}
		extra := native
		extra.Scenarios, extra.Complete = nil, false
		if err := json.Unmarshal(data, &extra); err != nil {
			t.Fatal(err)
		}
		if !extra.Complete || len(extra.Scenarios) != corpus.count {
			t.Fatalf("incomplete native %s command corpus", corpus.name)
		}
		native.Scenarios = append(native.Scenarios, extra.Scenarios...)
	}
	volume, err := os.ReadFile("../../block/testdata/bedrock_1_26_51_door_volume_fill.json")
	if err != nil {
		t.Fatal(err)
	}
	extra := native
	extra.Scenarios, extra.Complete = nil, false
	if err := json.Unmarshal(volume, &extra); err != nil {
		t.Fatal(err)
	}
	if !extra.Complete || len(extra.Scenarios) != 45 {
		t.Fatal("incomplete native volume fill corpus")
	}
	native.Scenarios = append(native.Scenarios, extra.Scenarios...)
	world.DefaultBlockRegistry.Finalize()
	blocks := map[string]world.Block{}
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		id, states := b.EncodeBlock()
		blocks[doorCommandNativeKey(t, doorCommandNativeBlock{ID: id, States: states})] = b
	}
	decode := func(t *testing.T, b doorCommandNativeBlock) world.Block {
		t.Helper()
		v, ok := blocks[doorCommandNativeKey(t, b)]
		if !ok {
			t.Fatalf("native command block unavailable: %+v", b)
		}
		return v
	}
	setblock := worldTestCommand("setblock", setBlockCommand{})
	fill := worldTestCommand("fill", fillCommand{}, fillReplaceCommand{})
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%s/%s/%s/%v/top%t/direction%d", c.Scenario.Block, c.Scenario.Command, c.Scenario.Context, c.Scenario.Mode, c.Scenario.Size, c.Scenario.Top, c.Scenario.Direction), func(t *testing.T) {
			cells := 4
			if c.Scenario.Size[0] != 0 {
				cells = c.Scenario.Size[0] * (c.Scenario.Size[1] + 2) * c.Scenario.Size[2]
			}
			if len(c.Positions) != cells || len(c.Initial) != cells || len(c.After) != cells || len(c.Samples) != 8 {
				t.Fatal("native door command controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-2, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", false)
			// Drain the authored floor's initial notifications before applying
			// the Script API context permutations and command at their clock.
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := c.Base[0] - 2; x <= c.Base[0]+2; x++ {
					for z := c.Base[2] - 2; z <= c.Base[2]+2; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					}
				}
			})
			w.AdvanceTick()
			compare := func(tx *world.Tx, want []doorCommandNativeBlock) {
				t.Helper()
				if len(want) != cells {
					t.Fatal("native command frame cells differ")
				}
				for i, pos := range c.Positions {
					id, states := tx.Block(pos).EncodeBlock()
					g, v := doorCommandNativeKey(t, doorCommandNativeBlock{ID: id, States: states}), doorCommandNativeKey(t, want[i])
					if g != v {
						t.Errorf("tick %d at %v: got %s, native %s", tx.CurrentTick(), pos, g, v)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				if tx.CurrentTick() != c.SetupTick {
					t.Fatal("native command setup clock differs")
				}
				for i, pos := range c.Positions {
					b := decode(t, c.Initial[i])
					if world.BlockRuntimeID(tx.Block(pos)) != world.BlockRuntimeID(b) {
						// Match Script setPermutation through the public API,
						// independently of the command-fill callback.
						tx.SetBlock(pos, b, nil)
					}
				}
				// Translate only the argument syntax. Require the actual command
				// parser to yield the exact independently captured native input.
				name := c.Requested.ID
				if name == "minecraft:wooden_door" {
					name = "minecraft:oak_door"
				}
				half, hinge := "lower", "left"
				if c.Scenario.Top {
					half = "upper"
				}
				if c.Scenario.Hinge {
					hinge = "right"
				}
				spec := ""
				want := decode(t, c.Requested)
				for _, facing := range []string{"north", "east", "south", "west"} {
					candidate := fmt.Sprintf("%s[facing=%s,half=%s,hinge=%s,open=%t]", name, facing, half, hinge, c.Scenario.Open)
					b, _, err := commandBlock(w, candidate)
					if err == nil && world.BlockRuntimeID(b) == world.BlockRuntimeID(want) {
						spec = candidate
						break
					}
				}
				if spec == "" {
					t.Fatal("no command argument encodes the native requested permutation")
				}
				var command cmd.Command
				var args string
				coord := fmt.Sprintf("%d %d %d", c.Base[0], c.Base[1], c.Base[2])
				if c.Scenario.Command == "setblock" {
					command, args = setblock, coord+" "+spec
				} else {
					end := c.Base
					if c.Scenario.Size[0] != 0 {
						end = end.Add(c.Scenario.Size).Add(cube.Pos{-1, -1, -1})
					}
					command, args = fill, coord+fmt.Sprintf(" %d %d %d ", end[0], end[1], end[2])+spec
				}
				if c.Scenario.Mode != "" {
					args += " " + c.Scenario.Mode
				}
				out := runTestCommand(command, args, tx)
				if success := out.ErrorCount() == 0; success != (c.SuccessCount != 0) {
					t.Errorf("command success=%t, native=%d; errors=%v", success, c.SuccessCount, out.Errors())
				}
				compare(tx, c.After)
			})
			if t.Failed() {
				return
			}
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Error("native command sample clock differs")
					}
					compare(tx, f.Blocks)
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
