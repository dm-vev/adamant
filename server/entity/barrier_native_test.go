package entity

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestBarrierConsumerPulsesMatchBDS(t *testing.T) {
	testBarrierConsumerNative(t, "bedrock_1_26_51_barrier_pulses.json", 108, 2, 13, false)
}

func TestBarrierConsumerSignalChangesMatchBDS(t *testing.T) {
	testBarrierConsumerNative(t, "bedrock_1_26_51_barrier_signals.json", 120, 4, 15, true)
}

func testBarrierConsumerNative(t *testing.T, file string, frames, cells, changeCount int, signal bool) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Kind, Form, Feed, Path string
				Initial, Powered       bool
				Phase, Height          int
			}
			Base               cube.Pos
			Positions, Sources []cube.Pos
			SetupTick          int64 `json:"setup_tick"`
			Initial            []pistonEntityNativeBlock
			InitialSources     []pistonEntityNativeBlock `json:"initial_sources"`
			Inputs             []struct {
				Position cube.Pos
				Block    pistonEntityNativeBlock
			}
			Changes []struct {
				Tick     int64
				Position cube.Pos
				Block    pistonEntityNativeBlock
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
	if !native.Complete || len(native.Scenarios) != 192 {
		t.Fatal("incomplete native barrier corpus")
	}
	decode := windMaterialBlocks(t)
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/initial%t/powered%t/%s/%s/height%d/phase%d", c.Scenario.Kind, c.Scenario.Initial, c.Scenario.Powered, c.Scenario.Feed, c.Scenario.Path, c.Scenario.Height, c.Scenario.Phase), func(t *testing.T) {
			if len(c.Samples) != frames || len(c.Positions) != cells || len(c.Initial) != cells || len(c.Sources) != len(c.InitialSources) || len(c.Changes) != changeCount || c.Changes[0].Tick%2 != int64(c.Scenario.Phase) {
				t.Fatal("native barrier input controls differ")
			}
			if signal && len(c.Inputs) != 13 && len(c.Inputs) != 14 {
				t.Fatal("native signal input geometry missing")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("spawn_mobs", false)
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				stone := decode(pistonEntityNativeBlock{ID: "minecraft:stone", States: map[string]any{}})
				minimumX := c.Base[0] - 3
				if signal {
					minimumX = c.Base[0] - 7
				}
				for x := minimumX; x <= c.Base[0]+3; x++ {
					for z := c.Base[2] - 3; z <= c.Base[2]+3; z++ {
						tx.SetBlock(cube.Pos{x, 64, z}, stone, nil)
					}
				}
				for i, pos := range c.Sources {
					tx.SetBlock(pos, decode(c.InitialSources[i]), nil)
				}
				for _, input := range c.Inputs {
					tx.SetBlock(input.Position, decode(input.Block), nil)
				}
				for i, pos := range c.Positions[:2] {
					tx.SetBlock(pos, decode(c.Initial[i]), nil)
				}
			})
			changes := 0
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick || len(f.Blocks) != cells {
						t.Fatal("native barrier clock/cells differ")
					}
					for i, pos := range c.Positions {
						id, states := tx.Block(pos).EncodeBlock()
						got := windMaterialState(t, pistonEntityNativeBlock{ID: id, States: states})
						want := windMaterialState(t, f.Blocks[i])
						if !bytes.Equal(got, want) {
							t.Errorf("tick %d (+%d) at %v: got %s, native %s", f.Tick, f.Tick-c.SetupTick, pos, got, want)
						}
					}
					for _, change := range c.Changes {
						if change.Tick == f.Tick {
							tx.SetBlock(change.Position, decode(change.Block), &world.SetOpts{CommandFill: true})
							changes++
						}
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && changes != len(c.Changes) {
				t.Fatal("native source changes were not replayed")
			}
		})
	}
}
