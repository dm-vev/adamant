package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type monumentGraphFixture struct {
	Seed             string                 `json:"seed"`
	NextLong         string                 `json:"nextLong"`
	Box              [6]int32               `json:"box"`
	SourceRoom       int                    `json:"sourceRoom"`
	CoreRoom         int                    `json:"coreRoom"`
	Rooms            []monumentRoomFixture  `json:"rooms"`
	Children         []monumentChildFixture `json:"children"`
	Directions       map[string][][6]int32  `json:"directions"`
	MarkerDirections map[string][]*[3]int32 `json:"markerDirections"`
}

type monumentRoomFixture struct {
	Index   int                   `json:"index"`
	Claimed bool                  `json:"claimed"`
	Source  bool                  `json:"source"`
	Edges   []monumentEdgeFixture `json:"edges"`
}

type monumentEdgeFixture struct {
	Room    int  `json:"room"`
	Opening bool `json:"opening"`
}

type monumentChildFixture struct {
	Type   string `json:"type"`
	Design int    `json:"design"`
}

type monumentPostFixture struct {
	Direction int                       `json:"direction"`
	Profile   string                    `json:"profile"`
	Calls     []monumentPostCallFixture `json:"calls"`
}

type monumentPostCallFixture struct {
	Index    int    `json:"clip"`
	Hash     string `json:"hash"`
	Count    int    `json:"count"`
	NextLong string `json:"nextLong"`
}
type monumentFullPostFixture struct {
	Direction  int                           `json:"direction"`
	Profile    string                        `json:"profile"`
	ClipOrigin [2]int32                      `json:"clipOrigin"`
	Calls      []monumentFullPostCallFixture `json:"calls"`
}

type monumentFullPostCallFixture struct {
	Index         int                         `json:"index"`
	Hash          string                      `json:"hash"`
	Count         int                         `json:"count"`
	PostNextLong  string                      `json:"postNextLong"`
	LevelNextLong string                      `json:"levelNextLong"`
	Entities      []monumentFullEntityFixture `json:"entities"`
}

type monumentFullEntityFixture struct {
	ID    string     `json:"id"`
	Pos   [3]float64 `json:"pos"`
	Block [3]int32   `json:"block"`
	NBT   string     `json:"nbt"`
}

func TestOceanMonumentFullPostProcessMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_ocean_monument_full_post.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []monumentFullPostFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	orientations := map[int]int{2: 0, 5: 1, 3: 2, 4: 3}
	for _, fixture := range fixtures {
		t.Run(strconv.Itoa(fixture.Direction)+"/"+fixture.Profile, func(t *testing.T) {
			piece := modernOceanMonumentPiece{west: 19, north: 83, orientation: orientations[fixture.Direction], seed: 123456789}
			constructorRandom := java26.NewLegacyRandom(123456789)
			if err := buildModernMonumentRoomGraph(&piece, &constructorRandom); err != nil {
				t.Fatal(err)
			}
			piece.children = fitModernMonumentRooms(&piece, &constructorRandom)
			base, ok := monumentFixtureBlock("minecraft:" + fixture.Profile)
			if !ok {
				t.Fatalf("missing base block minecraft:%s", fixture.Profile)
			}
			states := make(map[[3]int32]string)
			postRandom := java26.NewLegacyRandom(2468013579)
			levelRandom := java26.NewLegacyRandom(1357924680)
			for _, call := range fixture.Calls {
				x0, z0 := fixture.ClipOrigin[0], fixture.ClipOrigin[1]
				clip := javastructure.Bounds{Min: [3]int32{x0, -64, z0}, Max: [3]int32{x0 + 15, 319, z0 + 15}}
				env := javastructure.ProcessorEnvironment{
					BaseBlock: func(x, y, z int32) world.Block {
						if name, ok := states[[3]int32{x, y, z}]; ok {
							block, found := monumentFixtureBlock(name)
							if !found {
								panic("unknown monument fixture block " + name)
							}
							return block
						}
						return base
					},
					Random: &postRandom, LevelRandom: &levelRandom,
				}
				placement, err := piece.Place(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				for _, block := range placement.Blocks {
					states[block.Pos] = block.State.Name
				}
				if got := monumentPostHash(states); got != call.Hash {
					t.Errorf("call%d final block hash=%s, Java=%s", call.Index, got, call.Hash)
				}
				if len(states) != call.Count {
					t.Errorf("call%d final block count=%d, Java=%d", call.Index, len(states), call.Count)
				}
				if got := strconv.FormatInt(postRandom.NextLong(), 10); got != call.PostNextLong {
					t.Errorf("call%d postProcess RNG continuation=%s, Java=%s", call.Index, got, call.PostNextLong)
				}
				if got := strconv.FormatInt(levelRandom.NextLong(), 10); got != call.LevelNextLong {
					t.Errorf("call%d level RNG continuation=%s, Java=%s", call.Index, got, call.LevelNextLong)
				}
				if len(placement.Entities) != len(call.Entities) {
					t.Errorf("call%d elder guardian count=%d, Java=%d", call.Index, len(placement.Entities), len(call.Entities))
					continue
				}
				for i, source := range call.Entities {
					got := placement.Entities[i]
					if got.NBT["id"] != source.ID || got.BlockPos != source.Block || got.Pos != source.Pos {
						t.Errorf("call%d entity%d placement=%s at %v/%v, Java=%s at %v/%v", call.Index, i, got.NBT["id"], got.BlockPos, got.Pos, source.ID, source.Block, source.Pos)
					}
					if !strings.Contains(source.NBT, "Health:80.0f") || !strings.Contains(source.NBT, "PersistenceRequired:1b") {
						t.Errorf("call%d source guardian NBT lacks finalized mob fields: %s", call.Index, source.NBT)
					}
					if got.NBT["Health"] != float32(80) || got.NBT["PersistenceRequired"] != byte(1) || !got.SpawnFinalized || got.SpawnReason != "structure" || got.EntityRandom == nil {
						t.Errorf("call%d entity%d was not prepared as a persistent finalized structure mob: %+v", call.Index, i, got)
					}
				}
			}
		})
	}
}

func TestOceanMonumentPostProcessMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_ocean_monument_post.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []monumentPostFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	orientations := map[int]int{2: 0, 5: 1, 3: 2, 4: 3}
	for _, fixture := range fixtures {
		t.Run(strconv.Itoa(fixture.Direction)+"/"+fixture.Profile, func(t *testing.T) {
			piece := modernOceanMonumentPiece{west: 19, north: 83, orientation: orientations[fixture.Direction], seed: 123456789}
			constructorRandom := java26.NewLegacyRandom(123456789)
			if err := buildModernMonumentRoomGraph(&piece, &constructorRandom); err != nil {
				t.Fatal(err)
			}
			piece.children = fitModernMonumentRooms(&piece, &constructorRandom)
			base, ok := monumentFixtureBlock("minecraft:" + fixture.Profile)
			if !ok {
				t.Fatalf("missing base block minecraft:%s", fixture.Profile)
			}
			states := make(map[[3]int32]string)
			x0, z0 := piece.west+16, piece.north+16
			for _, call := range fixture.Calls {
				minX, maxX := x0, x0+7
				if call.Index == 1 {
					minX, maxX = x0+8, x0+15
				}
				clip := javastructure.Bounds{Min: [3]int32{minX, 39, z0}, Max: [3]int32{maxX, 41, z0 + 15}}
				postRandom := java26.NewLegacyRandom(987654321 + int64(call.Index))
				env := javastructure.ProcessorEnvironment{
					BaseBlock: func(x, y, z int32) world.Block {
						if name, ok := states[[3]int32{x, y, z}]; ok {
							block, found := monumentFixtureBlock(name)
							if !found {
								panic("unknown monument fixture block " + name)
							}
							return block
						}
						return base
					},
					Random: &postRandom,
				}
				placement, err := piece.Place(clip, env)
				if err != nil {
					t.Fatal(err)
				}
				for _, block := range placement.Blocks {
					states[block.Pos] = block.State.Name
				}
				if got := monumentPostHash(states); got != call.Hash {
					t.Errorf("call%d final block hash=%s, Java=%s", call.Index, got, call.Hash)
				}
				if len(states) != call.Count {
					t.Errorf("call%d final block count=%d, Java=%d", call.Index, len(states), call.Count)
				}
				if got := strconv.FormatInt(postRandom.NextLong(), 10); got != call.NextLong {
					t.Errorf("call%d postProcess RNG continuation=%s, Java=%s", call.Index, got, call.NextLong)
				}
			}
		})
	}
}

func monumentFixtureBlock(name string) (world.Block, bool) {
	if block, ok := world.BlockByName(name, nil); ok {
		return block, true
	}
	properties := map[string]any(nil)
	if name == "minecraft:water" {
		properties = map[string]any{"level": int32(0)}
	}
	state, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: name, Properties: properties})
	if err != nil {
		return nil, false
	}
	runtimeID := state.Block
	if state.Liquid != world.DefaultBlockRegistry.AirRuntimeID() {
		runtimeID = state.Liquid
	}
	return world.BlockByRuntimeID(runtimeID)
}

func monumentPostHash(states map[[3]int32]string) string {
	rows := make([]string, 0, len(states))
	for pos, name := range states {
		rows = append(rows, strconv.FormatInt(int64(pos[0]), 10)+","+strconv.FormatInt(int64(pos[1]), 10)+","+strconv.FormatInt(int64(pos[2]), 10)+"="+name)
	}
	sort.Strings(rows)
	hash := sha256.Sum256([]byte(strings.Join(rows, "\n")))
	return hex.EncodeToString(hash[:])
}

func TestOceanMonumentGraphMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_ocean_monument_graph.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []monumentGraphFixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		seed, err := strconv.ParseInt(fixture.Seed, 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		t.Run(fixture.Seed, func(t *testing.T) {
			var sourcePiece modernOceanMonumentPiece
			random := java26.NewLegacyRandom(seed)
			if err := buildModernMonumentRoomGraph(&sourcePiece, &random); err != nil {
				t.Fatal(err)
			}
			children := fitModernMonumentRooms(&sourcePiece, &random)
			if sourcePiece.rooms[sourcePiece.core].index != fixture.CoreRoom || sourcePiece.rooms[2].index != fixture.SourceRoom {
				t.Fatalf("source/core room=%d/%d, Java=%d/%d", sourcePiece.rooms[2].index, sourcePiece.core, fixture.SourceRoom, fixture.CoreRoom)
			}
			gotRooms := make(map[int]monumentRoomFixture)
			for _, room := range sourcePiece.rooms {
				if room.index < 0 {
					continue
				}
				row := monumentRoomFixture{Index: room.index, Claimed: room.claimed, Source: room.source, Edges: make([]monumentEdgeFixture, len(room.connections))}
				for direction, next := range room.connections {
					row.Edges[direction] = monumentEdgeFixture{Room: next, Opening: room.hasOpening[direction]}
				}
				gotRooms[room.index] = row
			}
			if len(gotRooms) != len(fixture.Rooms) {
				t.Fatalf("graph rooms=%d, Java=%d", len(gotRooms), len(fixture.Rooms))
			}
			for _, want := range fixture.Rooms {
				if got, ok := gotRooms[want.Index]; !ok || !reflect.DeepEqual(got, want) {
					t.Errorf("room %d=%+v, Java=%+v", want.Index, got, want)
				}
			}
			if len(children) != len(fixture.Children) {
				t.Fatalf("children=%d, Java=%d", len(children), len(fixture.Children))
			}
			for i, child := range children {
				if got := monumentSourceChildType(child.kind); got != fixture.Children[i].Type {
					t.Errorf("child %d=%s, Java=%s", i, got, fixture.Children[i].Type)
				}
				if (child.kind == monumentChildSimple || child.kind == monumentChildWing) && child.design != fixture.Children[i].Design {
					t.Errorf("%s child %d design=%d, Java=%d", monumentSourceChildType(child.kind), i, child.design, fixture.Children[i].Design)
				}
			}
			box := (modernOceanMonumentPiece{west: 19, north: 83}).Bounds()
			gotBox := [6]int32{box.Min[0], box.Min[1], box.Min[2], box.Max[0], box.Max[1], box.Max[2]}
			if gotBox != fixture.Box {
				t.Errorf("building bounds=%v, Java=%v", gotBox, fixture.Box)
			}
			if got := random.NextLong(); strconv.FormatInt(got, 10) != fixture.NextLong {
				t.Errorf("constructor RNG continuation=%d, Java=%s", got, fixture.NextLong)
			}
			for direction, orientation := range map[string]int{"2": 0, "5": 1, "3": 2, "4": 3} {
				piece := modernOceanMonumentPiece{west: 19, north: 83, orientation: orientation, children: children, rooms: sourcePiece.rooms}
				got := make([][6]int32, len(children))
				for i, child := range children {
					got[i] = monumentChildBounds(&piece, child)
				}
				if want := fixture.Directions[direction]; !reflect.DeepEqual(got, want) {
					t.Errorf("direction %s child boxes differ from Java", direction)
					for i := range min(len(got), len(want)) {
						if got[i] != want[i] {
							t.Errorf("child %d box=%v, Java=%v", i, got[i], want[i])
						}
					}
				}
				gotMarkers := make([]*[3]int32, 0, 3)
				for _, child := range children {
					if pos := monumentChildMarkerPosition(&piece, child); pos != nil {
						gotMarkers = append(gotMarkers, pos)
					}
				}
				if want := fixture.MarkerDirections[direction]; !reflect.DeepEqual(gotMarkers, want) {
					t.Errorf("direction %s elder markers=%v, Java=%v", direction, gotMarkers, want)
				}
			}
		})
	}
}

func monumentSourceChildType(kind modernMonumentChildKind) string {
	switch kind {
	case monumentChildEntry:
		return "OceanMonumentEntryRoom"
	case monumentChildCore:
		return "OceanMonumentCoreRoom"
	case monumentChildDoubleX:
		return "OceanMonumentDoubleXRoom"
	case monumentChildDoubleXY:
		return "OceanMonumentDoubleXYRoom"
	case monumentChildDoubleY:
		return "OceanMonumentDoubleYRoom"
	case monumentChildDoubleYZ:
		return "OceanMonumentDoubleYZRoom"
	case monumentChildDoubleZ:
		return "OceanMonumentDoubleZRoom"
	case monumentChildSimpleTop:
		return "OceanMonumentSimpleTopRoom"
	case monumentChildSimple:
		return "OceanMonumentSimpleRoom"
	case monumentChildWing:
		return "OceanMonumentWingRoom"
	case monumentChildPenthouse:
		return "OceanMonumentPenthouse"
	default:
		return "unknown"
	}
}

func monumentChildMarkerPosition(piece *modernOceanMonumentPiece, child modernMonumentChild) *[3]int32 {
	var x, y, z int32
	switch child.kind {
	case monumentChildPenthouse:
		x, y, z = 6, 1, 6
	case monumentChildWing:
		switch child.design {
		case 0:
			x, y, z = 11, 2, 16
		case 1:
			x, y, z = 11, 5, 13
		default:
			return nil
		}
	default:
		return nil
	}
	local := modernMonumentChildToBuilding(piece, child, x, y, z)
	pos := piece.worldPos(local[0], local[1], local[2])
	return &pos
}

func monumentChildBounds(piece *modernOceanMonumentPiece, child modernMonumentChild) [6]int32 {
	width, height, depth := modernMonumentChildRoomDimensions(child.kind)
	widthBlocks, heightBlocks, depthBlocks := int32(width*8), int32(height*4), int32(depth*8)
	if child.kind == monumentChildWing {
		widthBlocks, heightBlocks, depthBlocks = 23, 8, 21
	} else if child.kind == monumentChildPenthouse {
		widthBlocks, heightBlocks, depthBlocks = 14, 5, 14
	}
	minX, minY, minZ := int32(1<<30), int32(1<<30), int32(1<<30)
	maxX, maxY, maxZ := int32(-1<<30), int32(-1<<30), int32(-1<<30)
	for _, x := range [2]int32{0, widthBlocks - 1} {
		for _, y := range [2]int32{0, heightBlocks - 1} {
			for _, z := range [2]int32{0, depthBlocks - 1} {
				local := modernMonumentChildToBuilding(piece, child, x, y, z)
				pos := piece.worldPos(local[0], local[1], local[2])
				minX, minY, minZ = min(minX, pos[0]), min(minY, pos[1]), min(minZ, pos[2])
				maxX, maxY, maxZ = max(maxX, pos[0]), max(maxY, pos[1]), max(maxZ, pos[2])
			}
		}
	}
	return [6]int32{minX, minY, minZ, maxX, maxY, maxZ}
}

