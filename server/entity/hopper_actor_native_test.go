package entity

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHopperActorTransferClockMatchesBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_hopper_actor_clock.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		SetupTick int64 `json:"setup_tick"`
		StartTick int64 `json:"start_tick"`
		Loads     []struct {
			Tick     int64
			Position cube.Pos
			Count    int
		}
		Checkpoints []struct {
			Tick  int64
			Age   int64
			Cells []struct {
				Position            cube.Pos
				Source, Destination int
			}
			Actors []struct {
				Position     cube.Pos
				ActorTicks   int64 `json:"actor_ticks"`
				LastTick     int64 `json:"last_tick"`
				Cooldown     int64
				MoveCooldown int64 `json:"move_cooldown"`
				Speed        int
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Checkpoints) != 25 || len(native.Loads) != 2 || native.StartTick <= native.SetupTick {
		t.Fatal("incomplete native hopper heap controls")
	}
	settings := (world.NopProvider{}).Settings()
	settings.CurrentTick = native.SetupTick - 1
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry, Provider: world.NopProvider{Set: settings}}.New()
	loader := world.NewLoader(2, w, world.NopViewer{})
	defer func() { <-w.Exec(func(tx *world.Tx) { loader.Close(tx) }); _ = w.Close() }()
	if err := w.SetGameRule("spawn_mobs", false); err != nil {
		t.Fatal(err)
	}
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, native.Loads[0].Position.Vec3Centre())
		loader.Load(tx, 25)
		for _, load := range native.Loads {
			h := block.NewHopper()
			h.Facing = cube.FaceNorth
			tx.SetBlock(load.Position, h, nil)
			tx.SetBlock(load.Position.Side(cube.FaceNorth), block.NewBarrel(), nil)
		}
	})
	index := 0
	for tick := native.SetupTick + 1; tick <= native.Checkpoints[len(native.Checkpoints)-1].Tick; tick++ {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if tx.CurrentTick() != tick {
				t.Fatal("native hopper actor clock mismatch")
			}
			if index < len(native.Checkpoints) && native.Checkpoints[index].Tick == tick {
				frame := native.Checkpoints[index]
				if frame.Tick != native.StartTick+frame.Age || frame.Age != 36+int64(index) || len(frame.Cells) != 2 || len(frame.Actors) != 2 {
					t.Fatal("invalid native actor checkpoint")
				}
				for _, actor := range frame.Actors {
					if actor.LastTick != tick&^1 || actor.ActorTicks != tick-native.SetupTick || actor.Speed != 4 {
						t.Fatal("native actor tick/deduplication controls failed")
					}
					h := tx.Block(actor.Position).(block.Hopper)
					if h.TransferCooldown != actor.Cooldown {
						t.Errorf("tick %d %v transfer cooldown=%d, native=%d", tick, actor.Position, h.TransferCooldown, actor.Cooldown)
					}
				}
				for _, cell := range frame.Cells {
					for pos, want := range map[cube.Pos]int{cell.Position: cell.Source, cell.Position.Side(cube.FaceNorth): cell.Destination} {
						got := inventoryCount(tx.Block(pos).(block.Container).Inventory(tx, pos).Slots())
						if got != want {
							t.Errorf("tick %d %v items=%d, native=%d", tick, pos, got, want)
						}
					}
				}
				index++
			}
			for _, load := range native.Loads {
				if load.Tick == tick {
					if load.Count != 4 {
						t.Fatal("invalid native load control")
					}
					if err := tx.Block(load.Position).(block.Container).Inventory(tx, load.Position).SetItem(0, item.NewStack(item.Diamond{}, load.Count)); err != nil {
						t.Fatal(err)
					}
				}
			}
		})
		if t.Failed() {
			return
		}
	}
	if index != len(native.Checkpoints) {
		t.Fatal("skipped native actor checkpoint")
	}
}
