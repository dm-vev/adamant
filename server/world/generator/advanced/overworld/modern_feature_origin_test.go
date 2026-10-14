package overworld

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// TestTreesCrossingChunkBordersAreWhole checks a feature reaching across a
// chunk border is the same in every chunk it reaches: every leaf block of the
// chunks along the border hangs on a log within leaf distance, so no chunk
// keeps a canopy whose trunk its neighbour never placed.
func TestTreesCrossingChunkBordersAreWhole(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	g := NewOverworld(0)
	chunks := make(map[world.ChunkPos]*chunk.Chunk)
	for x := int32(1); x <= 3; x++ {
		for z := int32(2); z <= 5; z++ {
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			g.GenerateChunk(world.ChunkPos{x, z}, c, world.GenerationContext{})
			chunks[world.ChunkPos{x, z}] = c
		}
	}
	name := func(p cube.Pos) string {
		c, ok := chunks[world.ChunkPos{int32(p[0] >> 4), int32(p[2] >> 4)}]
		if !ok || p[1] < c.Range().Min() || p[1] > c.Range().Max() {
			return ""
		}
		name, properties, _ := world.DefaultBlockRegistry.RuntimeIDToState(c.Block(uint8(p[0]&15), int16(p[1]), uint8(p[2]&15), 0))
		if strings.HasSuffix(name, "leaves") && properties["persistent_bit"] == true {
			return "persistent"
		}
		return name
	}
	leaves := make(map[cube.Pos]bool)
	var queue []cube.Pos
	distance := make(map[cube.Pos]int)
	for pos := range chunks {
		for x := range 16 {
			for z := range 16 {
				for y := 50; y < 140; y++ {
					p := cube.Pos{int(pos[0])*16 + x, y, int(pos[1])*16 + z}
					switch n := name(p); {
					case strings.HasSuffix(n, "leaves"):
						leaves[p] = true
					case strings.HasSuffix(n, "_log") || strings.HasSuffix(n, "_wood"):
						distance[p] = 0
						queue = append(queue, p)
					}
				}
			}
		}
	}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if distance[p] == 6 {
			continue
		}
		for _, face := range cube.Faces() {
			n := p.Side(face)
			if _, seen := distance[n]; leaves[n] && !seen {
				distance[n] = distance[p] + 1
				queue = append(queue, n)
			}
		}
	}
	orphans := 0
	for p := range leaves {
		inner := p[0] >= 2*16 && p[0] < 3*16 && p[2] >= 3*16 && p[2] < 5*16
		if _, attached := distance[p]; inner && !attached {
			orphans++
		}
	}
	if orphans != 0 {
		t.Fatalf("%d leaf blocks along the chunk border hang on no log", orphans)
	}
}
