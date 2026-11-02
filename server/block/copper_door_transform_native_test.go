package block

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type doorOxidationZeroSource struct{}

func (doorOxidationZeroSource) Uint64() uint64 { return 0 }

// Force the oxidation decision, independently of its unaudited probability,
// so a queued survival check cannot tear apart the naturally changed pair.
func TestCopperDoorOxidationKeepsBothHalves(t *testing.T) {
	for _, top := range []bool{false, true} {
		t.Run(fmt.Sprint(top), func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			defer w.Close()
			_ = w.SetGameRule("block_drops", false)
			base := cube.Pos{0, 65, 0}
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlockKnownShape(base.Side(cube.FaceDown), Stone{})
				tx.SetBlockFromCommand(base, CopperDoor{Facing: cube.West, Right: true}, world.BlockState{})
				pos := base
				if top {
					pos = pos.Side(cube.FaceUp)
				}
				tx.Block(pos).(CopperDoor).RandomTick(pos, tx, rand.New(doorOxidationZeroSource{}))
			})
			for range 3 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				for half := 0; half < 2; half++ {
					d, ok := tx.Block(base.Add(cube.Pos{0, half, 0})).(CopperDoor)
					if !ok || d.Oxidation != ExposedOxidation() || d.Top != (half == 1) || d.Facing != cube.West || !d.Right {
						t.Fatalf("oxidation lost the coupled half %d: %+v", half, tx.Block(base.Add(cube.Pos{0, half, 0})))
					}
				}
			})
		})
	}
}

// Native observations use real sneaking player input. This replay selects
// the corresponding item-use path and checks its world mutations and survival
// callbacks. Packet fields and the player dispatch itself are separate tests.
func TestCopperDoorItemTransformsMatchNativeStates(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_copper_door_transform.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Block, Tool string
				Top         bool
			}
			Base, Target   cube.Pos
			Feet           mgl64.Vec3
			Positions      []cube.Pos
			SetupTick      int64 `json:"setup_tick"`
			Initial, Final []doorSupportNativeBlock
			Changes        []struct {
				Tick         int64
				Item         string
				Sneaking     bool
				BeforeSample bool `json:"before_sample"`
				Blocks       []doorSupportNativeBlock
			}
			Results []struct {
				Tick   int64
				Blocks []doorSupportNativeBlock
			}
			Samples []struct {
				Tick   int64
				Blocks []doorSupportNativeBlock
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 22 {
		t.Fatal("incomplete actual copper item-use corpus")
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%s/%s/top%t", c.Scenario.Block, c.Scenario.Tool, c.Scenario.Top), func(t *testing.T) {
			if len(c.Changes) != 1 || len(c.Results) != 1 || len(c.Initial) != 2 || len(c.Final) != 2 || len(c.Positions) != 2 || len(c.Samples) < 30 {
				t.Fatal("native copper item-use controls differ")
			}
			change := c.Changes[0]
			if !change.Sneaking || change.Item != "minecraft:"+c.Scenario.Tool || c.Results[0].Tick != change.Tick {
				t.Fatal("native input did not exercise the specified sneaking item use")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick, settings.WeatherCycle = c.SetupTick-1, false
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			_ = w.SetGameRule("block_drops", false)
			compare := func(tx *world.Tx, expected []doorSupportNativeBlock) {
				t.Helper()
				if len(expected) != 2 {
					t.Fatal("native copper frame cells differ")
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
				tx.SetBlockKnownShape(c.Base.Side(cube.FaceDown), Stone{})
				tx.SetBlockFromCommand(c.Base, doorSupportNativeDecode(t, c.Initial[0]), world.BlockState{})
				compare(tx, c.Initial)
			})
			apply := func() {
				<-w.Exec(func(tx *world.Tx) {
					compare(tx, change.Blocks)
					ctx := &item.UseContext{}
					user := &pistonTestUser{pos: c.Feet}
					var ok bool
					tx.WithBlockInteraction(func() {
						switch c.Scenario.Tool {
						case "honeycomb":
							ok = (item.Honeycomb{}).UseOnBlock(c.Target, cube.FaceSouth, mgl64.Vec3{.5, .5, .5}, tx, user, ctx)
						case "iron_axe":
							ok = (item.Axe{Tier: item.ToolTierIron}).UseOnBlock(c.Target, cube.FaceSouth, mgl64.Vec3{.5, .5, .5}, tx, user, ctx)
						default:
							t.Fatal("unknown native copper tool")
						}
					})
					if !ok {
						t.Error("native accepted item-use path was rejected")
					}
					compare(tx, c.Results[0].Blocks)
				})
			}
			for _, f := range c.Samples {
				if f.Tick == change.Tick && change.BeforeSample {
					apply()
				}
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != f.Tick {
						t.Fatal("native copper sample clock differs")
					}
					compare(tx, f.Blocks)
				})
				if f.Tick == change.Tick && !change.BeforeSample {
					apply()
				}
				if t.Failed() {
					break
				}
			}
			<-w.Exec(func(tx *world.Tx) { compare(tx, c.Final) })
		})
	}
}
