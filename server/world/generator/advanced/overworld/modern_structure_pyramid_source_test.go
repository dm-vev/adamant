package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type desertPyramidConstructorFixture struct {
	Seed      string   `json:"seed"`
	Direction string   `json:"direction"`
	Box       [6]int32 `json:"box"`
	AfterInt  int32    `json:"after_int"`
}

func TestDesertPyramidRawConstructorMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_desert_pyramid_constructor.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []desertPyramidConstructorFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, fixture := range fixtures {
		fixture := fixture
		seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(fixture.Seed, func(t *testing.T) {
			random := java26.NewLegacyRandom(seed)
			start, valid, err := buildModernDesertPyramid(modernStructureBuildInput{seed: seed, name: "minecraft:desert_pyramid", chunkX: -1, chunkZ: 2, random: &random})
			if err != nil || !valid {
				t.Fatalf("constructor valid=%v err=%v", valid, err)
			}
			piece := start.pieces[0].(modernDesertPyramid)
			gotBox := [6]int32{piece.box.Min[0], piece.box.Min[1], piece.box.Min[2], piece.box.Max[0], piece.box.Max[1], piece.box.Max[2]}
			if gotBox != fixture.Box {
				t.Errorf("raw source bounds %v, want %v", gotBox, fixture.Box)
			}
			gotDirection := [4]string{"north", "east", "south", "west"}[piece.dir]
			seen[gotDirection] = true
			if gotDirection != fixture.Direction {
				t.Errorf("orientation %q, want %q", gotDirection, fixture.Direction)
			}
			if got := random.NextInt(1_000_000); got != fixture.AfterInt {
				t.Errorf("constructor following random = %d, want %d", got, fixture.AfterInt)
			}
			if piece.Bounds() != (javastructure.Box{Min: [3]int32{-16, 64, 32}, Max: [3]int32{4, 78, 52}}) {
				t.Errorf("constructor eagerly terrain-adjusted bounds: %+v", piece.Bounds())
			}
		})
	}
	for _, direction := range []string{"north", "east", "south", "west"} {
		if !seen[direction] {
			t.Errorf("constructor fixture omits %s orientation", direction)
		}
	}
}

type desertPyramidActorFixture struct {
	Pos  [3]int32 `json:"pos"`
	Kind string   `json:"kind"`
	NBT  string   `json:"nbt"`
}
type desertPyramidSourceFixture struct {
	Seed             string                      `json:"seed"`
	Direction        string                      `json:"direction"`
	BBox             [6]int32                    `json:"bbox"`
	HPos             int32                       `json:"hpos"`
	NextPostInt      int32                       `json:"next_post_int"`
	NextLevelInt     int32                       `json:"next_level_int"`
	WriteCount       int                         `json:"write_count"`
	WritesSHA256     string                      `json:"writes_sha256"`
	Chests           [4]bool                     `json:"chests"`
	CandidateCount   int                         `json:"candidate_count"`
	Roof             [3]int32                    `json:"roof"`
	SuspiciousActors int                         `json:"suspicious_actors"`
	ChestActors      int                         `json:"chest_actors"`
	Actors           []desertPyramidActorFixture `json:"actors"`
	Markers          [][3]int32                  `json:"markers"`
}

func TestDesertPyramidPostProcessAndAfterPlaceMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_desert_pyramid_full.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []desertPyramidSourceFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(fixture.Direction, func(t *testing.T) {
			dir := map[string]int{"north": 0, "east": 1, "south": 2, "west": 3}[fixture.Direction]
			piece := modernDesertPyramid{box: javastructure.Box{Min: [3]int32{-8, 64, -8}, Max: [3]int32{12, 78, 12}}, dir: dir}
			state := javastructure.StructurePieceState{}
			random := java26.NewLegacyRandom(seed ^ 0x7137)
			levelRandom := java26.NewLegacyRandom(87654321)
			stateIDs := map[string]uint32{}
			stateID := func(name string) uint32 {
				if id, ok := stateIDs[name]; ok {
					return id
				}
				id, err := javadata.BlockStateID(name, nil)
				if err != nil {
					t.Fatal(err)
				}
				stateIDs[name] = id
				return id
			}
			height := func(x, z int32) int32 { return 70 + int32(((int64(x)*3+int64(z)*5)%7+7)%7) }
			clip := javastructure.Bounds{Min: [3]int32{-16, -64, -16}, Max: [3]int32{31, 319, 31}}
			env := javastructure.ProcessorEnvironment{
				WorldSeed: 123456789, Random: &random, LevelRandom: &levelRandom, PieceState: &state, Range: cube.Range{-64, 319},
				MotionBlockingNoLeavesHeight: height,
				JavaBlockStateID: func(x, y, z int32) (uint32, error) {
					name := "minecraft:air"
					if y >= 0 && y < height(x, z) {
						name = "minecraft:stone"
					}
					return stateID(name), nil
				},
				CanWriteBlock: func(x, y, z int32) bool {
					return y >= -64 && y <= 319 && (x>>4) >= -2 && (x>>4) <= 0 && (z>>4) >= -2 && (z>>4) <= 0
				},
			}
			post, err := piece.Place(clip, env)
			if err != nil {
				t.Fatal(err)
			}
			moved := mutableStructurePieceBounds(piece, &state)
			after, err := piece.AfterPlace(clip, env, moved)
			if err != nil {
				t.Fatal(err)
			}
			blocks := append(post.Blocks, after.Blocks...)
			trace := make([]string, len(blocks))
			for i, b := range blocks {
				trace[i] = fmt.Sprintf("%d,%d,%d=%s:%t", b.Pos[0], b.Pos[1], b.Pos[2], desertPyramidState(b.State), true)
			}
			gotHash := sha256.Sum256([]byte(strings.Join(trace, "\n")))
			if len(blocks) != fixture.WriteCount {
				t.Errorf("ordered source writes=%d, Java=%d", len(blocks), fixture.WriteCount)
			}
			if got := hex.EncodeToString(gotHash[:]); got != fixture.WritesSHA256 {
				t.Errorf("ordered source writes SHA-256=%s, Java=%s", got, fixture.WritesSHA256)
			}
			gotBox := [6]int32{moved.Min[0], moved.Min[1], moved.Min[2], moved.Max[0], moved.Max[1], moved.Max[2]}
			if gotBox != fixture.BBox || state.GroundHeight != fixture.HPos {
				t.Errorf("moved box/HPos=%v/%d, Java=%v/%d", gotBox, state.GroundHeight, fixture.BBox, fixture.HPos)
			}
			if got := random.NextInt(1_000_000); got != fixture.NextPostInt {
				t.Errorf("postProcess following RNG=%d, Java=%d", got, fixture.NextPostInt)
			}
			if got := levelRandom.NextInt(1_000_000); got != fixture.NextLevelInt {
				t.Errorf("level following RNG=%d, Java=%d", got, fixture.NextLevelInt)
			}
			candidateCount := 0
			roof := [3]int32{}
			if state.Pyramid != nil {
				candidateCount = len(state.Pyramid.SandPositions)
				roof = state.Pyramid.RoofPosition
			}
			if state.LootPlaced != 0x0f || candidateCount != fixture.CandidateCount || roof != fixture.Roof {
				t.Errorf("mutable source state chest=%#x candidates=%d roof=%v", state.LootPlaced, candidateCount, roof)
			}
			gotActors := make(map[[3]int32]javastructure.Block)
			for _, b := range blocks {
				if b.NBT != nil {
					gotActors[b.Pos] = b
				}
			}
			var suspicious, chests int
			for _, actor := range fixture.Actors {
				b, ok := gotActors[actor.Pos]
				if !ok {
					t.Errorf("missing %s block actor at %v", actor.Kind, actor.Pos)
					continue
				}
				if actor.Kind == "brushable" {
					suspicious++
				} else {
					chests++
				}
				if b.NBT["LootTable"] == nil {
					t.Errorf("actor at %v lacks source loot table", actor.Pos)
				}
				if got, ok := b.NBT["LootTableSeed"].(int64); !ok || got != desertFixtureLootSeed(t, actor.NBT) {
					t.Errorf("actor at %v loot seed=%v, Java NBT=%s", actor.Pos, b.NBT["LootTableSeed"], actor.NBT)
				}
			}
			if suspicious != fixture.SuspiciousActors || chests != fixture.ChestActors {
				t.Errorf("source actors brushable/chest=%d/%d, Java=%d/%d", suspicious, chests, fixture.SuspiciousActors, fixture.ChestActors)
			}
			if len(post.PostProcessing)+len(after.PostProcessing) != len(fixture.Markers) {
				t.Errorf("post-processing markers=%d, Java=%d", len(post.PostProcessing)+len(after.PostProcessing), len(fixture.Markers))
			}
		})
	}
}

func desertFixtureLootSeed(t *testing.T, nbt string) int64 {
	t.Helper()
	const key = "LootTableSeed:"
	start := strings.Index(nbt, key)
	if start < 0 {
		t.Fatalf("Java actor NBT has no LootTableSeed: %s", nbt)
	}
	start += len(key)
	end := strings.IndexByte(nbt[start:], 'L')
	if end < 0 {
		t.Fatalf("Java actor NBT has malformed LootTableSeed: %s", nbt)
	}
	seed, err := strconv.ParseInt(nbt[start:start+end], 10, 64)
	if err != nil {
		t.Fatalf("parsing Java actor LootTableSeed: %v", err)
	}
	return seed
}
func desertPyramidState(state world.BlockState) string {
	// Compare resolved Java states, not whether the port spells out default properties.
	id, err := javadata.BlockStateID(state.Name, state.Properties)
	if err != nil {
		panic(err)
	}
	state.Name, state.Properties, _ = javadata.BlockStateFromID(id)
	keys := make([]string, 0, len(state.Properties))
	for key := range state.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+fmt.Sprint(state.Properties[key]))
	}
	return state.Name + "[" + strings.Join(values, ",") + "]"
}

type desertPyramidSequenceFixture struct {
	Step           string   `json:"step"`
	Clip           [6]int32 `json:"clip"`
	PostCount      int      `json:"post_count"`
	PostHash       string   `json:"post_hash"`
	AfterCount     int      `json:"after_count"`
	AfterHash      string   `json:"after_hash"`
	BBox           [6]int32 `json:"bbox"`
	HPos           int32    `json:"hpos"`
	Chests         [4]bool  `json:"chests"`
	CandidateCount int      `json:"candidate_count"`
	Roof           [3]int32 `json:"roof"`
	NextPostInt    int32    `json:"next_post_int"`
	NextLevelInt   int32    `json:"next_level_int"`
}

func TestDesertPyramidRepeatedChunksAndSaveReloadMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_desert_pyramid_sequence.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []desertPyramidSequenceFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	const seed int64 = 1000016
	piece := modernDesertPyramid{box: javastructure.Box{Min: [3]int32{-8, 64, -8}, Max: [3]int32{12, 78, 12}}, dir: 0}
	start := modernStructureStart{key: modernStructureStartKey{set: "pyramid_sequence"}, pieces: []modernStructurePiece{piece}, mutablePieces: true}
	generator := &Overworld{seed: 123456789, modernStructures: new(modernStructures)}
	flags := generator.modernStructures.pieceStates.forStart(start)
	state := &flags.pieces[0]
	random := java26.NewLegacyRandom(seed ^ 0x7137)
	levelRandom := java26.NewLegacyRandom(87654321)
	overlay := make(map[[3]int32]uint32)
	rangeY := cube.Range{-64, 319}
	height := func(x, z int32) int32 { return 70 + int32(((int64(x)*3+int64(z)*5)%7+7)%7) }
	canWrite := func(x, y, z int32) bool {
		return y >= int32(rangeY.Min()) && y <= int32(rangeY.Max()) && (x>>4) >= -2 && (x>>4) <= 0 && (z>>4) >= -2 && (z>>4) <= 0
	}
	env := javastructure.ProcessorEnvironment{
		WorldSeed: 123456789, Random: &random, LevelRandom: &levelRandom, PieceState: state, Range: rangeY,
		MotionBlockingNoLeavesHeight: height,
		JavaBlockStateID: func(x, y, z int32) (uint32, error) {
			if id, ok := overlay[[3]int32{x, y, z}]; ok {
				return id, nil
			}
			name := "minecraft:air"
			if y >= 0 && y < height(x, z) {
				name = "minecraft:stone"
			}
			return javadata.BlockStateID(name, nil)
		},
		CanWriteBlock: canWrite,
	}
	for index, fixture := range fixtures {
		if index == 3 {
			saved, err := generator.SaveGenerationState()
			if err != nil {
				t.Fatal(err)
			}
			generator = &Overworld{seed: 123456789, modernStructures: new(modernStructures)}
			if err = generator.LoadGenerationState(saved); err != nil {
				t.Fatal(err)
			}
			flags = generator.modernStructures.pieceStates.forStart(start)
			if err = flags.prepare(start); err != nil {
				t.Fatal(err)
			}
			state = &flags.pieces[0]
			if state.Pyramid != nil {
				t.Fatal("pyramid runtime candidates/roof were serialized")
			}
			if !state.GroundHeightSet || state.GroundHeight != 70 || !state.BoundingBoxYSet || state.BoundingBoxMinY != 68 || state.LootPlaced != 0x0f {
				t.Fatalf("save/reload lost persisted source state: %+v", *state)
			}
			env.PieceState = state
		}
		clip := javastructure.Bounds{Min: [3]int32{fixture.Clip[0], fixture.Clip[1], fixture.Clip[2]}, Max: [3]int32{fixture.Clip[3], fixture.Clip[4], fixture.Clip[5]}}
		post, err := piece.Place(clip, env)
		if err != nil {
			t.Fatal(err)
		}
		postTrace := desertPyramidTrace(t, post.Blocks, canWrite, rangeY)
		desertPyramidCommit(t, overlay, post.Blocks, canWrite, rangeY)
		bounds := mutableStructurePieceBounds(piece, state)
		after, err := piece.AfterPlace(clip, env, bounds)
		if err != nil {
			t.Fatal(err)
		}
		afterTrace := desertPyramidTrace(t, after.Blocks, canWrite, rangeY)
		desertPyramidCommit(t, overlay, after.Blocks, canWrite, rangeY)
		if len(post.Blocks) != fixture.PostCount || desertPyramidHash(postTrace) != fixture.PostHash {
			t.Errorf("%s post writes=%d hash=%s, Java=%d %s", fixture.Step, len(post.Blocks), desertPyramidHash(postTrace), fixture.PostCount, fixture.PostHash)
		}
		if len(after.Blocks) != fixture.AfterCount || desertPyramidHash(afterTrace) != fixture.AfterHash {
			t.Errorf("%s afterPlace writes=%d hash=%s, Java=%d %s", fixture.Step, len(after.Blocks), desertPyramidHash(afterTrace), fixture.AfterCount, fixture.AfterHash)
		}
		gotBox := [6]int32{bounds.Min[0], bounds.Min[1], bounds.Min[2], bounds.Max[0], bounds.Max[1], bounds.Max[2]}
		if gotBox != fixture.BBox || state.GroundHeight != fixture.HPos {
			t.Errorf("%s bbox/HPos=%v/%d, Java=%v/%d", fixture.Step, gotBox, state.GroundHeight, fixture.BBox, fixture.HPos)
		}
		var gotChests [4]bool
		gotChests = [4]bool{state.LootPlaced&1 != 0, state.LootPlaced&2 != 0, state.LootPlaced&4 != 0, state.LootPlaced&8 != 0}
		if gotChests != fixture.Chests {
			t.Errorf("%s chest completion=%v, Java=%v", fixture.Step, gotChests, fixture.Chests)
		}
		candidates := 0
		roof := [3]int32{}
		if state.Pyramid != nil {
			candidates = len(state.Pyramid.SandPositions)
			roof = state.Pyramid.RoofPosition
		}
		if candidates != fixture.CandidateCount || roof != fixture.Roof {
			t.Errorf("%s transient candidates/roof=%d/%v, Java=%d/%v", fixture.Step, candidates, roof, fixture.CandidateCount, fixture.Roof)
		}
	}
	if len(fixtures) == 0 {
		t.Fatal("empty Java sequence fixture")
	}
	if got := random.NextInt(1_000_000); got != fixtures[len(fixtures)-1].NextPostInt {
		t.Errorf("cached-HPos/reload post RNG=%d, Java=%d", got, fixtures[len(fixtures)-1].NextPostInt)
	}
	if got := levelRandom.NextInt(1_000_000); got != fixtures[len(fixtures)-1].NextLevelInt {
		t.Errorf("sequence region RNG=%d, Java=%d", got, fixtures[len(fixtures)-1].NextLevelInt)
	}
}

func desertPyramidTrace(t *testing.T, blocks []javastructure.Block, canWrite func(int32, int32, int32) bool, r cube.Range) []string {
	t.Helper()
	trace := make([]string, len(blocks))
	for i, b := range blocks {
		x, y, z := b.Pos[0], b.Pos[1], b.Pos[2]
		ok := y >= int32(r.Min()) && y <= int32(r.Max()) && canWrite(x, y, z)
		trace[i] = fmt.Sprintf("%d,%d,%d=%s:%t", x, y, z, desertPyramidState(b.State), ok)
	}
	return trace
}

func desertPyramidCommit(t *testing.T, overlay map[[3]int32]uint32, blocks []javastructure.Block, canWrite func(int32, int32, int32) bool, r cube.Range) {
	t.Helper()
	for _, b := range blocks {
		x, y, z := b.Pos[0], b.Pos[1], b.Pos[2]
		if y < int32(r.Min()) || y > int32(r.Max()) || !canWrite(x, y, z) {
			continue
		}
		id, err := javadata.BlockStateID(b.State.Name, b.State.Properties)
		if err != nil {
			t.Fatal(err)
		}
		overlay[b.Pos] = id
	}
}

func desertPyramidHash(trace []string) string {
	hash := sha256.Sum256([]byte(strings.Join(trace, "\n")))
	return hex.EncodeToString(hash[:])
}
