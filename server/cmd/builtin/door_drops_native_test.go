package builtin

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
	"github.com/df-mc/dragonfly/server/world"
)

type doorCommandDrop struct {
	ID    string
	Count int
}
type doorCommandDropViewer struct {
	world.NopViewer
	drops map[string]int
}

func (v *doorCommandDropViewer) ViewEntity(e world.Entity) {
	if ent, ok := e.(*entity.Ent); ok {
		if b, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
			s := b.Item()
			id, _ := s.Item().EncodeItem()
			v.drops[id] += s.Count()
		}
	}
}

func TestDoorDestroyCommandsMatchNativeResources(t *testing.T) {
	data, err := os.ReadFile("../../block/testdata/bedrock_1_26_51_door_command_drops.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Command string
				Top, Drops     bool
				Phase          int
			}
			Base           cube.Pos
			Positions      []cube.Pos
			SetupTick      int64  `json:"setup_tick"`
			ChangeTick     int64  `json:"change_tick"`
			CommandInput   string `json:"command_input"`
			SuccessCount   int    `json:"success_count"`
			Initial, After []doorCommandNativeBlock
			Samples        []struct {
				Tick   int64
				Blocks []doorCommandNativeBlock
				Items  []doorCommandDrop
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 48 {
		t.Fatal("incomplete native command door resource corpus")
	}
	setblock := worldTestCommand("setblock", setBlockCommand{})
	fill := worldTestCommand("fill", fillCommand{}, fillReplaceCommand{})
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%s/top%t/drops%t/phase%d", c.Scenario.Block, c.Scenario.Command, c.Scenario.Top, c.Scenario.Drops, c.Scenario.Phase), func(t *testing.T) {
			if len(c.Positions) != 4 || len(c.Initial) != 4 || len(c.After) != 4 || c.ChangeTick&1 != int64(c.Scenario.Phase) {
				t.Fatal("native command resource controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			viewer := &doorCommandDropViewer{drops: map[string]int{}}
			loader := world.NewLoader(2, w, viewer)
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", c.Scenario.Drops)
			compare := func(tx *world.Tx, want []doorCommandNativeBlock) {
				t.Helper()
				if len(want) != 4 {
					t.Fatal("native resource frame cells differ")
				}
				for i, pos := range c.Positions {
					id, states := tx.Block(pos).EncodeBlock()
					got, expected := doorCommandNativeKey(t, doorCommandNativeBlock{ID: id, States: states}), doorCommandNativeKey(t, want[i])
					if got != expected {
						t.Errorf("tick %d at %v: %s, native %s", tx.CurrentTick(), pos, got, expected)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				tx.SetBlockKnownShape(c.Positions[0], block.Stone{})
				b, ok := world.BlockByName(c.Initial[1].ID, map[string]any{"minecraft:cardinal_direction": "north", "door_hinge_bit": false, "upper_block_bit": false, "open_bit": false})
				if !ok {
					t.Fatal("native door unavailable")
				}
				tx.SetBlockFromCommand(c.Base, b, world.BlockState{})
				compare(tx, c.Initial)
			})
			applied := 0
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native command drop clock differs")
					}
					compare(tx, f.Blocks)
					want := map[string]int{}
					for _, it := range f.Items {
						want[it.ID] += it.Count
					}
					if !maps.Equal(viewer.drops, want) {
						t.Errorf("tick %d resources=%v, native=%v", f.Tick, viewer.drops, want)
					}
					if f.Tick == c.ChangeTick {
						command := setblock
						if c.Scenario.Command == "fill" {
							command = fill
						}
						prefix := c.Scenario.Command + " "
						if !strings.HasPrefix(c.CommandInput, prefix) {
							t.Fatal("native command input differs")
						}
						out := runTestCommand(command, strings.TrimPrefix(c.CommandInput, prefix), tx)
						if (out.ErrorCount() == 0) != (c.SuccessCount != 0) {
							t.Errorf("command success=%t, native=%d", out.ErrorCount() == 0, c.SuccessCount)
						}
						compare(tx, c.After)
						applied++
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Fatal("native destroy command not replayed exactly once")
			}
		})
	}
}
