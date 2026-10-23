package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type mineshaftGraphFixture struct {
	Seed       int64    `json:"seed"`
	Structure  string   `json:"structure"`
	Chunk      [2]int32 `json:"chunk"`
	Start      [3]int32 `json:"start"`
	Bounds     [6]int32 `json:"bounds"`
	PieceCount int      `json:"piece_count"`
	PiecesHash string   `json:"pieces_sha256"`
	AfterInt   int32    `json:"after_int"`
}

type mineshaftGraphPiece struct {
	Box       [6]int32 `json:"box"`
	Kind      string   `json:"kind"`
	Depth     int      `json:"depth"`
	Direction string   `json:"direction,omitempty"`
	Sections  int      `json:"sections,omitempty"`
	Rails     bool     `json:"rails,omitempty"`
	Spider    bool     `json:"spider,omitempty"`
}

func TestMineshaftGraphsMatchReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_mineshaft_graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []mineshaftGraphFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*Overworld{}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fmt.Sprintf("seed_%d_%s_chunk_%d_%d", fixture.Seed, fixture.Structure, fixture.Chunk[0], fixture.Chunk[1]), func(t *testing.T) {
			generator := generators[fixture.Seed]
			if generator == nil {
				generator = NewOverworld(fixture.Seed)
				generators[fixture.Seed] = generator
			}
			wood := "normal"
			if fixture.Structure == "mineshaft_mesa" {
				wood = "mesa"
			}
			random := largeFeatureRandom(fixture.Seed, fixture.Chunk[0], fixture.Chunk[1])
			start, valid, err := buildModernMineshaft(modernStructureBuildInput{
				seed: fixture.Seed, name: "minecraft:mineshaft", chunkX: fixture.Chunk[0], chunkZ: fixture.Chunk[1],
				config: map[string]any{"mineshaft_type": wood}, source: modernTerrainSource{generator: generator, raw: true}, random: &random,
			})
			if err != nil {
				t.Fatal(err)
			}
			if !valid {
				t.Fatal("Java source produced a mineshaft graph")
			}
			pieces := make([]mineshaftGraphPiece, len(start.pieces))
			for i, raw := range start.pieces {
				p := raw.(minePiece)
				kind := map[string]string{"room": "MineShaftRoom", "corridor": "MineShaftCorridor", "crossing": "MineShaftCrossing", "stairs": "MineShaftStairs"}[p.kind]
				if kind == "" {
					t.Fatalf("unmapped mineshaft piece kind %q", p.kind)
				}
				direction := ""
				if p.kind != "room" {
					direction = p.facing
				}
				pieces[i] = mineshaftGraphPiece{
					Box:  [6]int32{p.box.Min[0], p.box.Min[1], p.box.Min[2], p.box.Max[0], p.box.Max[1], p.box.Max[2]},
					Kind: kind, Depth: p.depth, Direction: direction, Sections: p.sections, Rails: p.rails, Spider: p.spider,
				}
			}
			encoded, err := json.Marshal(pieces)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(encoded)
			if len(pieces) != fixture.PieceCount {
				t.Errorf("piece count = %d, want %d", len(pieces), fixture.PieceCount)
			}
			if got := hex.EncodeToString(sum[:]); got != fixture.PiecesHash {
				t.Errorf("ordered Java piece graph SHA-256 = %s, want %s", got, fixture.PiecesHash)
			}
			gotBounds := [6]int32{start.box.Min[0], start.box.Min[1], start.box.Min[2], start.box.Max[0], start.box.Max[1], start.box.Max[2]}
			if gotBounds != fixture.Bounds {
				t.Errorf("bounds = %v, want %v", gotBounds, fixture.Bounds)
			}
			gotStart := [3]int32{fixture.Chunk[0]*16 + 8, start.pieces[0].(minePiece).box.Min[1], fixture.Chunk[1] * 16}
			if gotStart != fixture.Start {
				t.Errorf("start = %v, want %v", gotStart, fixture.Start)
			}
			if got := random.NextInt(1_000_000); got != fixture.AfterInt {
				t.Errorf("post-generation Java RNG nextInt = %d, want %d", got, fixture.AfterInt)
			}
		})
	}
}

type mineshaftPlacementFixture struct {
	Name       string   `json:"name"`
	Seed       int64    `json:"seed"`
	Box        [6]int32 `json:"box"`
	Clip       [6]int32 `json:"clip"`
	AfterInt   int32    `json:"after_int"`
	Baseline   []string `json:"baseline"`
	PieceState struct {
		HasRails        bool   `json:"hasRails"`
		SpiderCorridor  bool   `json:"spiderCorridor"`
		HasPlacedSpider bool   `json:"hasPlacedSpider"`
		NumSections     int    `json:"numSections"`
		Facing          string `json:"facing"`
	} `json:"piece_state"`
	PieceStateBefore struct {
		HasPlacedSpider bool `json:"hasPlacedSpider"`
	} `json:"piece_state_before"`
	Writes   []string `json:"writes"`
	Post     []int    `json:"post"`
	Entities []string `json:"entities"`
	Actors   []struct {
		Pos string         `json:"pos"`
		NBT map[string]any `json:"nbt"`
	} `json:"actors"`
	EntityTraces []struct {
		Type      string     `json:"type"`
		Pos       [3]float64 `json:"pos"`
		LootTable string     `json:"lootTable"`
		LootSeed  string     `json:"lootSeed"`
	} `json:"entity_traces"`
}

func mineshaftFixtureBlockID(encoded string) (uint32, error) {
	open := strings.IndexByte(encoded, '[')
	name := encoded
	var properties map[string]any
	if open >= 0 {
		name = encoded[:open]
		properties = make(map[string]any)
		for _, property := range strings.Split(strings.TrimSuffix(encoded[open+1:], "]"), ",") {
			if property == "" {
				continue
			}
			key, value, ok := strings.Cut(property, "=")
			if !ok {
				return 0, fmt.Errorf("invalid Java block state %q", encoded)
			}
			properties[key] = value
		}
	}
	return gamedata.BlockStateID(name, properties)
}

func TestMineshaftPlacementsMatchReference(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_mineshaft_post.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []mineshaftPlacementFixture
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	airID, err := gamedata.BlockStateID("minecraft:air", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			box := structure.Box{Min: [3]int32{fixture.Box[0], fixture.Box[1], fixture.Box[2]}, Max: [3]int32{fixture.Box[3], fixture.Box[4], fixture.Box[5]}}
			clip := structure.Bounds{Min: [3]int32{fixture.Clip[0], fixture.Clip[1], fixture.Clip[2]}, Max: [3]int32{fixture.Clip[3], fixture.Clip[4], fixture.Clip[5]}}
			base := make(map[[3]int32]uint32, len(fixture.Baseline))
			for _, encoded := range fixture.Baseline {
				parts := strings.SplitN(encoded, "|", 2)
				coords := strings.Split(parts[0], ",")
				if len(parts) != 2 || len(coords) != 3 {
					t.Fatalf("invalid baseline record %q", encoded)
				}
				var pos [3]int32
				for i, value := range coords {
					parsed, e := strconv.ParseInt(value, 10, 32)
					if e != nil {
						t.Fatal(e)
					}
					pos[i] = int32(parsed)
				}
				id, e := mineshaftFixtureBlockID(parts[1])
				if e != nil {
					t.Fatal(e)
				}
				base[pos] = id
			}
			stateID := func(x, y, z int32) (uint32, error) {
				if id, ok := base[[3]int32{x, y, z}]; ok {
					return id, nil
				}
				return airID, nil
			}
			random := vanilla.NewLegacyRandom(fixture.Seed)
			env := structure.ProcessorEnvironment{BiomeAt: func(int32, int32, int32) string {
				if fixture.Name == "invalid_biome" {
					return "minecraft:deep_dark"
				}
				return "minecraft:plains"
			}, BaseHeight: func(int32, int32, bool) int32 { return 80 }, BlockStateID: stateID, Range: cube.Range{-64, 319}, Random: &random, PieceState: &structure.StructurePieceState{SpawnerPlaced: fixture.PieceStateBefore.HasPlacedSpider}}
			piece := minePiece{box: box, wood: "normal", depth: 1}
			switch {
			case strings.HasPrefix(fixture.Name, "corridor"), strings.HasPrefix(fixture.Name, "spider_"):
				piece.kind, piece.facing = "corridor", fixture.PieceState.Facing
				piece.sections, piece.rails, piece.spider = fixture.PieceState.NumSections, fixture.PieceState.HasRails, fixture.PieceState.SpiderCorridor
			case strings.HasPrefix(fixture.Name, "stairs_"):
				piece.kind, piece.facing = "stairs", strings.TrimPrefix(fixture.Name, "stairs_")
			case strings.HasPrefix(fixture.Name, "crossing_"):
				piece.kind, piece.facing = "crossing", strings.TrimPrefix(fixture.Name, "crossing_")
			default:
				piece.kind = "room"
			}
			placement, e := piece.Place(clip, env)
			if e != nil {
				t.Fatal(e)
			}
			if len(placement.Blocks) != len(fixture.Writes) {
				t.Fatalf("writes=%d, Java=%d", len(placement.Blocks), len(fixture.Writes))
			}
			for index, encoded := range fixture.Writes {
				parts := strings.Split(encoded, "|")
				if len(parts) != 3 {
					t.Fatalf("invalid Java write %q", encoded)
				}
				coords := strings.Split(parts[0], ",")
				var pos [3]int32
				for i, value := range coords {
					parsed, e := strconv.ParseInt(value, 10, 32)
					if e != nil {
						t.Fatal(e)
					}
					pos[i] = int32(parsed)
				}
				wantID, e := mineshaftFixtureBlockID(parts[1])
				if e != nil {
					t.Fatal(e)
				}
				got := placement.Blocks[index]
				gotID, e := gamedata.BlockStateID(got.State.Name, got.State.Properties)
				if e != nil {
					t.Fatal(e)
				}
				if got.Pos != pos || gotID != wantID {
					t.Errorf("write[%d] = %v state=%d, Java %v state=%d", index, got.Pos, gotID, pos, wantID)
					break
				}
			}
			expectedMarkers := append([]int{}, fixture.Post...)
			actualMarkers := make([]int, len(placement.PostProcessing))
			for i, pos := range placement.PostProcessing {
				y := int32(pos.Y())
				actualMarkers[i] = int((y+64)>>4)*4096 + (pos.X() & 15) + ((int(y) & 15) << 4) + ((pos.Z() & 15) << 8)
			}
			// ProtoChunk groups markers by section but retains duplicates and the
			// insertion order inside each section.
			sort.SliceStable(actualMarkers, func(i, j int) bool { return actualMarkers[i]>>12 < actualMarkers[j]>>12 })
			if !reflect.DeepEqual(actualMarkers, expectedMarkers) {
				t.Errorf("post-processing markers=%v, Java=%v", actualMarkers, expectedMarkers)
			}
			if got := random.NextInt(1_000_000); got != fixture.AfterInt {
				t.Errorf("postProcess RNG nextInt=%d, Java=%d", got, fixture.AfterInt)
			}
			if env.PieceState.SpawnerPlaced != fixture.PieceState.HasPlacedSpider {
				t.Errorf("spider placement state=%v, Java=%v", env.PieceState.SpawnerPlaced, fixture.PieceState.HasPlacedSpider)
			}
			if len(placement.Entities) != len(fixture.Entities) || len(placement.Entities) != len(fixture.EntityTraces) {
				t.Errorf("entities=%d, Java classes=%d traces=%d", len(placement.Entities), len(fixture.Entities), len(fixture.EntityTraces))
			} else {
				for i, entity := range placement.Entities {
					trace := fixture.EntityTraces[i]
					if !strings.HasSuffix(trace.Type, "MinecartChest") || entity.NBT["id"] != "minecraft:chest_minecart" || entity.Pos != trace.Pos || entity.NBT["LootTable"] != trace.LootTable || fmt.Sprint(entity.NBT["LootTableSeed"]) != trace.LootSeed {
						t.Errorf("entity[%d] payload=%v pos=%v, Java=%+v", i, entity.NBT, entity.Pos, trace)
					}
				}
			}
			sourceSpawners, nativeSpawners := 0, 0
			for _, actor := range fixture.Actors {
				if actor.NBT["id"] == "minecraft:mob_spawner" {
					sourceSpawners++
				}
			}
			for _, block := range placement.Blocks {
				if block.NBT != nil && block.NBT["id"] == "minecraft:mob_spawner" {
					nativeSpawners++
					for _, actor := range fixture.Actors {
						if actor.NBT["id"] == "minecraft:mob_spawner" && !reflect.DeepEqual(block.NBT["SpawnData"], actor.NBT["SpawnData"]) {
							t.Errorf("spawner SpawnData=%v, Java=%v", block.NBT["SpawnData"], actor.NBT["SpawnData"])
						}
					}
				}
			}
			if nativeSpawners != sourceSpawners {
				t.Errorf("spawner block entities=%d, Java=%d", nativeSpawners, sourceSpawners)
			}
			for _, block := range placement.Blocks {
				if block.NBT != nil && block.NBT["id"] == "minecraft:mob_spawner" && block.State.Name != "minecraft:spawner" {
					t.Errorf("spawner actor NBT attached to earlier state %s at %v", block.State.Name, block.Pos)
				}
			}
		})
	}
}

type mineshaftSupportFixture struct {
	Name     string   `json:"name"`
	Box      [6]int32 `json:"box"`
	Baseline []string `json:"baseline"`
	Writes   []string `json:"writes"`
}

func TestMineshaftSupportColumnsMatchReference(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_mineshaft_support.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []mineshaftSupportFixture
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	airID, err := gamedata.BlockStateID("minecraft:air", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			base := make(map[[3]int32]uint32, len(fixture.Baseline))
			for _, record := range fixture.Baseline {
				parts := strings.SplitN(record, "|", 2)
				coords := strings.Split(parts[0], ",")
				if len(parts) != 2 || len(coords) != 3 {
					t.Fatalf("invalid baseline record %q", record)
				}
				var pos [3]int32
				for i, value := range coords {
					parsed, e := strconv.ParseInt(value, 10, 32)
					if e != nil {
						t.Fatal(e)
					}
					pos[i] = int32(parsed)
				}
				id, e := mineshaftFixtureBlockID(parts[1])
				if e != nil {
					t.Fatal(e)
				}
				base[pos] = id
			}
			stateID := func(x, y, z int32) (uint32, error) {
				if id, ok := base[[3]int32{x, y, z}]; ok {
					return id, nil
				}
				return airID, nil
			}
			env := structure.ProcessorEnvironment{BlockStateID: stateID, Range: cube.Range{-64, 319}}
			piece := minePiece{box: structure.Box{Min: [3]int32{fixture.Box[0], fixture.Box[1], fixture.Box[2]}, Max: [3]int32{fixture.Box[3], fixture.Box[4], fixture.Box[5]}}, facing: "north"}
			clip := structure.Bounds{Min: [3]int32{0, -64, 0}, Max: [3]int32{15, 319, 15}}
			var placement structure.Placement
			journal := newJournalStructureWorld(env, clip, &placement)
			mineSupportColumn(piece, env, journal, 0, -1, 2, "oak_log", "oak_fence")
			if len(placement.Blocks) != len(fixture.Writes) {
				t.Fatalf("support writes=%d, Java=%d", len(placement.Blocks), len(fixture.Writes))
			}
			for index, encoded := range fixture.Writes {
				parts := strings.Split(encoded, "|")
				coords := strings.Split(parts[0], ",")
				var pos [3]int32
				for i, value := range coords {
					parsed, e := strconv.ParseInt(value, 10, 32)
					if e != nil {
						t.Fatal(e)
					}
					pos[i] = int32(parsed)
				}
				wantID, e := mineshaftFixtureBlockID(parts[1])
				if e != nil {
					t.Fatal(e)
				}
				got := placement.Blocks[index]
				gotID, e := gamedata.BlockStateID(got.State.Name, got.State.Properties)
				if e != nil {
					t.Fatal(e)
				}
				if got.Pos != pos || gotID != wantID {
					t.Errorf("support write[%d]=%v state=%d, Java %v state=%d", index, got.Pos, gotID, pos, wantID)
					break
				}
			}
		})
	}
}
