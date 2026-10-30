package world

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestVibrationSelectorMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_vibration_selector.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Listener   cube.Pos
			Candidates []struct {
				Tick     int64
				EventID  int `json:"event_id"`
				Position mgl64.Vec3
			}
			Selected []struct {
				Tick     int64
				Distance float32
				Position cube.Pos
				EventID  int `json:"event_id"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 80 {
		t.Fatal("incomplete executed-client vibration selector capture")
	}
	events := map[int]GameEvent{1: GameEventBlockActivate, 27: GameEventExplode}
	for i, trace := range native.Scenarios {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			if len(trace.Candidates) == 0 || len(trace.Candidates) != len(trace.Selected) {
				t.Fatal("missing native selection checkpoints")
			}
			var selector VibrationSelector
			for j, candidate := range trace.Candidates {
				event, ok := events[candidate.EventID]
				if !ok {
					t.Fatal("unmapped native event")
				}
				distance := vibrationDistance(candidate.Position, trace.Listener.Vec3Centre())
				selector.AddCandidate(VibrationInfo{Event: event, Position: candidate.Position, Distance: distance}, candidate.Tick)
				want := trace.Selected[j]
				if !selector.HasCandidate || selector.Tick != want.Tick || selector.Candidate.Distance != want.Distance ||
					selector.Candidate.Event != events[want.EventID] || vibrationCellPosition(selector.Candidate.Position) != want.Position {
					t.Fatalf("step %d selector=%+v, executed client=%+v", j, selector, want)
				}
			}
		})
	}
}

func TestVibrationRangeMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_vibration_selector.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Ranges   []struct {
			Origin, Listener mgl64.Vec3
			Radius           int
			Accepted         bool
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Ranges) != 92 {
		t.Fatal("incomplete executed-client dispatcher range capture")
	}
	for i, c := range native.Ranges {
		if got := vibrationInRange(c.Origin, c.Listener, c.Radius); got != c.Accepted {
			t.Errorf("case %d %v -> %v radius %d: accepted=%t, executed client=%t", i, c.Origin, c.Listener, c.Radius, got, c.Accepted)
		}
	}
}
