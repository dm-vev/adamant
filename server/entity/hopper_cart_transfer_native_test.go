package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestHopperCartContainerTransfersMatchBDS(t *testing.T) {
	replayHopperCartTransfersBDS(t, "bedrock_1_26_51_hopper_cart_transfer.json", 24)
}

func TestHopperCartItemCollectionMatchesBDS(t *testing.T) {
	replayHopperCartTransfersBDS(t, "bedrock_1_26_51_hopper_cart_items.json", 24)
}

func replayHopperCartTransfersBDS(t *testing.T, filename string, count int) {
	data, err := os.ReadFile("../block/testdata/" + filename)
	if err != nil {
		t.Fatal(err)
	}
	type snapshot struct {
		Tick                      int64
		Cart, Source, Destination *int
		WorldItems                *int `json:"world_items"`
		Rail                      pistonEntityNativeBlock
		Position                  pistonEntityNativeVec
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Mode                string
				Source, Destination bool
				Initial             int
				WorldItemCount      int `json:"world_item_count"`
			}
			Base, Above, Below cube.Pos
			SetupTick          int64 `json:"setup_tick"`
			Spawn              snapshot
			Samples            []snapshot
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != count {
		t.Fatal("incomplete native hopper cart transfer capture")
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("%s/source%t/dest%t/initial%d/drop%d", c.Mode, c.Source, c.Destination, c.Initial, c.WorldItemCount), func(t *testing.T) {
			if len(trace.Samples) != 200 || trace.Spawn.Tick != trace.SetupTick+40 || trace.Spawn.Cart == nil || *trace.Spawn.Cart != c.Initial {
				t.Fatal("invalid native hopper transfer controls")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.SetupTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
			loader := world.NewLoader(2, w, world.NopViewer{})
			defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
			if err := w.SetGameRule("spawn_mobs", false); err != nil {
				t.Fatal(err)
			}
			p := trace.Base
			<-w.Exec(func(tx *world.Tx) {
				loader.Move(tx, p.Vec3Centre())
				loader.Load(tx, 25)
				for x := -3; x <= 3; x++ {
					for z := -4; z <= 4; z++ {
						tx.SetBlock(p.Add(cube.Pos{x, -1, z}), block.Bedrock{}, nil)
					}
				}
				if c.Destination {
					tx.SetBlock(trace.Below, block.NewBarrel(), nil)
				}
				if c.Source {
					tx.SetBlock(trace.Above, block.NewBarrel(), nil)
					if err := tx.Block(trace.Above).(block.Container).Inventory(tx, trace.Above).SetItem(0, item.NewStack(item.Diamond{}, 64)); err != nil {
						t.Fatal(err)
					}
				}
				if c.Mode == "activator_on" {
					tx.SetBlock(p.Side(cube.FaceEast), block.RedstoneBlock{}, nil)
				}
				for z := -3; z <= 3; z++ {
					var r world.Block = block.ActivatorRail{}
					if c.Mode == "rail" {
						r = block.Rail{}
					}
					tx.SetBlock(p.Add(cube.Pos{0, 0, z}), r, nil)
				}
			})
			var handle, dropped *world.EntityHandle
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Fatal("native hopper transfer clock mismatch")
					}
					comparePistonEntityNativeBlock(t, tx, frame.Tick, p, frame.Rail)
					if frame.Cart != nil {
						if handle == nil {
							t.Fatal("native transfer precedes cart spawn")
						}
						e, ok := handle.Entity(tx)
						if !ok {
							t.Fatal("hopper cart disappeared")
						}
						got := inventoryCount(e.(*MinecartContainer).Inventory().Slots())
						if got != *frame.Cart {
							t.Errorf("tick %d cart=%d, native=%d", frame.Tick, got, *frame.Cart)
						}
					}
					for _, container := range []struct {
						pos  cube.Pos
						want *int
						name string
					}{{trace.Above, frame.Source, "source"}, {trace.Below, frame.Destination, "destination"}} {
						if container.want == nil {
							continue
						}
						got := inventoryCount(tx.Block(container.pos).(block.Container).Inventory(tx, container.pos).Slots())
						if got != *container.want {
							t.Errorf("tick %d %s=%d, native=%d", frame.Tick, container.name, got, *container.want)
						}
					}
					if frame.WorldItems != nil {
						got := 0
						for e := range tx.Entities() {
							if e.H().Type() == ItemType {
								got += e.(*Ent).Behaviour().(*ItemBehaviour).Item().Count()
							}
						}
						if got != *frame.WorldItems {
							t.Errorf("tick %d dropped=%d, native=%d", frame.Tick, got, *frame.WorldItems)
						}
						if *frame.WorldItems > 0 && dropped != nil && dropped.Closed() {
							t.Error("partially collected item was replaced")
						}
					}
					if frame.Tick == trace.Spawn.Tick {
						handle = world.EntitySpawnOpts{Position: trace.Spawn.Position.vec()}.New(HopperMinecartType, hopperMinecartConf)
						e := tx.AddEntity(handle).(*MinecartContainer)
						if c.Initial > 0 {
							n, err := e.Inventory().AddItem(item.NewStack(item.Diamond{}, c.Initial))
							if n != c.Initial || err != nil {
								t.Fatal("native initial cart load did not fit", n, err)
							}
						}
						if c.WorldItemCount > 0 {
							dropped = NewItem(world.EntitySpawnOpts{Position: p.Vec3Centre().Sub(mgl64.Vec3{0, .125, 0})}, item.NewStack(item.Diamond{}, c.WorldItemCount))
							tx.AddEntity(dropped)
						}
					}
				})
				if t.Failed() {
					break
				}
			}
		})
	}
}
