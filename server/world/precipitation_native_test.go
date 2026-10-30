package world

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestPrecipitationColumnsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_precipitation_column_random.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Initial      uint32 `json:"initial_state"`
			X, Z, Height int
			State        uint32
			Position     cube.Pos
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 588 {
		t.Fatal("incomplete native snow coordinate capture")
	}
	for i, c := range native.Scenarios {
		state := c.Initial
		x, z := nextPrecipitationColumn(&state, ChunkPos{int32(c.X >> 4), int32(c.Z >> 4)})
		if state != c.State || (cube.Pos{x, c.Height + 1, z}) != c.Position {
			t.Fatalf("case %d state=%d position=%v, native state=%d position=%v", i, state, cube.Pos{x, c.Height + 1, z}, c.State, c.Position)
		}
	}
}

type precipitationDrawCounter struct{ count int }

func (r *precipitationDrawCounter) Uint64() uint64 { r.count++; return 8 }

func TestChunkSnowRandomBudgetMatchesOwnedBDS(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_chunk_snow_random.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete    bool
		Checkpoints []struct {
			Age          int
			AppliedSpeed int `json:"applied_speed"`
			Chunks       []struct {
				Position ChunkPos
				State    uint32 `json:"snow_random"`
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Checkpoints) != 20 {
		t.Fatal("incomplete owned BDS snow counter capture")
	}
	random := &precipitationDrawCounter{}
	w := Config{Synchronous: true, RandomTickSpeed: -1, RandSource: random}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		previousSpeed := 0
		for i, frame := range native.Checkpoints {
			// The held Script callback changes the speed after the observed
			// actor pass. Its value therefore controls the following frame.
			states := map[ChunkPos]uint32{}
			for _, c := range frame.Chunks {
				states[c.Position] = c.State
			}
			if len(states) != 4 || frame.Age != i+1 {
				t.Fatal("invalid native chunk/age controls")
			}
			w.conf.RandomTickSpeed = previousSpeed
			random.count = 0
			for pos, want := range states {
				col := tx.chunk(pos)
				if i != 0 {
					w.tickChunkPrecipitation(tx, pos)
				}
				if col.snowRandomValue != want {
					t.Fatalf("frame %d chunk %v snow counter %d, native %d", i, pos, col.snowRandomValue, want)
				}
			}
			if i != 0 && random.count != 4*previousSpeed {
				t.Fatalf("frame %d level RNG consumed %d draws, native budget %d", i, random.count, 4*previousSpeed)
			}
			previousSpeed = frame.AppliedSpeed
		}
	})
}
