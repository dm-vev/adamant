package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestLeafDecayFlagsMatchNativeSourceChanges(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_leaf_decay_neighbours.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Action     string
				Persistent bool
			}
			Base            cube.Pos
			Positions       []cube.Pos
			SourcePositions []cube.Pos               `json:"source_positions"`
			SetupTick       int64                    `json:"setup_tick"`
			InitialSource   []doorSupportNativeBlock `json:"initial_source"`
			InitialLeaves   []doorSupportNativeBlock `json:"initial_leaves"`
			RequestedSource doorSupportNativeBlock   `json:"requested_source"`
			AfterSource     []doorSupportNativeBlock `json:"after_source"`
			AfterLeaves     []doorSupportNativeBlock `json:"after_leaves"`
			Samples         []struct {
				Tick           int64
				Source, Leaves []doorSupportNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 16 {
		t.Fatal("incomplete native decay-source corpus")
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/persistent%t", c.Scenario.Action, c.Scenario.Persistent), func(t *testing.T) {
			if len(c.Positions) != 9 || len(c.SourcePositions) != 2 || len(c.Samples) != 6 {
				t.Fatal("native decay controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", false)
			compare := func(tx *world.Tx, positions []cube.Pos, expected []doorSupportNativeBlock) {
				t.Helper()
				if len(positions) != len(expected) {
					t.Fatal("native decay frame cells differ")
				}
				for i, pos := range positions {
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
					t.Fatal("native decay setup clock differs")
				}
				b := doorSupportNativeDecode(t, c.InitialSource[0])
				if _, door := b.(WoodDoor); door {
					tx.SetBlockKnownShape(c.Base.Side(cube.FaceDown), Stone{})
					tx.SetBlockFromCommand(c.Base, b, world.BlockState{})
				} else {
					tx.SetBlockKnownShape(c.Base, b)
				}
				for i, pos := range c.Positions {
					tx.SetBlockKnownShape(pos, doorSupportNativeDecode(t, c.InitialLeaves[i]))
				}
				compare(tx, c.SourcePositions, c.InitialSource)
				compare(tx, c.Positions, c.InitialLeaves)
				tx.SetBlockKnownShape(c.Base, doorSupportNativeDecode(t, c.RequestedSource))
				compare(tx, c.SourcePositions, c.AfterSource)
				compare(tx, c.Positions, c.AfterLeaves)
			})
			if t.Failed() {
				return
			}
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native decay frame clock differs")
					}
					compare(tx, c.SourcePositions, f.Source)
					compare(tx, c.Positions, f.Leaves)
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
