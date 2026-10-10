package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

type structureStateTerrain struct{ ModernFeatureSource }

func (structureStateTerrain) SeaLevel() int32 { return 63 }

func statefulMineshaftStart(t *testing.T) modernStructureStart {
	t.Helper()
	random := largeFeatureRandom(0, 0, 0)
	start, valid, err := buildModernMineshaft(modernStructureBuildInput{seed: 0, name: "minecraft:mineshaft", config: map[string]any{"mineshaft_type": "normal"}, source: structureStateTerrain{}, random: &random})
	if err != nil {
		t.Fatal(err)
	}
	if !valid {
		t.Fatal("Java source mineshaft graph is unavailable")
	}
	start.key = modernStructureStartKey{set: "mineshafts"}
	start.mutablePieces = hasMutableStructurePieceState(start.pieces)
	return start
}

func TestMineshaftSpawnerDoesNotDuplicateAcrossChunkClipsAfterMCDBRestart(t *testing.T) {
	start := statefulMineshaftStart(t)
	var chosen minePiece
	var first, second [3]int32
	found := false
	for _, raw := range start.pieces {
		p := raw.(minePiece)
		if p.kind != "corridor" || !p.spider {
			continue
		}
		x, y, z := mineLocal(p, 1, 0, 2)
		a := [3]int32{x, y, z}
		x, y, z = mineLocal(p, 1, 0, int32(p.sections*5-3))
		b := [3]int32{x, y, z}
		if a[0]>>4 == b[0]>>4 && a[2]>>4 == b[2]>>4 {
			continue
		}
		chosen, first, second, found = p, a, b, true
		break
	}
	if !found {
		t.Fatal("Java source graph has no cross-chunk spider corridor")
	}
	stone, err := javadata.BlockStateID("minecraft:stone", nil)
	if err != nil {
		t.Fatal(err)
	}
	render := func(generator *Overworld, start modernStructureStart, point [3]int32) int {
		t.Helper()
		b := chosen.box
		minX, minZ := point[0]>>4<<4, point[2]>>4<<4
		clip := javastructure.Bounds{Min: [3]int32{max(b.Min[0], minX), b.Min[1], max(b.Min[2], minZ)}, Max: [3]int32{min(b.Max[0], minX+15), b.Max[1], min(b.Max[2], minZ+15)}}
		random := java26.NewLegacyRandom(0)
		env := javastructure.ProcessorEnvironment{JavaBlockStateID: func(int32, int32, int32) (uint32, error) { return stone, nil }, JavaBiomeAt: func(int32, int32, int32) string { return "minecraft:plains" }, BaseHeight: func(int32, int32, bool) int32 { return 320 }, Range: cube.Range{-64, 319}, Random: &random}
		ctx := modernStructureContext{pieceStates: &generator.modernStructures.pieceStates}
		count := 0
		ctx.placeStart(start, clip, env, func(placement javastructure.Placement) {
			for _, write := range placement.Blocks {
				if write.State.Name == "minecraft:spawner" && write.Pos[0] >= b.Min[0] && write.Pos[0] <= b.Max[0] && write.Pos[2] >= b.Min[2] && write.Pos[2] <= b.Max[2] {
					count++
				}
			}
		})
		return count
	}
	newGenerator := func() *Overworld { return &Overworld{seed: 0, modernStructures: new(modernStructures)} }
	generator := newGenerator()
	if count := render(generator, start, first); count != 1 {
		t.Fatalf("first clip placed %d spawners, want one", count)
	}
	// Rebuilding a graph must not reset source state when the bounded cache evicts it.
	if count := render(generator, statefulMineshaftStart(t), second); count != 0 {
		t.Fatalf("rebuilt graph duplicated %d spawners", count)
	}
	saved, err := generator.SaveGenerationState()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	db, err := mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.SaveWorldData("generation:0", saved); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = mcdb.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = db.LoadWorldData("generation:0")
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	restored := newGenerator()
	if err = restored.LoadGenerationState(saved); err != nil {
		t.Fatal(err)
	}
	if count := render(restored, statefulMineshaftStart(t), second); count != 0 {
		t.Fatalf("restarted second clip duplicated %d spawners", count)
	}
	if count := render(newGenerator(), statefulMineshaftStart(t), second); count != 1 {
		t.Fatalf("cold second clip placed %d spawners, want one", count)
	}
}

type structureStateDecorationSource struct {
	modernTerrainSource
	context *modernStructureContext
	stone   world.NativeState
}

func (s structureStateDecorationSource) BaseState(int32, int32, int32) world.NativeState {
	return s.stone
}
func (s structureStateDecorationSource) BaseHeight(int32, int32, bool) int32 { return 80 }
func (s structureStateDecorationSource) JavaNoiseBiomeAt(int32, int32, int32) string {
	return "minecraft:plains"
}
func (s structureStateDecorationSource) JavaBiomeAt(int32, int32, int32) string {
	return "minecraft:plains"
}
func (s structureStateDecorationSource) PlaceStructureStep(pos world.ChunkPos, step int, env javastructure.ProcessorEnvironment, emit func(javastructure.Placement)) {
	s.context.PlaceStep(pos, world.Overworld.Range(), step, env, emit)
}

func TestNeighbourDecorationPreviewDoesNotConsumeMineshaftSpawner(t *testing.T) {
	// A four-section source corridor crosses Z=16. Neighbour writes are previewed,
	// but only the returned target column owns the persisted placement flag.
	for _, firstZ := range []int32{0, 1} {
		name := "minecraft:mineshaft"
		piece := minePiece{box: mbox(0, 10, 0, 2, 12, 19), kind: "corridor", facing: "south", wood: "normal", sections: 4, spider: true}
		start := modernStructureStart{key: modernStructureStartKey{set: "mineshafts"}, name: name, step: 3, mutablePieces: true, pieces: []modernStructurePiece{piece}}
		ctx := &modernStructureContext{pieceStates: new(modernStructureStateStore), starts: []modernStructureStart{start}, structureSteps: map[int][]modernStructureStepEntry{3: {{name: name, index: 0}}}}
		stone, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:stone"})
		if err != nil {
			t.Fatal(err)
		}
		features := &ModernFeatures{source: structureStateDecorationSource{context: ctx, stone: stone}, biomes: map[string]modernBiomeFeatures{"plains": {}}}
		spawnerID := world.BlockRuntimeID(block.NewMobSpawner())
		generate := func(z int32) (int, int) {
			c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
			for y := int16(0); y < 80; y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						world.SetNativeState(c, x, y, z, stone)
					}
				}
			}
			result := features.Decorate(world.ChunkPos{0, z}, c)
			spawners := 0
			for y := int16(0); y < 80; y++ {
				for x := range uint8(16) {
					for z := range uint8(16) {
						if c.Block(x, y, z, 0) == spawnerID {
							spawners++
						}
					}
				}
			}
			return spawners, len(result.BlockEntities)
		}
		spawners, actors := generate(firstZ)
		if spawners != 1 || actors != 1 {
			t.Fatalf("first target Z=%d: spawners=%d actors=%d, want one live spawner and actor", firstZ, spawners, actors)
		}
		spawners, actors = generate(1 - firstZ)
		if spawners != 0 || actors != 0 {
			t.Fatalf("second target Z=%d duplicated spawners=%d actors=%d", 1-firstZ, spawners, actors)
		}
	}
}
