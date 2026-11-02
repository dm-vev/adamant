package player

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type doorBreakNativeItem struct {
	ID       string
	Count    int
	Position struct{ X, Y, Z float64 }
}

type doorBreakNativeViewer struct {
	world.NopViewer
	drops map[string]int
}

func (v *doorBreakNativeViewer) ViewEntity(e world.Entity) {
	if ent, ok := e.(*entity.Ent); ok {
		if b, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
			stack := b.Item()
			id, _ := stack.Item().EncodeItem()
			v.drops[fmt.Sprintf("%s@%v", id, cube.PosFromVec3(e.Position()))] += stack.Count()
		}
	}
}

// Compare immediate paired removal and resource counts through the real player
// break path. The independent API corpus checks the deferred survival cascade.
// Item trajectories and the exact random spawn coordinates are outside this check.
func TestDoorPlayerBreakMatchesNativeResources(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_door_player_break.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Tool string
				GameMode    string `json:"game_mode"`
				Top, Drops  bool
			}
			Base, Target cube.Pos
			Positions    []cube.Pos
			Feet         mgl64.Vec3
			SetupTick    int64 `json:"setup_tick"`
			Requested    int64
			Initial      []tripwireBreakNativeBlock
			Changes      []struct {
				Tick         int64
				BeforeSample bool `json:"before_sample"`
				Held         string
				Blocks       []tripwireBreakNativeBlock
			}
			Results []struct{ Blocks []tripwireBreakNativeBlock }
			Spawns  []struct {
				Tick int64
				Body doorBreakNativeItem
			}
			Samples []struct {
				Tick   int64
				Blocks []tripwireBreakNativeBlock
				Items  []doorBreakNativeItem
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 36 {
		t.Fatal("incomplete native player door break corpus")
	}
	data, err = os.ReadFile("../block/testdata/bedrock_1_26_51_door_player_break_empty_hand.json")
	if err != nil {
		t.Fatal(err)
	}
	extra := native
	extra.Complete, extra.Scenarios = false, nil
	if err := json.Unmarshal(data, &extra); err != nil {
		t.Fatal(err)
	}
	if !extra.Complete || len(extra.Scenarios) != 6 {
		t.Fatal("incomplete native empty-hand door break corpus")
	}
	native.Scenarios = append(native.Scenarios, extra.Scenarios...)
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/top%t/%s/%s/drops%t", c.Scenario.Block, c.Scenario.Top, c.Scenario.GameMode, c.Scenario.Tool, c.Scenario.Drops), func(t *testing.T) {
			if len(c.Changes) != 1 || len(c.Results) != 1 || len(c.Positions) != 2 || len(c.Initial) != 2 || len(c.Samples) < 20 {
				t.Fatal("native player door break controls differ")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			viewer := &doorBreakNativeViewer{drops: map[string]int{}}
			loader := world.NewLoader(2, w, viewer)
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", c.Scenario.Drops)
			_ = w.SetGameRule("spawn_mobs", false)
			var handle *world.EntityHandle
			compare := func(tx *world.Tx, expected []tripwireBreakNativeBlock) {
				t.Helper()
				if len(expected) != 2 {
					t.Fatal("native door frame cells differ")
				}
				for i, pos := range c.Positions {
					id, states := tx.Block(pos).EncodeBlock()
					got, want := barrierPlacementState(t, tripwireBreakNativeBlock{ID: id, States: states}), barrierPlacementState(t, expected[i])
					if got != want {
						t.Errorf("tick %d at %v: %s, native %s", tx.CurrentTick(), pos, got, want)
					}
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, c.Base.Vec3Centre())
				loader.Load(tx, 25)
				for x := -3; x <= 3; x++ {
					for z := -3; z <= 3; z++ {
						tx.SetBlockKnownShape(cube.Pos{x, 64, z}, block.Stone{})
					}
				}
				b, ok := world.BlockByName(c.Initial[0].ID, map[string]any{"minecraft:cardinal_direction": "north", "open_bit": false, "door_hinge_bit": false, "upper_block_bit": false})
				if !ok {
					t.Fatal("native door type unavailable")
				}
				tx.SetBlockFromCommand(c.Base, b, world.BlockState{})
				compare(tx, c.Initial)
				if c.Scenario.GameMode == "survival" && c.Scenario.Tool == "" {
					want := time.Duration(c.Changes[0].Tick-c.Requested) * time.Second / 20
					if got := block.BreakDuration(b, item.Stack{}, block.BreakContext{}); got != want {
						t.Errorf("empty-hand duration=%v, native=%v", got, want)
					}
				}
				var mode world.GameMode = world.GameModeSurvival
				if c.Scenario.GameMode == "creative" {
					mode = world.GameModeCreative
				}
				handle = world.EntitySpawnOpts{Position: c.Feet}.New(Type, Config{Name: "RedstoneAudit", Position: c.Feet, GameMode: mode})
				p := tx.AddEntity(handle).(*Player)
				if c.Scenario.Tool != "" {
					tool, ok := world.ItemByName("minecraft:"+c.Scenario.Tool, 0)
					if !ok {
						t.Fatal("native tool unavailable")
					}
					p.SetHeldItems(item.NewStack(tool, 1), item.Stack{})
				}
			})
			applied := 0
			apply := func(tx *world.Tx) {
				compare(tx, c.Changes[0].Blocks)
				e, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("break player missing")
				}
				e.(*Player).BreakBlock(c.Target)
				compare(tx, c.Results[0].Blocks)
				applied++
			}
			for _, f := range c.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native break clock differs")
					}
					change := c.Changes[0]
					if change.Tick == f.Tick && change.BeforeSample {
						apply(tx)
					}
					compare(tx, f.Blocks)
					want := map[string]int{}
					for _, spawn := range c.Spawns {
						if spawn.Tick <= f.Tick {
							body := spawn.Body
							cell := cube.PosFromVec3(mgl64.Vec3{body.Position.X, body.Position.Y, body.Position.Z})
							want[fmt.Sprintf("%s@%v", body.ID, cell)] += body.Count
						}
					}
					if !maps.Equal(viewer.drops, want) {
						t.Errorf("tick %d resources=%v, native=%v", f.Tick, viewer.drops, want)
					}
					if change.Tick == f.Tick && !change.BeforeSample {
						apply(tx)
					}
				})
				if t.Failed() {
					break
				}
			}
			if !t.Failed() && applied != 1 {
				t.Fatal("native accepted player break not replayed once")
			}
		})
	}
}
