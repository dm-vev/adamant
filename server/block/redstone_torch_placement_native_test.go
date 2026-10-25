package block

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type torchPlacementTestUser struct {
	pistonTestUser
	tx *world.Tx
}

func (u *torchPlacementTestUser) PlaceBlock(pos cube.Pos, b world.Block, ctx *item.UseContext) {
	u.tx.SetBlock(pos, b, nil)
	ctx.CountSub++
}

// The client placement path selects the default torch variant and its facing;
// BDS supplies initialization and world-publication traces for that permutation.
// Exercise UseOnBlock so an early attachment-power check cannot hide a mismatch
// by directly installing the expected lit block in this replay.
func TestTorchPlacementMatchesNativeCapture(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_torch_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	type sample struct {
		Tick                         int64
		Torch, Lamp, Support, Source torchNetworkBlock
	}
	var reference struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Face             string
				Mode             string
				InitiallyPowered bool
				Phase            int
			}
			Torch, Lamp, Support, Source cube.Pos
			PreparedTick                 int64 `json:"prepared_tick"`
			Placement                    sample
			Samples                      []sample
			Changes                      []struct {
				Tick    int64
				Kind    string
				Pos     cube.Pos
				Powered bool
			}
		}
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	if !reference.Complete || len(reference.Scenarios) != 60 {
		t.Fatal("incomplete native placement capture")
	}
	faces := map[string]cube.Face{"top": cube.FaceDown, "north": cube.FaceNorth, "south": cube.FaceSouth, "west": cube.FaceWest, "east": cube.FaceEast}
	for _, trace := range reference.Scenarios {
		c := trace.Scenario
		mode := c.Mode
		t.Run(fmt.Sprintf("%s/%s/powered%v/phase%d", c.Face, mode, c.InitiallyPowered, c.Phase), func(t *testing.T) {
			facing, ok := faces[c.Face]
			if !ok || len(trace.Samples) != 40 || len(trace.Changes) != 3 || trace.Changes[0].Kind != "setup" {
				t.Fatal("invalid native placement trace")
			}
			if mode != "direct" && mode != "block_behind_stone" && mode != "lever" {
				t.Fatal("unknown native source arrangement")
			}
			input := func(powered bool) world.Block {
				if mode == "lever" {
					return Lever{Facing: facing, Direction: cube.North, Powered: powered}
				}
				if powered {
					return RedstoneBlock{}
				}
				if mode == "direct" {
					return Stone{}
				}
				return Air{}
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.PreparedTick - 1
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(trace.Support, Stone{}, nil)
				tx.SetBlock(trace.Source, input(c.InitiallyPowered), nil)
				tx.SetBlock(trace.Lamp, RedstoneLamp{}, nil)
			})
			setup := trace.Changes[0].Tick
			for tick := trace.PreparedTick; tick < setup; tick++ {
				w.AdvanceTick()
			}
			compare := func(tx *world.Tx, tick int64, expected sample) {
				compareTorchNetworkBlock(t, tx, tick, trace.Torch, expected.Torch)
				compareTorchNetworkBlock(t, tx, tick, trace.Lamp, expected.Lamp)
				compareTorchNetworkBlock(t, tx, tick, trace.Support, expected.Support)
				compareTorchNetworkBlock(t, tx, tick, trace.Source, expected.Source)
				_, states := tx.Block(trace.Torch).EncodeBlock()
				if states["torch_facing_direction"] != expected.Torch.States.Facing {
					t.Errorf("tick %d torch facing=%v, native=%s", tick, states["torch_facing_direction"], expected.Torch.States.Facing)
				}
			}
			<-w.Exec(func(tx *world.Tx) {
				if tx.CurrentTick() != setup {
					t.Errorf("setup tick %d, native %d", tx.CurrentTick(), setup)
				}
				ctx := &item.UseContext{}
				if !(RedstoneTorch{}).UseOnBlock(trace.Support, facing.Opposite(), mgl64.Vec3{}, tx, &torchPlacementTestUser{tx: tx}, ctx) {
					t.Error("valid torch placement was rejected")
				}
				compare(tx, setup, trace.Placement)
			})
			if t.Failed() {
				return
			}
			for _, frame := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != frame.Tick {
						t.Errorf("world tick %d, native %d", tx.CurrentTick(), frame.Tick)
					}
					compare(tx, frame.Tick, frame)
					for _, change := range trace.Changes {
						if change.Tick == frame.Tick && change.Kind != "setup" {
							tx.SetBlock(change.Pos, input(change.Powered), nil)
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
