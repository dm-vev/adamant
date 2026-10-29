package nether

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/dm-vev/adamant/net/nbt"
)

type fortressStateSourcePiece struct {
	Index   int      `json:"index"`
	Type    string   `json:"type"`
	Box     [6]int32 `json:"box"`
	Initial string   `json:"initial"`
}

type fortressStateSourceCall struct {
	Chunk       string            `json:"chunk"`
	FlagsBefore map[string]string `json:"flags_before"`
	Writes      []string          `json:"writes"`
	Markers     []string          `json:"markers"`
	Actors      map[string]string `json:"actors"`
	RNGNextLong int64             `json:"rng_next_long"`
	FlagsAfter  map[string]string `json:"flags_after"`
}

type fortressStateSourceFixture struct {
	Seed   int64                      `json:"seed"`
	Start  [2]int32                   `json:"start"`
	Pieces []fortressStateSourcePiece `json:"pieces"`
	Calls  []fortressStateSourceCall  `json:"calls"`
}

func readFortressStateSourceFixture(t *testing.T) fortressStateSourceFixture {
	t.Helper()
	file, err := os.Open("testdata/java263_fortress_state.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	var fixture fortressStateSourceFixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func fortressStatefulPiece(piece bridgePiece) bool {
	switch piece.(type) {
	case *netherBridgeCorridor, *netherBridgeCorridor2, *netherBridgeThrone:
		return true
	default:
		return false
	}
}

func fortressPieceName(piece bridgePiece) string {
	switch piece.(type) {
	case *netherBridgeThrone:
		return "MonsterThrone"
	case *netherBridgeCorridor:
		return "CastleSmallCorridorLeftTurnPiece"
	case *netherBridgeCorridor2:
		return "CastleSmallCorridorRightTurnPiece"
	default:
		return fmt.Sprintf("%T", piece)
	}
}

func fortressSourceFlags(pieces []bridgePiece, flags []structure.StructurePieceState) map[string]string {
	out := make(map[string]string)
	for i, piece := range pieces {
		switch p := piece.(type) {
		case *netherBridgeCorridor:
			out[strconv.Itoa(i)] = strconv.FormatBool(p.chest && !flags[i].ChestPlaced)
		case *netherBridgeCorridor2:
			out[strconv.Itoa(i)] = strconv.FormatBool(p.chest && !flags[i].ChestPlaced)
		case *netherBridgeThrone:
			out[strconv.Itoa(i)] = strconv.FormatBool(flags[i].SpawnerPlaced)
		}
	}
	return out
}

func fortressWriteString(write structure.Block) string {
	keys := make([]string, 0, len(write.State.Properties))
	for key := range write.State.Properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	properties := make([]string, 0, len(keys))
	for _, key := range keys {
		properties = append(properties, key+"="+fmt.Sprint(write.State.Properties[key]))
	}
	return fmt.Sprintf("%d,%d,%d=%s[%s]", write.Pos[0], write.Pos[1], write.Pos[2], write.State.Name, strings.Join(properties, ","))
}

func fortressPosString(pos [3]int32) string {
	return fmt.Sprintf("%d,%d,%d", pos[0], pos[1], pos[2])
}

func fortressSourceActorKeys(actors map[string]string) []string {
	keys := make([]string, 0, len(actors))
	for key := range actors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func fortressPlacementActorKeys(actors map[string]map[string]any) []string {
	keys := make([]string, 0, len(actors))
	for key := range actors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func TestFortressStatefulPostProcessMatchesReference(t *testing.T) {
	fixture := readFortressStateSourceFixture(t)
	g := NewNether(fixture.Seed)
	start, err := g.complexStart(fixture.Start[0], fixture.Start[1])
	if err != nil {
		t.Fatal(err)
	}
	if start == nil || start.fortress == nil {
		t.Fatal("Java source fixture start did not rebuild as a fortress")
	}
	pieces := start.fortress.pieces
	if len(pieces) != len(fixture.Pieces) {
		t.Fatalf("fortress graph has %d pieces, Java source has %d", len(pieces), len(fixture.Pieces))
	}
	for _, want := range fixture.Pieces {
		if want.Initial == "" {
			continue
		}
		piece := pieces[want.Index]
		base := piece.pieceBase()
		gotBox := [6]int32{int32(base.bb.minX), int32(base.bb.minY), int32(base.bb.minZ), int32(base.bb.maxX), int32(base.bb.maxY), int32(base.bb.maxZ)}
		if gotBox != want.Box || fortressPieceName(piece) != want.Type {
			t.Fatalf("piece %d geometry/type=%v/%s, Java=%v/%s", want.Index, gotBox, fortressPieceName(piece), want.Box, want.Type)
		}
		var gotInitial bool
		switch p := piece.(type) {
		case *netherBridgeCorridor:
			gotInitial = p.chest
		case *netherBridgeCorridor2:
			gotInitial = p.chest
		case *netherBridgeThrone:
			gotInitial = p.hasSpawner
		}
		if gotInitial != (want.Initial == "true") {
			t.Fatalf("piece %d initial source flag=%v, Java=%s", want.Index, gotInitial, want.Initial)
		}
	}

	key := fortressStartKey{x: fixture.Start[0], z: fixture.Start[1]}
	state := g.fortressStates.forStart(key, pieces)
	baseBlocks := make(map[cube.Pos]world.Block)
	actors := make(map[string]map[string]any)
	state.mu.Lock()
	defer state.mu.Unlock()
	for callIndex, want := range fixture.Calls {
		if got := fortressSourceFlags(pieces, state.pieces); !reflect.DeepEqual(got, want.FlagsBefore) {
			t.Fatalf("call %d pre-state=%v, Java=%v", callIndex, got, want.FlagsBefore)
		}
		coords := strings.Split(want.Chunk, ",")
		chunkX, err := strconv.Atoi(coords[0])
		if err != nil {
			t.Fatal(err)
		}
		chunkZ, err := strconv.Atoi(coords[1])
		if err != nil {
			t.Fatal(err)
		}
		pos := world.ChunkPos{int32(chunkX), int32(chunkZ)}
		random := g.featureSource.structureStepRandom(pos, 1, 7)
		env := structure.ProcessorEnvironment{
			BaseBlock: func(x, y, z int32) world.Block {
				if b := baseBlocks[cube.Pos{int(x), int(y), int(z)}]; b != nil {
					return b
				}
				return block.Air{}
			},
			Random: random,
		}
		region := &journalFortressRegion{
			chunkX: int(chunkX), chunkZ: int(chunkZ), env: env,
			registry: world.DefaultBlockRegistry, writes: make(map[cube.Pos]uint32),
		}
		clip := structureBB{minX: chunkX << 4, minY: 0, minZ: chunkZ << 4, maxX: (chunkX << 4) + 15, maxY: 127, maxZ: (chunkZ << 4) + 15}
		placement := structure.Placement{}
		for i, piece := range pieces {
			if !fortressStatefulPiece(piece) || !piece.pieceBase().bb.intersects(clip) {
				continue
			}
			renderFortressPiece(piece, region, chunkX, chunkZ, clip, random, g, &placement, &state.pieces[i])
		}
		gotWrites := make([]string, len(placement.Blocks))
		for i, write := range placement.Blocks {
			gotWrites[i] = fortressWriteString(write)
			name := write.State.Name
			if name == "minecraft:chest" || name == "minecraft:spawner" {
				if write.NBT != nil {
					actors[fortressPosString(write.Pos)] = write.NBT
				}
			} else {
				delete(actors, fortressPosString(write.Pos))
			}
			native, err := world.ResolveState(world.DefaultBlockRegistry, write.State)
			if err != nil {
				t.Fatal(err)
			}
			placed, ok := world.DefaultBlockRegistry.BlockByRuntimeID(native.Block)
			if !ok {
				t.Fatalf("cannot restore fortress native state %s %v", name, write.State.Properties)
			}
			baseBlocks[cube.Pos{int(write.Pos[0]), int(write.Pos[1]), int(write.Pos[2])}] = placed
		}
		if !reflect.DeepEqual(gotWrites, want.Writes) {
			limit := min(len(gotWrites), len(want.Writes))
			first := 0
			for first < limit && gotWrites[first] == want.Writes[first] {
				first++
			}
			t.Fatalf("call %d ordered writes differ at %d (native=%d Java=%d): native=%q Java=%q", callIndex, first, len(gotWrites), len(want.Writes), fortressSliceAt(gotWrites, first), fortressSliceAt(want.Writes, first))
		}
		gotMarkers := make([]string, len(placement.PostProcessing))
		for i, marker := range placement.PostProcessing {
			gotMarkers[i] = fmt.Sprintf("%d,%d,%d", marker[0], marker[1], marker[2])
		}
		wantMarkers := append([]string(nil), want.Markers...)
		sort.Strings(gotMarkers)
		sort.Strings(wantMarkers)
		if len(gotMarkers) != len(wantMarkers) {
			t.Fatalf("call %d post-processing markers=%v, Java=%v", callIndex, gotMarkers, wantMarkers)
		}
		for i, marker := range gotMarkers {
			if marker != wantMarkers[i] {
				t.Fatalf("call %d marker %d=%s, Java=%s", callIndex, i, marker, wantMarkers[i])
			}
		}
		if got := random.NextLong(); got != want.RNGNextLong {
			t.Fatalf("call %d RNG nextLong=%d, Java=%d", callIndex, got, want.RNGNextLong)
		}
		if got := fortressSourceFlags(pieces, state.pieces); !reflect.DeepEqual(got, want.FlagsAfter) {
			t.Fatalf("call %d post-state=%v, Java=%v", callIndex, got, want.FlagsAfter)
		}
		if got, expected := fortressPlacementActorKeys(actors), fortressSourceActorKeys(want.Actors); !reflect.DeepEqual(got, expected) {
			t.Fatalf("call %d block entities=%v, Java=%v", callIndex, got, expected)
		}
		for actorPos, sourceTag := range want.Actors {
			actor := actors[actorPos]
			if strings.Contains(sourceTag, "LootTableSeed:") {
				start := strings.Index(sourceTag, "LootTableSeed:") + len("LootTableSeed:")
				end := strings.IndexByte(sourceTag[start:], 'L')
				if end < 0 {
					t.Fatalf("source chest tag has no loot seed: %s", sourceTag)
				}
				seed, err := strconv.ParseInt(sourceTag[start:start+end], 10, 64)
				if err != nil || actor["LootTableSeed"] != seed || actor["LootTable"] != "minecraft:chests/nether_bridge" {
					t.Fatalf("native chest actor %v does not match Java tag %s", actor, sourceTag)
				}
			}
			if strings.Contains(sourceTag, "SpawnData:{entity:{id:\"minecraft:blaze\"}}") {
				spawn, _ := actor["SpawnData"].(map[string]any)
				entity, _ := spawn["entity"].(map[string]any)
				if entity["id"] != "minecraft:blaze" {
					t.Fatalf("native fortress spawner actor=%v, Java=%s", actor, sourceTag)
				}
			}
		}
	}
	for i, piece := range pieces {
		if p, ok := piece.(*netherBridgeCorridor); ok && p.chest != (fixture.Pieces[i].Initial == "true") {
			t.Fatalf("cached corridor %d chest flag was mutated", i)
		}
		if p, ok := piece.(*netherBridgeCorridor2); ok && p.chest != (fixture.Pieces[i].Initial == "true") {
			t.Fatalf("cached corridor2 %d chest flag was mutated", i)
		}
		if p, ok := piece.(*netherBridgeThrone); ok && p.hasSpawner {
			t.Fatalf("cached throne %d spawner flag was mutated", i)
		}
	}
}

func fortressSliceAt(values []string, index int) []string {
	if index >= len(values) {
		return nil
	}
	end := min(index+3, len(values))
	return values[index:end]
}

func fortressPlacementAt(placement structure.Placement, pos cube.Pos) (structure.Block, bool) {
	for i := len(placement.Blocks) - 1; i >= 0; i-- {
		write := placement.Blocks[i]
		if write.Pos == [3]int32{int32(pos[0]), int32(pos[1]), int32(pos[2])} {
			return write, true
		}
	}
	return structure.Block{}, false
}

func fortressRenderCall(g *Nether, pos world.ChunkPos, transient bool, base func(int32, int32, int32) world.Block) structure.Placement {
	var placement structure.Placement
	g.placeFortressStructureStep(pos, structure.ProcessorEnvironment{
		BaseBlock: base, Random: g.featureSource.structureStepRandom(pos, 1, 7),
		Range: cube.Range{0, 127}, TransientStructureState: transient,
	}, func(p structure.Placement) { placement = p })
	return placement
}

func TestFortressStateSurvivesMCDBRestartPreviewAndCacheEviction(t *testing.T) {
	const seed = int64(1)
	chestPos := cube.Pos{-207, 65, -796}
	chestChunk := world.ChunkPos{int32(chestPos[0] >> 4), int32(chestPos[2] >> 4)}
	startKey := fortressStartKey{x: -14, z: -54}
	base := func(int32, int32, int32) world.Block { return block.Air{} }

	beforeRestart := NewNether(seed)
	if err := beforeRestart.LoadGenerationState(nil); err != nil {
		t.Fatal(err)
	}
	first := fortressRenderCall(beforeRestart, chestChunk, false, base)
	chest, ok := fortressPlacementAt(first, chestPos)
	if !ok || chest.State.Name != "minecraft:chest" || chest.NBT["LootTable"] != "minecraft:chests/nether_bridge" || chest.NBT["LootTableSeed"] != int64(-8055585923370692160) {
		t.Fatalf("first target-chunk postProcess chest=%+v, want Java loot actor", chest)
	}
	state := beforeRestart.fortressStates.lookup(startKey)
	if state == nil {
		t.Fatal("first target-chunk postProcess did not retain fortress start state")
	}
	state.mu.Lock()
	if len(state.pieces) != 56 || !state.pieces[27].ChestPlaced {
		state.mu.Unlock()
		t.Fatalf("first target-chunk postProcess state=%+v", state.pieces)
	}
	state.mu.Unlock()
	saved, err := beforeRestart.SaveGenerationState()
	if err != nil {
		t.Fatal(err)
	}

	directory := t.TempDir()
	db, err := mcdb.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveWorldData("generation:1", saved); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = mcdb.Open(directory)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := db.LoadWorldData("generation:1")
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	afterRestart := NewNether(seed)
	if err := afterRestart.LoadGenerationState(recovered); err != nil {
		t.Fatal(err)
	}
	beforePreview, err := afterRestart.SaveGenerationState()
	if err != nil {
		t.Fatal(err)
	}
	preview := fortressRenderCall(afterRestart, chestChunk, true, afterRestart.featureSource.BaseBlock)
	if placed, ok := fortressPlacementAt(preview, chestPos); ok && placed.State.Name == "minecraft:chest" {
		t.Fatal("transient fortress preview consumed a persisted chest flag and recreated its chest")
	}
	afterPreview, err := afterRestart.SaveGenerationState()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforePreview, afterPreview) {
		t.Fatal("transient fortress preview changed saved start state")
	}

	// Drop the rebuilt graph and both retained terrain views before the real
	// target call. State must remain keyed by source start, not either cache.
	afterRestart.complexMu.Lock()
	delete(afterRestart.complexStarts, world.ChunkPos{startKey.x, startKey.z})
	afterRestart.complexMu.Unlock()
	afterRestart.featureSource.mu.Lock()
	afterRestart.featureSource.chunks = make(map[world.ChunkPos]*chunk.Chunk)
	afterRestart.featureSource.order = nil
	afterRestart.featureSource.terrain = newNetherTerrainCache(2048)
	afterRestart.featureSource.mu.Unlock()

	replayed := fortressRenderCall(afterRestart, chestChunk, false, afterRestart.featureSource.BaseBlock)
	if placed, ok := fortressPlacementAt(replayed, chestPos); ok && placed.State.Name == "minecraft:chest" {
		t.Fatal("reopened fortress state replayed an already placed chest after graph/terrain cache eviction")
	}
	afterReplay, err := afterRestart.SaveGenerationState()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforePreview, afterReplay) {
		t.Fatal("normal replay changed persisted fortress flags after MCDB restart")
	}
}

func TestFortressChestFlagConsumesWhenChestCreationFails(t *testing.T) {
	g := NewNether(1)
	g.chestRID = 0
	pos := world.ChunkPos{-13, -50}
	base := func(int32, int32, int32) world.Block { return block.Air{} }
	placement := fortressRenderCall(g, pos, false, base)
	if placed, ok := fortressPlacementAt(placement, cube.Pos{-207, 65, -796}); ok && placed.State.Name == "minecraft:chest" {
		t.Fatal("missing chest runtime ID unexpectedly placed a chest")
	}
	state := g.fortressStates.lookup(fortressStartKey{x: -14, z: -54})
	if state == nil {
		t.Fatal("fortress start state was not created")
	}
	state.mu.Lock()
	consumed := len(state.pieces) > 27 && state.pieces[27].ChestPlaced
	state.mu.Unlock()
	if !consumed {
		t.Fatal("corridor chest flag was not consumed when generateChest returned false")
	}
}

func encodeFortressState(t *testing.T, state savedFortressState) []byte {
	t.Helper()
	data, err := nbt.MarshalEncoding(state, nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFortressStateRejectsInvalidDataWithoutReplacingLoadedState(t *testing.T) {
	g := NewNether(1)
	g.fortressStates.forStart(fortressStartKey{x: -14, z: -54}, make([]bridgePiece, 56)).pieces[27].ChestPlaced = true
	valid, err := g.SaveGenerationState()
	if err != nil {
		t.Fatal(err)
	}
	start := savedFortressStartState{X: -14, Z: -54, Pieces: []savedFortressPieceState{{Index: 27, Chest: 1}}}
	base := savedFortressState{FormatVersion: fortressStateFormatVersion, DataVersion: fortressStateDataVersion, Seed: 1, Starts: []savedFortressStartState{start}}
	cases := map[string]savedFortressState{
		"format-version": func() savedFortressState { v := base; v.FormatVersion++; return v }(),
		"data-version":   func() savedFortressState { v := base; v.DataVersion++; return v }(),
		"seed":           func() savedFortressState { v := base; v.Seed++; return v }(),
		"invalid-bit": func() savedFortressState {
			v := base
			v.Starts = []savedFortressStartState{{X: -14, Z: -54, Pieces: []savedFortressPieceState{{Index: 27, Chest: 2}}}}
			return v
		}(),
		"both-bits": func() savedFortressState {
			v := base
			v.Starts = []savedFortressStartState{{X: -14, Z: -54, Pieces: []savedFortressPieceState{{Index: 27, Chest: 1, Spawner: 1}}}}
			return v
		}(),
		"negative-index": func() savedFortressState {
			v := base
			v.Starts = []savedFortressStartState{{X: -14, Z: -54, Pieces: []savedFortressPieceState{{Index: -1, Chest: 1}}}}
			return v
		}(),
		"duplicate-piece": func() savedFortressState {
			v := base
			v.Starts = []savedFortressStartState{{X: -14, Z: -54, Pieces: []savedFortressPieceState{{Index: 27, Chest: 1}, {Index: 27, Chest: 1}}}}
			return v
		}(),
		"duplicate-start": func() savedFortressState { v := base; v.Starts = []savedFortressStartState{start, start}; return v }(),
	}
	for name, invalid := range cases {
		t.Run(name, func(t *testing.T) {
			if err := g.LoadGenerationState(encodeFortressState(t, invalid)); err == nil {
				t.Fatal("invalid fortress state loaded without error")
			}
			after, err := g.SaveGenerationState()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(valid, after) {
				t.Fatal("rejected fortress state replaced previously loaded flags")
			}
		})
	}
}

func TestFortressStateValidatesLoadedIndexAgainstRebuiltGraph(t *testing.T) {
	g := NewNether(1)
	invalid := savedFortressState{
		FormatVersion: fortressStateFormatVersion, DataVersion: fortressStateDataVersion, Seed: 1,
		Starts: []savedFortressStartState{{X: -14, Z: -54, Pieces: []savedFortressPieceState{{Index: 1 << 30, Chest: 1}}}},
	}
	if err := g.LoadGenerationState(encodeFortressState(t, invalid)); err != nil {
		t.Fatal("piece index is checked only once its exact source graph exists", err)
	}
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		fortressRenderCall(g, world.ChunkPos{-14, -54}, false, func(int32, int32, int32) world.Block { return block.Air{} })
	}()
	if recovered == nil || !strings.Contains(fmt.Sprint(recovered), "outside source graph") {
		t.Fatalf("rebuilt fortress graph did not reject out-of-range loaded piece index: %v", recovered)
	}
	state := g.fortressStates.lookup(fortressStartKey{x: -14, z: -54})
	state.mu.Lock()
	if len(state.pieces) != 56 || len(state.pending) != 1 {
		state.mu.Unlock()
		t.Fatalf("invalid input allocated by untrusted index or was consumed: pieces=%d pending=%d", len(state.pieces), len(state.pending))
	}
	state.mu.Unlock()
}
