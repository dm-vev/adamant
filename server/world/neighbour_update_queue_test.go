package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestNeighbourUpdateFIFOAcrossWrapAndGrowth(t *testing.T) {
	var queue neighbourUpdateQueue
	push := func(start, end int) {
		for i := start; i < end; i++ {
			queue.push(neighbourUpdate{pos: cube.Pos{i, 64, -i}, neighbour: cube.Pos{i - 1, 64, -i}})
		}
	}
	pop := func(start, end int) {
		for i := start; i < end; i++ {
			want := neighbourUpdate{pos: cube.Pos{i, 64, -i}, neighbour: cube.Pos{i - 1, 64, -i}}
			if got := queue.pop(); got != want {
				t.Fatalf("update %d = %v, want %v", i, got, want)
			}
		}
	}
	push(0, 60)
	pop(0, 50)
	push(60, 150)
	pop(50, 150)
	push(150, 300)
	pop(150, 300)
}
