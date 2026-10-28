package entity

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestCollisionFetchCellsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_collision_fetch.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete   bool
		WorldRange cube.Range `json:"world_range"`
		Scenarios  []struct {
			Bounds [6]float64
			Cells  []cube.Pos
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 150 {
		t.Fatal("incomplete executed-client collision fetch fixture")
	}
	for n, c := range native.Scenarios {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			source := &collisionFetchCellRecorder{}
			bounds := c.Bounds
			boxes := nativeBlockBBoxsAround(source, cube.Box(bounds[0], bounds[1], bounds[2], bounds[3], bounds[4], bounds[5]), native.WorldRange)
			if len(boxes) != 0 || !slices.Equal(source.cells, c.Cells) {
				t.Fatalf("query=%v: cells=%v, original=%v", bounds, source.cells, c.Cells)
			}
		})
	}
}

type collisionFetchCellRecorder struct{ cells []cube.Pos }

func (r *collisionFetchCellRecorder) Block(pos cube.Pos) world.Block {
	r.cells = append(r.cells, pos)
	return block.Air{}
}
