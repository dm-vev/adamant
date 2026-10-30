package block

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestPistonActorMatchesNativeHeap(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_actor.json")
	if err != nil {
		t.Fatal(err)
	}
	type actor struct {
		Pos              cube.Pos
		Sticky           bool
		Progress         float32
		LastProgress     float32 `json:"last_progress"`
		State            uint8
		NewState         uint8 `json:"new_state"`
		OldState         uint8 `json:"old_state"`
		Attached         []cube.Pos
		ConsumerStrength int `json:"consumer_strength"`
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Scenario struct {
				Sticky, Blocked bool
				Phase           int
			}
			Base, Source cube.Pos
			Front        []cube.Pos
			Changes      []struct {
				Tick    int64
				Powered bool
			}
			Samples []struct {
				Tick         int64
				State        string
				Moving       bool
				Base, Source nativeTripwireBlock
				Front        []nativeTripwireBlock
			}
		}
		Checkpoints []struct {
			Tick   int64
			Actors []actor
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 8 || len(native.Checkpoints) != 33 {
		t.Fatal("incomplete native actor checkpoint fixture")
	}
	checkpoints := make(map[int64]map[cube.Pos]actor)
	for _, checkpoint := range native.Checkpoints {
		if len(checkpoint.Actors) != 8 {
			t.Fatal("missing native checkpoint actor")
		}
		actors := make(map[cube.Pos]actor)
		for _, a := range checkpoint.Actors {
			actors[a.Pos] = a
		}
		checkpoints[checkpoint.Tick] = actors
	}
	for _, trace := range native.Scenarios {
		c := trace.Scenario
		t.Run(fmt.Sprintf("sticky%t/blocked%t/phase%d", c.Sticky, c.Blocked, c.Phase), func(t *testing.T) {
			if len(trace.Samples) != 90 || len(trace.Changes) != 2 || len(trace.Front) != 3 {
				t.Fatal("invalid native actor trace")
			}
			settings := (world.NopProvider{}).Settings()
			settings.CurrentTick = trace.Samples[0].Tick - 2
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Dim: world.End, Provider: world.NopProvider{Set: settings}}.New()
			t.Cleanup(func() { _ = w.Close() })
			<-w.Exec(func(tx *world.Tx) {
				tx.SetBlock(trace.Base, Piston{Facing: cube.FaceEast, Sticky: c.Sticky}, nil)
				var b world.Block = Stone{}
				if c.Blocked {
					b = Obsidian{}
				}
				tx.SetBlock(trace.Front[0], b, nil)
			})
			observed := 0
			for _, sample := range trace.Samples {
				w.AdvanceTick()
				<-w.Exec(func(tx *world.Tx) {
					if tx.CurrentTick() != sample.Tick {
						t.Fatal("native actor replay clock diverged")
					}
					compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Base, sample.Base)
					compareSculkContactNativeBlock(t, tx, sample.Tick, trace.Source, sample.Source)
					for i, pos := range trace.Front {
						compareSculkContactNativeBlock(t, tx, sample.Tick, pos, sample.Front[i])
					}
					if a, ok := checkpoints[sample.Tick][trace.Base]; ok {
						observed++
						p := tx.Block(trace.Base).(Piston)
						if p.previousState != a.OldState {
							t.Errorf("tick %d previous state=%d, native %d", sample.Tick, p.previousState, a.OldState)
						}
						nbt := p.EncodeNBT()
						if nbt["Progress"] != a.Progress || nbt["LastProgress"] != a.LastProgress || nbt["State"] != a.State || nbt["NewState"] != a.NewState || p.Sticky != a.Sticky || !slices.Equal(p.Attached, a.Attached) {
							t.Errorf("tick %d actor progress=%v/%v state=%v/%v attached=%v, native progress=%v/%v state=%d/%d attached=%v", sample.Tick, nbt["Progress"], nbt["LastProgress"], nbt["State"], nbt["NewState"], p.Attached, a.Progress, a.LastProgress, a.State, a.NewState, a.Attached)
						}
						power, active := tx.Redstone().ConsumerPower(trace.Base)
						if !active || power != a.ConsumerStrength {
							t.Errorf("tick %d consumer=%d active=%t, native %d", sample.Tick, power, active, a.ConsumerStrength)
						}
					}
					for _, change := range trace.Changes {
						if change.Tick == sample.Tick {
							var b world.Block = Air{}
							if change.Powered {
								b = RedstoneBlock{}
							}
							tx.SetBlock(trace.Source, b, nil)
						}
					}
				})
			}
			if observed != 33 {
				t.Fatalf("compared %d actor checkpoints, want 33", observed)
			}
		})
	}
}
