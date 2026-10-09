package nether

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestNetherMaterialJava263(t *testing.T) {
	rawData, err := os.ReadFile("testdata/java263_raw_terrain.json")
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Columns []struct {
			Seed     int64  `json:"seed"`
			X        int32  `json:"x"`
			Z        int32  `json:"z"`
			Material string `json:"material"`
		} `json:"columns"`
	}
	if err = json.Unmarshal(rawData, &raw); err != nil {
		t.Fatal(err)
	}
	type key struct {
		seed int64
		x, z int32
	}
	rawColumns := make(map[key]string, len(raw.Columns))
	for _, row := range raw.Columns {
		rawColumns[key{row.Seed, row.X, row.Z}] = row.Material
	}
	fixture, err := os.ReadFile("testdata/java263_nether_material.txt")
	if err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*Nether{}
	for _, line := range strings.Split(string(fixture), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 5 {
			t.Fatalf("invalid source row %q", line)
		}
		seed64, _ := strconv.ParseInt(fields[0], 10, 64)
		x64, _ := strconv.ParseInt(fields[1], 10, 32)
		z64, _ := strconv.ParseInt(fields[2], 10, 32)
		biome, _ := strconv.Atoi(fields[3])
		x, z := int32(x64), int32(z64)
		expected := fields[4]
		if len(expected) != 128 {
			t.Fatalf("source output length %d at seed=%d x=%d z=%d", len(expected), seed64, x, z)
		}
		g := generators[seed64]
		if g == nil {
			g = NewNether(seed64)
			generators[seed64] = g
		}
		c := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
		var scratch netherScratch
		g.prepareHeights(int(x>>4), int(z>>4), c, &scratch, nil)
		input, ok := rawColumns[key{seed64, x, z}]
		if !ok {
			t.Fatalf("missing raw terrain fixture seed=%d x=%d z=%d", seed64, x, z)
		}
		for y, symbol := range []byte(input) {
			if got := materialSymbol(c.Block(uint8(x&15), int16(y), uint8(z&15), 0), g); got != symbol {
				t.Fatalf("raw input differs at seed=%d (%d,%d,%d): got=%c source=%c", seed64, x, y, z, got, symbol)
			}
			if got := g.biomeIndexAt(x, int32(y), z); got != biome {
				t.Fatalf("biome mismatch at seed=%d (%d,%d,%d): got=%d source=%d", seed64, x, y, z, got, biome)
			}
		}
		g.applyJavaNetherMaterial(int(x>>4), int(z>>4), c)
		for y, symbol := range []byte(expected) {
			if got := materialSymbol(c.Block(uint8(x&15), int16(y), uint8(z&15), 0), g); got != symbol {
				t.Fatalf("material mismatch at seed=%d (%d,%d,%d): got=%c source=%c", seed64, x, y, z, got, symbol)
			}
		}
	}
}

func TestNetherMaterialJava263Chunks(t *testing.T) {
	fixture, err := os.ReadFile("testdata/java263_nether_material_chunks.txt")
	if err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*Nether{}
	for _, line := range strings.Split(string(fixture), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 6 {
			t.Fatalf("invalid source chunk row %q", line)
		}
		seed, _ := strconv.ParseInt(fields[0], 10, 64)
		chunkX, _ := strconv.Atoi(fields[1])
		chunkZ, _ := strconv.Atoi(fields[2])
		g := generators[seed]
		if g == nil {
			g = NewNether(seed)
			generators[seed] = g
		}
		c := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
		var scratch netherScratch
		g.prepareHeights(chunkX, chunkZ, c, &scratch, nil)
		rawHash := hashMaterialChunk(c, g)
		if rawHash != fields[3] {
			t.Fatalf("raw chunk hash mismatch seed=%d chunk=(%d,%d): got=%s source=%s", seed, chunkX, chunkZ, rawHash, fields[3])
		}
		g.applyJavaNetherMaterial(chunkX, chunkZ, c)
		materialHash := hashMaterialChunk(c, g)
		if materialHash != fields[4] {
			t.Fatalf("material chunk hash mismatch seed=%d chunk=(%d,%d): got=%s source=%s", seed, chunkX, chunkZ, materialHash, fields[4])
		}
		var expectedCounts [5]int
		for i, count := range strings.Split(fields[5], ",") {
			if i >= len(expectedCounts) {
				t.Fatalf("invalid biome counts %q", fields[5])
			}
			expectedCounts[i], _ = strconv.Atoi(count)
		}
		if biomeCounts(g, chunkX, chunkZ) != expectedCounts {
			t.Fatalf("biome counts mismatch seed=%d chunk=(%d,%d): got=%v source=%v", seed, chunkX, chunkZ, biomeCounts(g, chunkX, chunkZ), expectedCounts)
		}
	}
}

func hashMaterialChunk(c *chunk.Chunk, g *Nether) string {
	hasher := sha256.New()
	for y := int16(0); y < 128; y++ {
		for x := uint8(0); x < 16; x++ {
			for z := uint8(0); z < 16; z++ {
				hasher.Write([]byte{materialSymbol(c.Block(x, y, z, 0), g)})
			}
		}
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

func biomeCounts(g *Nether, chunkX, chunkZ int) [5]int {
	var counts [5]int
	for y := int32(0); y < 128; y++ {
		for x := int32(0); x < 16; x++ {
			for z := int32(0); z < 16; z++ {
				counts[g.biomeIndexAt(int32(chunkX*16)+x, y, int32(chunkZ*16)+z)]++
			}
		}
	}
	return counts
}

func materialSymbol(rid uint32, g *Nether) byte {
	switch rid {
	case g.airRID:
		return 'a'
	case g.lavaStillRID, g.lavaFlowRID:
		return 'l'
	case g.netherrackRID:
		return 'n'
	case g.bedrockRID:
		return 'b'
	case g.soulSandRID:
		return 's'
	case g.soulSoilRID:
		return 'q'
	case g.gravelRID:
		return 'g'
	case g.basaltRID:
		return 'B'
	case g.blackstoneRID:
		return 'k'
	case g.crimsonNyliumRID:
		return 'c'
	case g.warpedNyliumRID:
		return 'w'
	case g.netherWartBlockRID:
		return 'N'
	case g.warpedWartBlockRID:
		return 'W'
	default:
		return '?'
	}
}
