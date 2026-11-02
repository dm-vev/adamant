package world

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestBlockResourceOffsetsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_block_resource_native.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Seed                         uint32
			Position                     cube.Pos
			Drops, Empty, Owner, Created bool
			Words                        []uint32
			Origin                       [3]float32
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 240 {
		t.Fatal("incomplete executed native popResource corpus")
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("seed%d/%v/drops%t/empty%t/owner%t", c.Seed, c.Position, c.Drops, c.Empty, c.Owner), func(t *testing.T) {
			if c.Created != (c.Drops && !c.Empty) {
				t.Fatal("native resource guard differs")
			}
			if !c.Created {
				if len(c.Words) != 0 {
					t.Fatal("native guarded resource consumed words")
				}
				return
			}
			if len(c.Words) != 3 {
				t.Fatal("native popResource did not consume three offset words")
			}
			used := 0
			got := nativeBlockResourcePosition(c.Position, func() float32 {
				if used == len(c.Words) {
					t.Fatal("unobserved resource draw")
				}
				v := nativeRandomFloat32(c.Words[used])
				used++
				return v
			})
			if used != 3 {
				t.Fatal("resource offset draw count differs")
			}
			for axis, want := range c.Origin {
				if math.Float32bits(float32(got[axis])) != math.Float32bits(want) {
					t.Errorf("axis %d origin=%v, native=%v", axis, got[axis], want)
				}
			}
		})
	}
}
