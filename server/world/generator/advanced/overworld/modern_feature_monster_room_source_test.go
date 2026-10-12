package overworld

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type monsterRoomSourceCase struct {
	Algorithm   string                   `json:"algorithm"`
	Seed        int64                    `json:"seed"`
	Holes       int                      `json:"holes"`
	Scene       int                      `json:"scene"`
	Origin      [3]int32                 `json:"origin"`
	Placed      bool                     `json:"placed"`
	NextLong    int64                    `json:"next_long"`
	StateCount  int                      `json:"state_count"`
	StateSHA256 string                   `json:"state_sha256"`
	Actors      []monsterRoomSourceActor `json:"actors"`
}
type monsterRoomSourceActor struct {
	Position  [3]int `json:"position"`
	Type      string `json:"type"`
	Entity    string `json:"entity"`
	LootTable string `json:"loot_table"`
	LootSeed  int64  `json:"loot_seed"`
}
type monsterRoomSourceFixture struct {
	Cases []monsterRoomSourceCase `json:"cases"`
}

type monsterRoomSourceTerrain struct {
	modernTerrainSource
	origin       featurePos
	xr, zr       int32
	holes, scene int
	minY, maxY   int32
	states       map[string]world.NativeState
}

func (s monsterRoomSourceTerrain) BaseState(x, y, z int32) world.NativeState {
	if y < s.minY || y > s.maxY {
		return s.states["void_air"]
	}
	dx, dz, dy := x-s.origin.x, z-s.origin.z, y-s.origin.y
	if dx < -5 || dx > 5 || dz < -5 || dz > 5 || dy < -2 || dy > 4 {
		return s.states["air"]
	}
	name := "stone"
	if dy >= 0 && dy <= 3 && dx >= -s.xr && dx <= s.xr && dz >= -s.zr && dz <= s.zr {
		name = "air"
	}
	for i := 0; i < s.holes; i++ {
		var hx, hz int32
		switch {
		case i < int(2*s.zr+1):
			hx, hz = -s.xr-1, -s.zr+int32(i)
		case i < int(2*(2*s.zr+1)):
			hx, hz = s.xr+1, -s.zr+int32(i)-2*s.zr-1
		case i < int(2*(2*s.zr+1)+2*s.xr-1):
			hx, hz = -s.xr+1+int32(i)-2*(2*s.zr+1), -s.zr-1
		default:
			hx, hz = -s.xr+1+int32(i)-(2*(2*s.zr+1)+2*s.xr-1), s.zr+1
		}
		if dx == hx && dz == hz && (dy == 0 || dy == 1) {
			name = "air"
		}
	}
	if s.scene == 1 && dx == 0 && dy == -1 && dz == 0 {
		name = "air"
	}
	if s.scene == 2 && dx == 0 && dy == 4 && dz == 0 {
		name = "air"
	}
	if s.scene == 3 && dx == 0 && dy == 0 && dz == 0 {
		name = "bedrock"
	}
	if s.scene == 4 && dx == 0 && dy == -1 && dz == 0 {
		name = "farmland"
	}
	if s.scene == 7 && dx == 0 && dy == 0 && dz == 0 {
		name = "spawner"
	}
	if s.scene == 8 && dx == 1 && dy == 0 && dz == 0 {
		name = "chest"
	}
	return s.states[name]
}
func monsterRoomSourceNativeState(t *testing.T, name string) world.NativeState {
	t.Helper()
	s, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:" + name})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func monsterRoomJavaStateString(t *testing.T, state world.NativeState) string {
	t.Helper()
	js, err := world.JavaState(world.DefaultBlockRegistry, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(js.Properties) == 0 {
		return js.Name + "{}"
	}
	keys := make([]string, 0, len(js.Properties))
	for k := range js.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, js.Properties[k]))
	}
	return js.Name + "{" + strings.Join(parts, ", ") + "}"
}
func monsterRoomSourceInt(value any) (int64, bool) {
	switch n := value.(type) {
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}
func TestMonsterRoomMatchesJava263Source(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	b, err := os.ReadFile("testdata/java263_monster_room_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture monsterRoomSourceFixture
	if err := json.Unmarshal(b, &fixture); err != nil {
		t.Fatal(err)
	}
	states := make(map[string]world.NativeState)
	for _, name := range []string{"air", "void_air", "stone", "bedrock", "farmland", "cave_air", "mossy_cobblestone", "cobblestone", "chest", "spawner"} {
		states[name] = monsterRoomSourceNativeState(t, name)
	}
	for _, tc := range fixture.Cases {
		name := fmt.Sprintf("%s/seed%d/holes%d/scene%d/origin%v", tc.Algorithm, tc.Seed, tc.Holes, tc.Scene, tc.Origin)
		t.Run(name, func(t *testing.T) {
			origin := featurePos{tc.Origin[0], tc.Origin[1], tc.Origin[2]}
			radius := java26.NewLegacyRandom(tc.Seed)
			if tc.Algorithm == "xor" {
				radius = java26.NewXoroshiroRandom(tc.Seed)
			}
			xr, zr := radius.NextInt(2)+2, radius.NextInt(2)+2
			source := monsterRoomSourceTerrain{modernTerrainSource: modernTerrainSource{}, origin: origin, xr: xr, zr: zr, holes: tc.Holes, scene: tc.Scene, minY: -64, maxY: 319, states: states}
			target := world.ChunkPos{origin.x >> 4, origin.z >> 4}
			c := chunk.New(world.DefaultBlockRegistry, cube.Range{-64, 319})
			for x := int32(target[0]) * 16; x < int32(target[0])*16+16; x++ {
				for z := int32(target[1]) * 16; z < int32(target[1])*16+16; z++ {
					for y := max(source.minY, origin.y-2); y <= min(source.maxY, origin.y+4); y++ {
						world.SetNativeState(c, uint8(x&15), int16(y), uint8(z&15), source.BaseState(x, y, z))
					}
				}
			}
			result := &world.GenerationResult{}
			if tc.Scene == 7 {
				result.BlockEntities = append(result.BlockEntities, chunk.BlockEntity{Pos: cube.Pos{int(origin.x), int(origin.y), int(origin.z)}, Data: map[string]any{
					"id": "minecraft:mob_spawner", "x": origin.x, "y": origin.y, "z": origin.z,
					"Delay": int16(20), "MinSpawnDelay": int16(200), "MaxSpawnDelay": int16(800), "SpawnCount": int16(4),
					"MaxNearbyEntities": int16(6), "RequiredPlayerRange": int16(16), "SpawnRange": int16(4),
					"SpawnData": map[string]any{"entity": map[string]any{"id": "minecraft:pig"}}, "SpawnPotentials": []any{},
				}})
			}
			if tc.Scene == 8 {
				p := featurePos{origin.x + 1, origin.y, origin.z}
				result.BlockEntities = append(result.BlockEntities, chunk.BlockEntity{Pos: cube.Pos{int(p.x), int(p.y), int(p.z)}, Data: map[string]any{
					"id": "minecraft:chest", "x": p.x, "y": p.y, "z": p.z,
					"LootTable": "minecraft:chests/simple_dungeon", "LootTableSeed": int64(777),
				}})
			}
			for i, actor := range result.BlockEntities {
				state, err := world.JavaState(c.BlockRegistry(), world.NativeStateAt(c, uint8(actor.Pos[0]&15), int16(actor.Pos[1]), uint8(actor.Pos[2]&15)))
				if err != nil {
					t.Fatal(err)
				}
				data, err := javastructure.JavaBlockEntityPayload(state, [3]int32{int32(actor.Pos[0]), int32(actor.Pos[1]), int32(actor.Pos[2])}, actor.Data)
				if err != nil {
					t.Fatal(err)
				}
				result.BlockEntities[i].Data = data
			}
			region := newFeatureRegion(&ModernFeatures{source: source}, c, target, result)
			region.SetOrigin(target[0], target[1])
			var random java26.Random
			if tc.Algorithm == "legacy" {
				random = java26.NewLegacyRandom(tc.Seed)
			} else {
				random = java26.NewXoroshiroRandom(tc.Seed)
			}
			if got := placeModernMonsterRoom(region, origin, &random); got != tc.Placed {
				t.Fatalf("placed=%v, Source=%v", got, tc.Placed)
			}
			if got := random.NextLong(); got != tc.NextLong {
				t.Errorf("nextLong=%d, Source=%d", got, tc.NextLong)
			}
			changes := make([]string, 0, len(region.overlay))
			for p, state := range region.overlay {
				if state == source.BaseState(p.x, p.y, p.z) {
					continue
				}
				changes = append(changes, fmt.Sprintf("%d,%d,%d=%s", p.x, p.y, p.z, monsterRoomJavaStateString(t, state)))
			}
			sort.Strings(changes)
			digest := sha256.Sum256([]byte(strings.Join(changes, ";")))
			if len(changes) != tc.StateCount || hex.EncodeToString(digest[:]) != tc.StateSHA256 {
				first := changes[:min(3, len(changes))]
				t.Errorf("final Source state projection count/hash=%d/%x; Source=%d/%s; first=%v", len(changes), digest, tc.StateCount, tc.StateSHA256, first)
			}
			wantActors := make(map[[3]int]monsterRoomSourceActor)
			for _, actor := range tc.Actors {
				if actor.Position[0]>>4 == int(origin.x>>4) && actor.Position[2]>>4 == int(origin.z>>4) {
					wantActors[actor.Position] = actor
				}
			}
			gotActors := make(map[[3]int]chunk.BlockEntity)
			for _, actor := range result.BlockEntities {
				gotActors[[3]int{actor.Pos[0], actor.Pos[1], actor.Pos[2]}] = actor
			}
			if len(gotActors) != len(wantActors) {
				t.Errorf("block actors=%v; Source=%v", gotActors, wantActors)
			}
			for pos, want := range wantActors {
				got, ok := gotActors[pos]
				if !ok {
					t.Errorf("missing %s actor at %v", want.Type, pos)
					continue
				}
				native, ok := world.BlockByRuntimeID(region.nativeState(featurePos{int32(pos[0]), int32(pos[1]), int32(pos[2])}).Block)
				if !ok {
					t.Fatalf("missing native actor block at %v", pos)
				}
				decoded := native.(world.NBTer).DecodeNBT(got.Data).(world.NBTer).EncodeNBT()
				if want.Type == "chest" {
					if decoded["id"] != "Chest" || decoded["LootTable"] != want.LootTable || decoded["LootTableSeed"] != want.LootSeed {
						t.Errorf("decoded chest actor at %v=%v; Source loot=%s seed=%d", pos, decoded, want.LootTable, want.LootSeed)
					}
				} else {
					if decoded["id"] != "MobSpawner" || decoded["EntityIdentifier"] != want.Entity {
						t.Errorf("decoded spawner actor at %v=%v; Source entity=%s", pos, decoded, want.Entity)
					}
					for key, expected := range map[string]int64{"Delay": 20, "MinSpawnDelay": 200, "MaxSpawnDelay": 800, "SpawnCount": 4, "MaxNearbyEntities": 6, "RequiredPlayerRange": 16, "SpawnRange": 4} {
						if actual, ok := monsterRoomSourceInt(decoded[key]); !ok || actual != expected {
							t.Errorf("decoded spawner %s=%v; Source=%d", key, decoded[key], expected)
						}
					}
				}
			}
		})
	}
}
