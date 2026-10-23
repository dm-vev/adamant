package end

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestNaturalEndPlatformMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_platform.json")
	if err != nil {
		t.Fatal(err)
	}
	var cells []struct {
		Position [3]int
		Block    string
	}
	if err := json.Unmarshal(data, &cells); err != nil {
		t.Fatal(err)
	}
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	g := New(0)
	columns := map[world.ChunkPos]*chunk.Chunk{}
	for _, cell := range cells {
		x, y, z := cell.Position[0], cell.Position[1], cell.Position[2]
		pos := world.ChunkPos{int32(x >> 4), int32(z >> 4)}
		c := columns[pos]
		if c == nil {
			c = chunk.New(registry, cube.Range{0, 255})
			g.GenerateChunk(pos, c, world.GenerationContext{})
			columns[pos] = c
		}
		name, _, _ := registry.RuntimeIDToState(c.Block(uint8(x&15), int16(y), uint8(z&15), 0))
		if name != cell.Block {
			t.Errorf("at %v got=%s source=%s", cell.Position, name, cell.Block)
		}
	}
}
