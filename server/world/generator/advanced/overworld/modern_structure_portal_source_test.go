package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

const javaPortalSeed = -7406315911040434051

// TestRuinedPortalStartsMatchJava checks ruined portal starts against the
// pieces the Java 26.3 server saved for them. The mirror used to be drawn
// the wrong way round and to move the pivot, the template stayed at y 0
// instead of the height findSuitableY chose, and the biome at that height
// was never checked.
func TestRuinedPortalStartsMatchJava(t *testing.T) {
	s := New(javaPortalSeed).modernStructures
	for _, c := range []struct {
		structure string
		chunk     [2]int32
		box       structure.Box
		rotation  structure.Rotation
		mirror    structure.Mirror
		vertical  string
	}{
		{"ruined_portal", [2]int32{326, 95}, structure.Box{Min: [3]int32{5224, 52, 1520}, Max: [3]int32{5231, 60, 1528}}, structure.Clockwise180, structure.FrontBack, "underground"},
		{"ruined_portal_mountain", [2]int32{379, 143}, structure.Box{Min: [3]int32{6065, 103, 2299}, Max: [3]int32{6074, 110, 2310}}, structure.CounterClockwise90, structure.FrontBack, "in_mountain"},
		{"ruined_portal_mountain", [2]int32{282, 101}, structure.Box{Min: [3]int32{4512, 58, 1616}, Max: [3]int32{4520, 64, 1624}}, structure.Clockwise90, structure.NoMirror, "in_mountain"},
		{"ruined_portal", [2]int32{-151, -344}, structure.Box{Min: [3]int32{-2415, 49, -5504}, Max: [3]int32{-2406, 56, -5493}}, structure.CounterClockwise90, structure.NoMirror, "underground"},
	} {
		start, valid, err := s.buildStructure("minecraft:"+c.structure, c.chunk[0], c.chunk[1])
		if err != nil {
			t.Fatal(err)
		}
		if !valid {
			t.Errorf("%s at %v: no start, Java has one", c.structure, c.chunk)
			continue
		}
		p := start.pieces[0].(modernRuinedPortal)
		if p.box != c.box || p.rotation != c.rotation || p.mirror != c.mirror || p.vertical != c.vertical {
			t.Errorf("%s at %v: box %v rotation %v mirror %v %s, Java has %v %v %v %s", c.structure, c.chunk, p.box, p.rotation, p.mirror, p.vertical, c.box, c.rotation, c.mirror, c.vertical)
		}
	}
	// The standard portal of chunk 379, 143 stands in dripstone caves at the
	// height Java picks, not in its biome, so the mountain portal starts.
	if _, valid, err := s.buildStructure("minecraft:ruined_portal", 379, 143); err != nil || valid {
		t.Errorf("standard ruined portal at 379, 143 starts (%v), Java rejects its biome", err)
	}
}

// TestRuinedPortalReachesNeighbourChunks checks portal blocks as the Java
// server generated them. Java places a ruined portal from the chunk holding
// its centre, writing into the chunks around it: the portal of chunk
// 379, 143 is centred in chunk 379, 144.
func TestRuinedPortalReachesNeighbourChunks(t *testing.T) {
	g := New(javaPortalSeed)
	chunks := map[world.ChunkPos]*chunk.Chunk{}
	for _, b := range []struct {
		pos   cube.Pos
		state string
	}{
		{cube.Pos{4516, 59, 1620}, "minecraft:magma_block"},
		{cube.Pos{4514, 59, 1621}, "minecraft:obsidian"},
		{cube.Pos{4514, 58, 1621}, "minecraft:magma_block"},
		{cube.Pos{6070, 104, 2300}, "minecraft:stone_brick_stairs[facing=south,half=bottom,shape=straight,waterlogged=false]"},
		{cube.Pos{6068, 104, 2302}, "minecraft:crying_obsidian"},
		{cube.Pos{6068, 105, 2302}, "minecraft:obsidian"},
		// The air pocket of the template clears the mountain.
		{cube.Pos{6066, 104, 2300}, "minecraft:air"},
	} {
		pos := world.ChunkPos{int32(b.pos[0] >> 4), int32(b.pos[2] >> 4)}
		c, ok := chunks[pos]
		if !ok {
			c = chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			g.GenerateChunk(pos, c, world.GenerationContext{})
			chunks[pos] = c
		}
		state, err := world.CanonicalState(world.DefaultBlockRegistry, world.NativeStateAt(c, uint8(b.pos[0]&15), int16(b.pos[1]), uint8(b.pos[2]&15)))
		if err != nil {
			t.Fatal(err)
		}
		if got := javaStateString(state); got != b.state {
			t.Errorf("%v is %s, want %s as Java generates it", b.pos, got, b.state)
		}
	}
}
