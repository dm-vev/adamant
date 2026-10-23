package end

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type endCityPieceFixture struct {
	Name     string `json:"name"`
	X        int    `json:"x"`
	Y        int    `json:"y"`
	Z        int    `json:"z"`
	Rotation string `json:"rotation"`
	MinX     int    `json:"minX"`
	MinY     int    `json:"minY"`
	MinZ     int    `json:"minZ"`
	MaxX     int    `json:"maxX"`
	MaxY     int    `json:"maxY"`
	MaxZ     int    `json:"maxZ"`
}

type endCityFixture struct {
	Seed    int64                  `json:"seed"`
	StartX  int                    `json:"startX"`
	StartZ  int                    `json:"startZ"`
	Pieces  []endCityPieceFixture  `json:"pieces"`
	Heights []endCityHeightFixture `json:"heights"`
}

type endCityHeightFixture struct {
	X      int `json:"x"`
	Z      int `json:"z"`
	Height int `json:"height"`
}

func TestEndCityPieceLayoutMatchesReferenceShip(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_endcity_ship.json")
	if err != nil {
		t.Fatal(err)
	}
	var want endCityFixture
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	world.DefaultBlockRegistry.Finalize()
	g := NewEnd(want.Seed)
	chunks := map[[2]int]*chunk.Chunk{}
	for _, h := range want.Heights {
		chunkX, chunkZ := h.X>>4, h.Z>>4
		key := [2]int{chunkX, chunkZ}
		c := chunks[key]
		if c == nil {
			c = chunk.New(world.DefaultBlockRegistry, world.End.Range())
			var scratch endScratch
			g.generateTerrain(chunkX, chunkZ, c, &scratch)
			chunks[key] = c
		}
		gotHeight := int(c.HighestBlock(uint8(h.X&15), uint8(h.Z&15)))
		if gotHeight != h.Height {
			t.Errorf("Java height at (%d,%d)=%d, native=%d", h.X, h.Z, h.Height, gotHeight)
		}
	}

	got, err := g.endCityStructure(want.StartX, want.StartZ)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("source-accepted city was rejected")
	}
	if len(got.pieces) != len(want.Pieces) {
		t.Fatalf("piece count=%d, want Java source %d", len(got.pieces), len(want.Pieces))
	}
	var mismatches []string
	names := []string{"base_floor", "base_roof", "bridge_end", "bridge_gentle_stairs", "bridge_piece", "bridge_steep_stairs", "fat_tower_base", "fat_tower_middle", "fat_tower_top", "second_floor_1", "second_floor_2", "second_roof", "ship", "third_floor_1", "third_floor_2", "third_roof", "tower_base", "tower_floor", "tower_piece", "tower_top"}
	templates := make(map[*endCityTemplate]string, len(names))
	for _, name := range names {
		template, err := getEndCityTemplate(name)
		if err != nil {
			t.Fatal(err)
		}
		templates[template] = name
	}
	for i, p := range got.pieces {
		w := want.Pieces[i]
		name := templates[p.template]
		rotation := [...]string{"NONE", "CLOCKWISE_90", "CLOCKWISE_180", "COUNTERCLOCKWISE_90"}[p.rot]
		checks := []struct {
			field string
			got   any
			want  any
		}{
			{"name", name, w.Name}, {"x", p.pos.x, w.X}, {"y", p.pos.y, w.Y}, {"z", p.pos.z, w.Z}, {"rotation", rotation, w.Rotation},
			{"minX", p.bb.minX, w.MinX}, {"minY", p.bb.minY, w.MinY}, {"minZ", p.bb.minZ, w.MinZ},
			{"maxX", p.bb.maxX, w.MaxX}, {"maxY", p.bb.maxY, w.MaxY}, {"maxZ", p.bb.maxZ, w.MaxZ},
		}
		for _, check := range checks {
			if fmt.Sprint(check.got) != fmt.Sprint(check.want) {
				mismatches = append(mismatches, fmt.Sprintf("piece %d %s %s: got %v want %v", i, name, check.field, check.got, check.want))
			}
		}
	}
	if len(mismatches) != 0 {
		t.Fatalf("%d/%d piece fields differ from mapped Java 26.3: %v", len(mismatches), len(want.Pieces)*11, mismatches)
	}
}

type renderedEntityFixture struct {
	Type         string    `json:"type"`
	Position     []float64 `json:"position"`
	Rotation     []float64 `json:"rotation"`
	Facing       string    `json:"facing"`
	Item         string    `json:"item"`
	ItemCount    int       `json:"itemCount"`
	ItemRotation int       `json:"itemRotation"`
	Color        int       `json:"color"`
	Peek         float64   `json:"peek"`
	AttachFace   string    `json:"attachFace"`
}

type renderedCityFixture struct {
	Seed    int64 `json:"seed"`
	ChunkX  int   `json:"chunkX"`
	ChunkZ  int   `json:"chunkZ"`
	Before  []int `json:"before"`
	After   []int `json:"after"`
	Palette map[string]struct {
		ID         string            `json:"id"`
		Properties map[string]string `json:"properties"`
	} `json:"palette"`
	Actors map[string]struct {
		ID            string `json:"id"`
		LootTable     string `json:"LootTable"`
		LootTableSeed string `json:"LootTableSeed"`
	} `json:"actors"`
	Entities []renderedEntityFixture `json:"entities"`
}

func TestEndCityShipChunkRenderingMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_endcity_ship_render.json")
	if err != nil {
		t.Fatal(err)
	}
	var source renderedCityFixture
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	world.DefaultBlockRegistry.Finalize()
	g := NewEnd(source.Seed)
	target := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
	var scratch endScratch
	region := newEndFeatureRegion(g, source.ChunkX, source.ChunkZ, target, &scratch)
	var result world.GenerationResult
	beforeMismatches := compareCityFixtureStates(t, source.Before, source.Palette, source.ChunkX, source.ChunkZ, region)
	g.applyEndCityStructures(source.ChunkX, source.ChunkZ, region, true, &result)
	region.publishTarget(source.ChunkX, source.ChunkZ, target)
	afterMismatches := compareCityFixtureStates(t, source.After, source.Palette, source.ChunkX, source.ChunkZ, region)
	if beforeMismatches != 0 || afterMismatches != 0 {
		t.Fatalf("full source city chunk states differ: terrain=%d/65536, decorated=%d/65536", beforeMismatches, afterMismatches)
	}

	var actorMismatches []string
	for pos, want := range source.Actors {
		parts := strings.Split(pos, ",")
		x, _ := strconv.Atoi(parts[0])
		y, _ := strconv.Atoi(parts[1])
		z, _ := strconv.Atoi(parts[2])
		actor, ok := region.blockActors[cube.Pos{x, y, z}]
		if !ok {
			actorMismatches = append(actorMismatches, pos+": missing actor")
			continue
		}
		wantState := source.Palette[strconv.Itoa(source.After[y*256+(x&15)*16+(z&15)])]
		if actor.record.State.Name != wantState.ID {
			actorMismatches = append(actorMismatches, fmt.Sprintf("%s: block entity=%s want block=%s", pos, actor.record.State.Name, wantState.ID))
		}
		if want.LootTable != "" && actor.record.NBT["LootTable"] != want.LootTable {
			actorMismatches = append(actorMismatches, fmt.Sprintf("%s: loot table=%v want=%s", pos, actor.record.NBT["LootTable"], want.LootTable))
		}
		if want.LootTableSeed != "" {
			wantSeed, err := strconv.ParseInt(strings.TrimSuffix(want.LootTableSeed, "L"), 10, 64)
			if err != nil {
				t.Fatalf("%s: parse source loot seed: %v", pos, err)
			}
			if gotSeed, ok := actor.record.NBT["LootTableSeed"].(int64); !ok || gotSeed != wantSeed {
				actorMismatches = append(actorMismatches, fmt.Sprintf("%s: loot seed=%v want=%d", pos, actor.record.NBT["LootTableSeed"], wantSeed))
			}
		}
	}
	if len(region.blockActors) != len(source.Actors) {
		actorMismatches = append(actorMismatches, fmt.Sprintf("actor count=%d want=%d", len(region.blockActors), len(source.Actors)))
	}

	var shulkers int
	for _, handle := range result.Entities {
		sourceEntity := handle.WorldlessEntity(nil)
		if shulker, ok := sourceEntity.(*entity.Shulker); ok {
			shulkers++
			var want *renderedEntityFixture
			for i := range source.Entities {
				if source.Entities[i].Type == "minecraft:shulker" {
					want = &source.Entities[i]
					break
				}
			}
			if want == nil {
				actorMismatches = append(actorMismatches, "unexpected native Shulker event")
			} else {
				got := shulker.Position()
				wantColor := want.Color
				if wantColor == -1 {
					wantColor = 16
				}
				if got[0] != want.Position[0] || got[1] != want.Position[1] || got[2] != want.Position[2] ||
					int(shulker.Peek()) != int(want.Peek) || int(shulker.Color()) != wantColor {
					actorMismatches = append(actorMismatches, fmt.Sprintf("Shulker event differs: position=%v peek=%d color=%d want=%v/%v/%d", got, shulker.Peek(), shulker.Color(), want.Position, want.Peek, want.Color))
				}
			}
		}
		if err := handle.Close(); err != nil {
			t.Fatal(err)
		}
	}
	wantShulkers := 0
	for _, event := range source.Entities {
		if event.Type == "minecraft:shulker" {
			wantShulkers++
		}
	}
	if shulkers != wantShulkers {
		actorMismatches = append(actorMismatches, fmt.Sprintf("Shulker events=%d want=%d", shulkers, wantShulkers))
	}
	wantFrame := false
	for _, sourceEntity := range source.Entities {
		if sourceEntity.Type == "minecraft:item_frame" && sourceEntity.Item == "minecraft:elytra" && sourceEntity.ItemCount == 1 {
			wantFrame = true
		}
	}
	frameCount := 0
	for _, blockEntity := range result.BlockEntities {
		if blockEntity.Data["id"] != "ItemFrame" {
			continue
		}
		frameCount++
		item, ok := blockEntity.Data["Item"].(map[string]any)
		if !ok || item["Name"] != "minecraft:elytra" || fmt.Sprint(item["Count"]) != "1" ||
			fmt.Sprint(blockEntity.Data["ItemRotation"]) != "0" {
			actorMismatches = append(actorMismatches, fmt.Sprintf("item-frame block entity differs from source marker: %v", blockEntity.Data))
		}
	}
	if !wantFrame || frameCount != 1 {
		actorMismatches = append(actorMismatches, fmt.Sprintf("item-frame events=%d source marker=%t", frameCount, wantFrame))
	}

	if len(actorMismatches) != 0 {
		t.Fatalf("%d source loot/entity mismatches; block entities=%v generated entities=%d: %v", len(actorMismatches), result.BlockEntities, len(result.Entities), actorMismatches)
	}
}

func compareCityFixtureStates(t *testing.T, source []int, palette map[string]struct {
	ID         string            `json:"id"`
	Properties map[string]string `json:"properties"`
}, chunkX, chunkZ int, region *endFeatureRegion) int {
	t.Helper()
	if len(source) != 65536 {
		t.Fatalf("source block state count=%d, want 65536", len(source))
	}
	mismatches := 0
	for index, sourceID := range source {
		y := index / 256
		x := index / 16 % 16
		z := index % 16
		want, ok := palette[strconv.Itoa(sourceID)]
		if !ok {
			t.Fatalf("source palette missing state ID %d", sourceID)
		}
		state, err := world.CanonicalState(world.DefaultBlockRegistry, region.nativeState(chunkX*16+x, y, chunkZ*16+z))
		if err != nil {
			t.Fatalf("state at (%d,%d,%d): %v", chunkX*16+x, y, chunkZ*16+z, err)
		}
		properties := make(map[string]string, len(state.Properties))
		for key, value := range state.Properties {
			properties[key] = fmt.Sprint(value)
		}
		if len(properties) == 0 {
			properties = nil
		}
		if state.Name != want.ID || !reflect.DeepEqual(properties, want.Properties) {
			if mismatches < 3 {
				t.Logf("state at (%d,%d,%d) got=%q %#v want=%q %#v native=%#v", chunkX*16+x, y, chunkZ*16+z, state.Name, properties, want.ID, want.Properties, region.nativeState(chunkX*16+x, y, chunkZ*16+z))
			}
			mismatches++
		}
	}
	return mismatches
}
