package overworld

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type scatteredSourceCase struct {
	Piece        string                `json:"piece"`
	Seed         string                `json:"seed"`
	RenderSeed   string                `json:"renderSeed"`
	RegionSeed   string                `json:"regionRandomSeed"`
	RegionOrigin [3]int32              `json:"regionRandomOrigin"`
	Orientation  string                `json:"orientation"`
	Scenario     string                `json:"scenario"`
	InitialBox   [6]int32              `json:"initialBox"`
	Calls        []scatteredSourceCall `json:"calls"`
}
type scatteredSourceCall struct {
	Clip          [6]int32          `json:"clip"`
	Writes        []string          `json:"writes"`
	Markers       [][3]int32        `json:"markers"`
	Flags         map[string]bool   `json:"flags"`
	Box           [6]int32          `json:"bbox"`
	HPos          int32             `json:"hPos"`
	RenderNext    string            `json:"nextLongRender"`
	RegionNext    string            `json:"nextLongRegion"`
	BlockEntities map[string]string `json:"blockEntities"`
	Entities      []map[string]any  `json:"entities"`
}
type scatteredNativeCall struct {
	Writes        []string                  `json:"writes"`
	Markers       [][3]int32                `json:"markers"`
	Flags         map[string]bool           `json:"flags"`
	Box           [6]int32                  `json:"bbox"`
	HPos          int32                     `json:"hPos"`
	RenderNext    string                    `json:"nextLongRender"`
	RegionNext    string                    `json:"nextLongRegion"`
	BlockEntities map[string]map[string]any `json:"blockEntities,omitempty"`
	Entities      []map[string]any          `json:"entities,omitempty"`
}

type scatteredFixtureWorld struct {
	scenario          string
	states            map[[3]int32]uint32
	actors            map[[3]int32]map[string]any
	markers           [][3]int32
	entities          []map[string]any
	air, stone, water uint32
}

func newScatteredFixtureWorld(scenario string) *scatteredFixtureWorld {
	air, _ := javadata.BlockStateID("minecraft:air", nil)
	stone, _ := javadata.BlockStateID("minecraft:stone", nil)
	water, _ := javadata.BlockStateID("minecraft:water", nil)
	return &scatteredFixtureWorld{scenario: scenario, states: make(map[[3]int32]uint32), actors: make(map[[3]int32]map[string]any), air: air, stone: stone, water: water}
}
func (w *scatteredFixtureWorld) state(x, y, z int32) (uint32, error) {
	if id, ok := w.states[[3]int32{x, y, z}]; ok {
		return id, nil
	}
	if x>>4 < -1 || x>>4 > 1 || z>>4 < -1 || z>>4 > 1 {
		return w.air, nil
	}
	h := int32(62) + ((3*x+5*z)%7+7)%7
	bottom := int32(0)
	if w.scenario == "below_zero" {
		h, bottom = -5, -10
	}
	if w.scenario == "no_samples" {
		h = 64
	}
	if y >= bottom && y <= h {
		return w.stone, nil
	}
	if w.scenario == "fluid_overlay" && x&15 == 8 && z&15 == 8 && y == h+1 {
		return w.water, nil
	}
	return w.air, nil
}
func (w *scatteredFixtureWorld) height(x, z int32) int32 {
	for y := int32(319); y >= -64; y-- {
		id, _ := w.state(x, y, z)
		name, _ := javadata.BlockStateName(id)
		if javaBlockTagContains("blocks_motion_in_heightmap_no_leaves", name) || name == "minecraft:water" || name == "minecraft:lava" {
			return y + 1
		}
	}
	return -64
}
func (w *scatteredFixtureWorld) canWrite(x, y, z int32) bool {
	originX := int32(0)
	if w.scenario == "clipped" {
		originX = 2
	}
	return y >= -64 && y <= 319 && absFeatureInt(x>>4-originX) <= 1 && absFeatureInt(z>>4) <= 1
}
func scatteredCanonicalState(id uint32) string {
	name, props, ok := javadata.BlockStateFromID(id)
	if !ok {
		panic(fmt.Sprintf("missing state %d", id))
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]string, len(keys))
	for i, k := range keys {
		fields[i] = k + "=" + fmt.Sprint(props[k])
	}
	return name + "[" + strings.Join(fields, ",") + "]"
}
func (w *scatteredFixtureWorld) apply(placement javastructure.Placement) []string {
	writes := make([]string, 0, len(placement.Blocks))
	for _, b := range placement.Blocks {
		id, err := javadata.BlockStateID(b.State.Name, b.State.Properties)
		if err != nil {
			panic(err)
		}
		ok := w.canWrite(b.Pos[0], b.Pos[1], b.Pos[2])
		writes = append(writes, fmt.Sprintf("%d,%d,%d=%s:%t", b.Pos[0], b.Pos[1], b.Pos[2], scatteredCanonicalState(id), ok))
		if !ok {
			continue
		}
		old, _ := w.state(b.Pos[0], b.Pos[1], b.Pos[2])
		oldName, _ := javadata.BlockStateName(old)
		if oldName != b.State.Name {
			delete(w.actors, b.Pos)
		}
		w.states[b.Pos] = id
		if b.NBT != nil {
			w.actors[b.Pos] = b.NBT
		}
	}
	for _, p := range placement.PostProcessing {
		w.markers = append(w.markers, [3]int32{int32(p[0]), int32(p[1]), int32(p[2])})
	}
	for _, e := range placement.Entities {
		m := map[string]any{"id": e.NBT["id"], "position": e.Pos, "rotation": e.NBT["Rotation"], "persistent": e.NBT["PersistenceRequired"], "health": e.NBT["Health"]}
		attribute := e.NBT["attributes"].([]any)[0].(map[string]any)
		modifier := attribute["modifiers"].([]any)[0].(map[string]any)
		base, amount := attribute["base"].(float64), modifier["amount"].(float64)
		m["followRange"] = map[string]any{"base": base, "value": base + base*amount, "random_spawn_bonus": modifier}
		m["leftHanded"] = e.NBT["LeftHanded"] == byte(1)
		if e.NBT["id"] == "minecraft:cat" {
			m["variant"] = e.NBT["variant"]
			m["soundVariant"] = e.NBT["sound_variant"]
			m["collar"] = "red"
		}
		w.entities = append(w.entities, m)
	}
	return writes
}
func (w *scatteredFixtureWorld) orderedMarkers() [][3]int32 {
	ordered := append([][3]int32{}, w.markers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a[0]>>4 != b[0]>>4 {
			return a[0]>>4 < b[0]>>4
		}
		if a[2]>>4 != b[2]>>4 {
			return a[2]>>4 < b[2]>>4
		}
		return (a[1]+64)>>4 < (b[1]+64)>>4
	})
	for i, p := range ordered {
		ordered[i] = [3]int32{p[0] >> 4, p[2] >> 4, ((p[1]+64)>>4)*4096 + (p[0] & 15) + (p[1]&15)*16 + (p[2]&15)*256}
	}
	return ordered
}
func (w *scatteredFixtureWorld) actorProjection() map[string]map[string]any {
	out := make(map[string]map[string]any)
	for p, m := range w.actors {
		a := map[string]any{"id": m["id"]}
		if table, ok := m["LootTable"].(string); ok {
			a["LootTable"] = table
			a["LootTableSeed"] = fmt.Sprint(m["LootTableSeed"])
		}
		out[fmt.Sprintf("%d,%d,%d", p[0], p[1], p[2])] = a
	}
	return out
}
func runScatteredNativeCase(f scatteredSourceCase) ([]scatteredNativeCall, error) {
	seed, err := strconv.ParseInt(f.Seed, 10, 64)
	if err != nil {
		return nil, err
	}
	renderSeed, err := strconv.ParseInt(f.RenderSeed, 10, 64)
	if err != nil {
		return nil, err
	}
	regionSeed, err := strconv.ParseInt(f.RegionSeed, 10, 64)
	if err != nil {
		return nil, err
	}
	box := javastructure.Box{Min: [3]int32{f.InitialBox[0], f.InitialBox[1], f.InitialBox[2]}, Max: [3]int32{f.InitialBox[3], f.InitialBox[4], f.InitialBox[5]}}
	var piece modernStructurePiece
	if f.Piece == "jungle_temple" {
		dir := 0
		for i, v := range [4]string{"north", "east", "south", "west"} {
			if v == f.Orientation {
				dir = i
			}
		}
		piece = modernJungleTemple{box: box, dir: dir}
	} else {
		piece = modernSwampHutPiece{box: box, direction: f.Orientation, seed: seed}
	}
	w := newScatteredFixtureWorld(f.Scenario)
	if f.Scenario == "occupied" {
		var total, count int32
		for z := box.Min[2]; z <= box.Max[2]; z++ {
			for x := box.Min[0]; x <= box.Max[0]; x++ {
				total += w.height(x, z)
				count++
			}
		}
		moved := box
		moved.Min[1] = total / count
		moved.Max[1] = moved.Min[1] + box.Max[1] - box.Min[1]
		for _, local := range [3][3]int32{{8, -3, 3}, {9, -3, 10}, {9, -2, 3}} {
			p := modernJungleTemple{box: moved, dir: 0}.pos(local[0], local[1], local[2])
			name := "minecraft:chest"
			if local[0] == 9 && local[2] == 3 {
				name = "minecraft:dispenser"
			}
			id, _ := javadata.BlockStateID(name, nil)
			w.states[p] = id
			w.actors[p] = map[string]any{"id": name}
		}
	}
	random := java26.NewLegacyRandom(renderSeed)
	root := java26.NewXoroshiroRandom(regionSeed)
	named := root.ForkPositional().FromHashOf("minecraft:worldgen_region_random")
	level := named.ForkPositional().At(f.RegionOrigin[0], f.RegionOrigin[1], f.RegionOrigin[2])
	start := modernStructureStart{key: modernStructureStartKey{set: f.Piece}, name: "minecraft:" + f.Piece, pieces: []modernStructurePiece{piece}, mutablePieces: true, box: box}
	generator := &Overworld{seed: seed, modernStructures: new(modernStructures)}
	result := make([]scatteredNativeCall, 0, len(f.Calls))
	for index, c := range f.Calls {
		if index == 1 && f.Scenario == "split_reload" {
			saved, e := generator.SaveGenerationState()
			if e != nil {
				return nil, e
			}
			generator = &Overworld{seed: seed, modernStructures: new(modernStructures)}
			if e = generator.LoadGenerationState(saved); e != nil {
				return nil, e
			}
		}
		ctx := modernStructureContext{pieceStates: &generator.modernStructures.pieceStates}
		env := javastructure.ProcessorEnvironment{Range: cube.Range{-64, 319}, Random: &random, LevelRandom: &level, JavaBlockStateID: w.state, CanWriteBlock: w.canWrite, MotionBlockingNoLeavesHeight: w.height}
		clip := javastructure.Bounds{Min: [3]int32{c.Clip[0], c.Clip[1], c.Clip[2]}, Max: [3]int32{c.Clip[3], c.Clip[4], c.Clip[5]}}
		var writes []string
		ctx.placeStart(start, clip, env, func(p javastructure.Placement) { writes = w.apply(p) })
		flags := generator.modernStructures.pieceStates.lookup(start.key)
		flags.mu.Lock()
		state := flags.pieces[0]
		flags.mu.Unlock()
		bounds := mutableStructurePieceBounds(piece, &state)
		hPos := int32(-1)
		if state.GroundHeightSet {
			hPos = state.GroundHeight
		}
		m := map[string]bool{}
		if f.Piece == "jungle_temple" {
			m = map[string]bool{"placedMainChest": state.LootPlaced&jungleTempleMainChest != 0, "placedHiddenChest": state.LootPlaced&jungleTempleHiddenChest != 0, "placedTrap1": state.LootPlaced&jungleTempleTrap1 != 0, "placedTrap2": state.LootPlaced&jungleTempleTrap2 != 0}
		} else {
			m = map[string]bool{"spawnedWitch": state.EntitiesSpawned&swampHutWitch != 0, "spawnedCat": state.EntitiesSpawned&swampHutCat != 0}
		}
		renderCopy, levelCopy := random, level
		result = append(result, scatteredNativeCall{Writes: writes, Markers: w.orderedMarkers(), Flags: m, Box: [6]int32{bounds.Min[0], bounds.Min[1], bounds.Min[2], bounds.Max[0], bounds.Max[1], bounds.Max[2]}, HPos: hPos, RenderNext: fmt.Sprint(renderCopy.NextLong()), RegionNext: fmt.Sprint(levelCopy.NextLong()), BlockEntities: w.actorProjection(), Entities: w.entities})
	}
	return result, nil
}
func TestScatteredPostProcessMatchesJava263(t *testing.T) {
	file, err := os.Open("testdata/java263_scattered_post.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer compressed.Close()
	data, err := io.ReadAll(compressed)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Cases []scatteredSourceCase `json:"cases"`
	}
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	idPattern := regexp.MustCompile(`id:"([^"]+)"`)
	tablePattern := regexp.MustCompile(`LootTable:"([^"]+)"`)
	seedPattern := regexp.MustCompile(`LootTableSeed:(-?\d+)L`)
	for index, fixture := range input.Cases {
		t.Run(fmt.Sprintf("%02d_%s_%s_%s_seed_%s", index, fixture.Piece, fixture.Orientation, fixture.Scenario, fixture.Seed), func(t *testing.T) {
			actual, err := runScatteredNativeCase(fixture)
			if err != nil {
				t.Fatal(err)
			}
			for call, want := range fixture.Calls {
				got := actual[call]
				if len(got.Writes) != len(want.Writes) {
					t.Fatalf("call%d: ordered writes=%d, Java=%d", call, len(got.Writes), len(want.Writes))
				}
				for row, write := range want.Writes {
					if got.Writes[row] != write {
						t.Fatalf("call%d write%d: %s, Java %s", call, row, got.Writes[row], write)
					}
				}
				if len(got.Markers) != len(want.Markers) {
					t.Fatalf("call%d: markers=%v, Java=%v", call, got.Markers, want.Markers)
				}
				for row, marker := range want.Markers {
					if got.Markers[row] != marker {
						t.Fatalf("call%d marker%d: %v, Java %v", call, row, got.Markers[row], marker)
					}
				}
				if !reflect.DeepEqual(got.Flags, want.Flags) || got.Box != want.Box || got.HPos != want.HPos {
					t.Fatalf("call%d source state: flags=%v box=%v HPos=%d; Java flags=%v box=%v HPos=%d", call, got.Flags, got.Box, got.HPos, want.Flags, want.Box, want.HPos)
				}
				if got.RenderNext != want.RenderNext || got.RegionNext != want.RegionNext {
					t.Fatalf("call%d source RNG: render/region=%s/%s, Java=%s/%s", call, got.RenderNext, got.RegionNext, want.RenderNext, want.RegionNext)
				}
				if fixture.Piece == "swamp_hut" {
					expected := make([]map[string]any, len(want.Entities))
					for row, entity := range want.Entities {
						projection := make(map[string]any, len(entity)-2)
						for key, value := range entity {
							if key != "entityId" && key != "savedEntityNbt" {
								projection[key] = value
							}
						}
						expected[row] = projection
					}
					if len(got.Entities) != len(expected) {
						t.Fatalf("call%d mob count=%d Java=%d", call, len(got.Entities), len(expected))
					}
					for row, mob := range expected {
						native, err := json.Marshal(got.Entities[row])
						if err != nil {
							t.Fatal(err)
						}
						source, err := json.Marshal(mob)
						if err != nil {
							t.Fatal(err)
						}
						if string(native) != string(source) {
							t.Fatalf("call%d mob%d: %s, Java %s", call, row, native, source)
						}
					}
				} else {
					expected := make(map[string]map[string]any, len(want.BlockEntities))
					for pos, snbt := range want.BlockEntities {
						match := idPattern.FindStringSubmatch(snbt)
						if match == nil {
							t.Fatalf("actor at%s has no source id: %s", pos, snbt)
						}
						actor := map[string]any{"id": match[1]}
						if table := tablePattern.FindStringSubmatch(snbt); table != nil {
							seed := seedPattern.FindStringSubmatch(snbt)
							if seed == nil {
								t.Fatalf("loot actor at%s has no source seed: %s", pos, snbt)
							}
							actor["LootTable"], actor["LootTableSeed"] = table[1], seed[1]
						}
						expected[pos] = actor
					}
					if !reflect.DeepEqual(got.BlockEntities, expected) {
						t.Fatalf("call%d loot actors=%v, Java=%v", call, got.BlockEntities, expected)
					}
				}
			}
		})
	}
}

func TestScatteredConstructorsMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_scattered_constructors.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Type      string   `json:"type"`
		Seed      string   `json:"seed"`
		Chunk     [2]int32 `json:"chunk"`
		Direction string   `json:"direction"`
		Box       [6]int32 `json:"box"`
		NextLong  string   `json:"next_long"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fmt.Sprintf("%s_seed%s_%d_%d", fixture.Type, fixture.Seed, fixture.Chunk[0], fixture.Chunk[1]), func(t *testing.T) {
			seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			random := java26.NewLegacyRandom(seed)
			in := modernStructureBuildInput{seed: seed, name: "minecraft:" + fixture.Type, chunkX: fixture.Chunk[0], chunkZ: fixture.Chunk[1], random: &random}
			var start modernStructureStart
			var valid bool
			if fixture.Type == "jungle_temple" {
				start, valid, err = buildModernJungleTemple(in)
			} else {
				start, valid, err = buildModernSwampHut(in)
			}
			if err != nil || !valid {
				t.Fatalf("source constructor: valid=%t err=%v", valid, err)
			}
			direction := ""
			switch piece := start.pieces[0].(type) {
			case modernJungleTemple:
				direction = [4]string{"north", "east", "south", "west"}[piece.dir]
			case modernSwampHutPiece:
				direction = piece.direction
			}
			box := start.box
			if got := ([6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}); got != fixture.Box || direction != fixture.Direction {
				t.Fatalf("constructor bbox=%v orientation=%s, Java=%v/%s", got, direction, fixture.Box, fixture.Direction)
			}
			if got := fmt.Sprint(random.NextLong()); got != fixture.NextLong {
				t.Fatalf("constructor RNG=%s, Java=%s", got, fixture.NextLong)
			}
		})
	}
}
