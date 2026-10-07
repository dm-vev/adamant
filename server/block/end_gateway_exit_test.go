package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

func TestGatewayFullCollisionShapeUnion(t *testing.T) {
	for _, test := range []struct {
		name string
		boxes []cube.BBox
		full bool
	}{
		{name: "empty"},
		{name: "full cube", boxes: []cube.BBox{cube.Box(0, 0, 0, 1, 1, 1)}, full: true},
		{name: "split cube", boxes: []cube.BBox{cube.Box(0, 0, 0, .5, 1, 1), cube.Box(.5, 0, 0, 1, 1, 1)}, full: true},
		{name: "gap", boxes: []cube.BBox{cube.Box(0, 0, 0, .49, 1, 1), cube.Box(.5, 0, 0, 1, 1, 1)}},
		{name: "slab", boxes: []cube.BBox{cube.Box(0, 0, 0, 1, .5, 1)}},
		{name: "hollow solid faces", boxes: []cube.BBox{
			cube.Box(0, 0, 0, .1, 1, 1), cube.Box(.9, 0, 0, 1, 1, 1),
			cube.Box(0, 0, 0, 1, .1, 1), cube.Box(0, .9, 0, 1, 1, 1),
			cube.Box(0, 0, 0, 1, 1, .1), cube.Box(0, 0, .9, 1, 1, 1),
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := gatewayShapeCovers(mgl64.Vec3{}, mgl64.Vec3{1, 1, 1}, test.boxes); got != test.full {
				t.Fatalf("full collision cube = %v, want %v", got, test.full)
			}
		})
	}
}
