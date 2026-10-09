package nether

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func TestPortalCenterOwnershipAndCrossChunkRendering(t *testing.T) {
	// BoundingBox.getCenter is min + inclusive span/2. Truncating
	// (min+max)/2 would yield x=0 and assign this negative-spanning piece
	// to a different chunk.
	box := javastructure.Box{Min: [3]int32{-2, 64, -1}, Max: [3]int32{0, 64, 1}}
	if got, want := portalBoxCenter(box), [3]int32{-1, 64, 0}; got != want {
		t.Fatalf("portal box center = %v, want Java center %v", got, want)
	}
	base := block.Netherrack{}
	templateBlocks := make([]javastructure.Block, 0, 9)
	for x := int32(0); x < 3; x++ {
		for z := int32(0); z < 3; z++ {
			templateBlocks = append(templateBlocks, javastructure.Block{Pos: [3]int32{x, 0, z}, State: world.BlockState{Name: "minecraft:netherrack"}})
		}
	}
	start := &netherPortalStart{
		box:      box,
		template: &javastructure.Template{Size: [3]int32{3, 1, 3}, Palettes: [][]javastructure.Block{templateBlocks}},
		origin:   [3]int32{-2, 64, -1},
	}
	random := java26.NewLegacyRandom(1)
	air := block.Air{}
	env := javastructure.ProcessorEnvironment{
		BaseBlock: func(_, y, _ int32) world.Block {
			if y == 64 {
				return base
			}
			return air
		},
		BaseHeight: func(int32, int32, bool) int32 { return 65 },
		Random:     &random,
	}
	clip := func(chunkX int32) javastructure.Bounds {
		return javastructure.Bounds{Min: [3]int32{chunkX * 16, 0, 0}, Max: [3]int32{chunkX*16 + 15, 127, 15}}
	}
	if _, placed, err := start.place(clip(0), env, 1); err != nil || placed {
		t.Fatalf("portal was owned by chunk 0: placed=%v err=%v", placed, err)
	}
	placement, placed, err := start.place(clip(-1), env, 1)
	if err != nil || !placed {
		t.Fatalf("center chunk did not render portal: placed=%v err=%v", placed, err)
	}
	var sawNeighborChunkBlock bool
	for _, b := range placement.Blocks {
		if b.Pos[1] < 0 || b.Pos[1] > 127 {
			t.Fatalf("portal emitted outside Nether build range: %v", b.Pos)
		}
		if b.Pos[0]>>4 == 0 {
			sawNeighborChunkBlock = true
		}
	}
	if !sawNeighborChunkBlock {
		t.Fatal("center-owned portal did not emit its cross-chunk template geometry")
	}
}

func TestPortalRendererMatchesJavaPostProcessFixture(t *testing.T) {
	// Captured by actual Java 26.3 RuinedPortalPiece.postProcess for seed 0,
	// source chunk (-66,-26), giant_portal_2 and the source-proven bounding box.
	// The source level had netherrack through y=64, air above it, and a lava
	// block at (-1061,28,-401). The fixture records every final block position
	// and full state after StructureTemplate's neighbor-shape update.
	g := NewNether(0)
	start, err := g.portalStart(-66, -26)
	if err != nil || start == nil {
		t.Fatalf("Java-proven portal start was unavailable: start=%v err=%v", start, err)
	}
	wantBox := javastructure.Box{Min: [3]int32{-1066, 28, -416}, Max: [3]int32{-1056, 43, -401}}
	if start.box != wantBox {
		t.Fatalf("Java-proven portal box = %+v, want %+v", start.box, wantBox)
	}
	center := portalBoxCenter(start.box)
	chunkPos := world.ChunkPos{center[0] >> 4, center[2] >> 4}
	clip := javastructure.Bounds{
		Min: [3]int32{chunkPos[0] * 16, 0, chunkPos[1] * 16},
		Max: [3]int32{chunkPos[0]*16 + 15, 127, chunkPos[1]*16 + 15},
	}
	baseBlock := func(x, y, z int32) world.Block {
		if x == -1061 && y == 28 && z == -401 {
			return block.Lava{Depth: 8, Still: true}
		}
		if y <= 64 {
			return block.Netherrack{}
		}
		return block.Air{}
	}
	random := g.featureSource.structureStepRandom(chunkPos, 32, 4)
	placement, placed, err := start.place(clip, javastructure.ProcessorEnvironment{
		BaseBlock:  baseBlock,
		BaseHeight: func(int32, int32, bool) int32 { return 65 },
		Random:     random,
	}, g.seed)
	if err != nil || !placed {
		t.Fatalf("Java-proven portal failed to render: placed=%v err=%v", placed, err)
	}

	shapeWorld := portalShapeWorld{
		states: make(map[cube.Pos]uint32, len(placement.Blocks)),
		random: java26.NewLegacyRandom(0),
	}
	shapePositions := make([]cube.Pos, 0, len(placement.Blocks))
	for _, placedBlock := range placement.Blocks {
		state, err := javadata.BlockStateID(placedBlock.State.Name, placedBlock.State.Properties)
		if err != nil {
			t.Fatalf("encode portal state at %v: %v", placedBlock.Pos, err)
		}
		pos := cube.Pos{int(placedBlock.Pos[0]), int(placedBlock.Pos[1]), int(placedBlock.Pos[2])}
		shapeWorld.states[pos] = state
		shapePositions = append(shapePositions, pos)
	}
	if placement.TemplateUpdateFlags != 0 && !placement.TemplateKnownShape {
		world.UpdateJavaTemplateShapes(&shapeWorld, shapePositions, placement.TemplateUpdateFlags)
	}

	fixture, err := os.Open("testdata/java263_portal_postprocess_seed0.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()
	want := make(map[[3]int32]string, 1114)
	scanner := bufio.NewScanner(fixture)
	for scanner.Scan() {
		var x, y, z int32
		var state string
		if _, err := fmt.Sscanf(scanner.Text(), "%d,%d,%d=%s", &x, &y, &z, &state); err != nil {
			t.Fatalf("parse Java portal fixture row %q: %v", scanner.Text(), err)
		}
		want[[3]int32{x, y, z}] = state
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	got := make(map[[3]int32]string, len(shapeWorld.states))
	for pos, id := range shapeWorld.states {
		name, properties, ok := javadata.BlockStateFromID(id)
		if !ok {
			t.Fatalf("decode Java portal state at %v: unknown state ID %d", pos, id)
		}
		properties = javadata.BlockPropertiesWithDefaults(name, properties)
		encoded := make([]string, 0, len(properties))
		for key, value := range properties {
			encoded = append(encoded, fmt.Sprintf("%s=%v", key, value))
		}
		sort.Strings(encoded)
		got[[3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])}] = name + "|" + strings.Join(encoded, ",")
	}
	if len(want) != 1114 {
		t.Fatalf("Java portal fixture has %d states, want 1114", len(want))
	}
	if len(got) != len(want) {
		t.Fatalf("portal output has %d states, want Java's %d", len(got), len(want))
	}
	for pos, wantState := range want {
		if gotState := got[pos]; gotState != wantState {
			t.Fatalf("portal state at %v = %q, want Java 26.3 state %q", pos, gotState, wantState)
		}
	}
}

type portalShapeWorld struct {
	states map[cube.Pos]uint32
	random java26.Random
}

func (w *portalShapeWorld) JavaBlockStateID(pos cube.Pos) (uint32, error) {
	if state, ok := w.states[pos]; ok {
		return state, nil
	}
	if pos == (cube.Pos{-1061, 28, -401}) {
		return javadata.BlockStateID("minecraft:lava", map[string]any{"level": int32(0)})
	}
	if pos[1] <= 64 {
		return javadata.BlockStateID("minecraft:netherrack", nil)
	}
	return javadata.BlockStateID("minecraft:air", nil)
}
func (*portalShapeWorld) Range() cube.Range               { return cube.Range{0, 127} }
func (w *portalShapeWorld) JavaRandom() *java26.Random    { return &w.random }
func (*portalShapeWorld) Light(cube.Pos) uint8            { return 0 }
func (*portalShapeWorld) JavaFluidTickDelay(string) int32 { return 0 }
func (*portalShapeWorld) ScheduleJavaTick(cube.Pos, chunk.ScheduledTickKind, string, int32) {
}
func (*portalShapeWorld) HasJavaBlockTick(cube.Pos, string) bool { return false }
func (*portalShapeWorld) JavaSignal(cube.Pos, cube.Face) int     { return 0 }
func (*portalShapeWorld) JavaHiveHasOccupants(cube.Pos) bool     { return false }
func (w *portalShapeWorld) SetJavaBlockState(pos cube.Pos, state uint32, _ int) bool {
	w.states[pos] = state
	return true
}

func TestPortalSurfaceUsesWorldSurfaceIncludingLava(t *testing.T) {
	air, netherrack := block.Air{}, block.Netherrack{}
	lava := block.Lava{Depth: 8, Still: true}
	env := javastructure.ProcessorEnvironment{
		BaseBlock: func(_, y, _ int32) world.Block {
			switch y {
			case 70:
				return lava
			case 60:
				return netherrack
			default:
				return air
			}
		},
		// The Nether feature source's solid-only base height is lower than
		// Java WORLD_SURFACE_WG when a lava block caps the column.
		BaseHeight: func(int32, int32, bool) int32 { return 61 },
	}
	overlay := map[[3]int32]javastructure.Block{
		{0, 75, 0}: {Pos: [3]int32{0, 75, 0}, State: world.BlockState{Name: "minecraft:air"}},
	}
	if got := sPortalSurface(env, overlay, 0, 0); got != 70 {
		t.Fatalf("portal surface y = %d, want WORLD_SURFACE_WG top y 70", got)
	}
}
