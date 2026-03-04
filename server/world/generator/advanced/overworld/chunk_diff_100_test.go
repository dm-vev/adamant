package overworld

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

const (
	chunkDiffSeed         = int64(0)
	chunkDiffSampleChunks = 100
	chunkDiffBaselineFile = "testdata/chunk_diff_100_seed0.json"
	chunkDiffUpdateEnv    = "OVERWORLD_UPDATE_DIFF_BASELINE"
)

type chunkDiffBaseline struct {
	Seed    int64             `json:"seed"`
	Chunks  int               `json:"chunks"`
	Digests []chunkDiffDigest `json:"digests"`
}

type chunkDiffDigest struct {
	X      int    `json:"x"`
	Z      int    `json:"z"`
	SHA256 string `json:"sha256"`
}

func TestGenerateChunkDiff100(t *testing.T) {
	digests := generateChunkDigests(chunkDiffSeed, chunkDiffPositions())

	if os.Getenv(chunkDiffUpdateEnv) == "1" {
		saveChunkDiffBaseline(t, chunkDiffBaseline{
			Seed:    chunkDiffSeed,
			Chunks:  len(digests),
			Digests: digests,
		})
		t.Skipf("updated %s", chunkDiffBaselineFile)
	}

	baseline := loadChunkDiffBaseline(t)
	if baseline.Seed != chunkDiffSeed {
		t.Fatalf("baseline seed=%d, expected seed=%d", baseline.Seed, chunkDiffSeed)
	}
	if baseline.Chunks != chunkDiffSampleChunks {
		t.Fatalf("baseline chunks=%d, expected chunks=%d", baseline.Chunks, chunkDiffSampleChunks)
	}
	if len(baseline.Digests) != len(digests) {
		t.Fatalf("baseline has %d digests, current has %d digests", len(baseline.Digests), len(digests))
	}

	var diffCount int
	var lines []string
	for i := range digests {
		want := baseline.Digests[i]
		got := digests[i]
		if want.X != got.X || want.Z != got.Z {
			t.Fatalf("baseline position mismatch at index %d: want=(%d,%d), got=(%d,%d)", i, want.X, want.Z, got.X, got.Z)
		}
		if want.SHA256 != got.SHA256 {
			diffCount++
			if len(lines) < 10 {
				lines = append(lines, fmt.Sprintf("chunk (%d,%d): want %s, got %s", got.X, got.Z, want.SHA256, got.SHA256))
			}
		}
	}
	if diffCount > 0 {
		t.Fatalf("%d/%d chunks differ from baseline:\n%s", diffCount, len(digests), strings.Join(lines, "\n"))
	}
}

func chunkDiffPositions() []world.ChunkPos {
	positions := make([]world.ChunkPos, 0, chunkDiffSampleChunks)
	for x := -5; x < 5; x++ {
		for z := -5; z < 5; z++ {
			positions = append(positions, world.ChunkPos{int32(x), int32(z)})
		}
	}
	return positions
}

func generateChunkDigests(seed int64, positions []world.ChunkPos) []chunkDiffDigest {
	g := NewOverworld(seed)
	digests := make([]chunkDiffDigest, 0, len(positions))
	for _, pos := range positions {
		c := chunk.New(g.airRID, cube.Range{0, 255})
		g.GenerateChunk(pos, c)
		digests = append(digests, chunkDiffDigest{
			X:      int(pos[0]),
			Z:      int(pos[1]),
			SHA256: hashChunk(c),
		})
	}
	return digests
}

func hashChunk(c *chunk.Chunk) string {
	h := sha256.New()
	var buf [4]byte
	r := c.Range()
	for y := int16(r.Min()); y <= int16(r.Max()); y++ {
		for z := uint8(0); z < 16; z++ {
			for x := uint8(0); x < 16; x++ {
				binary.LittleEndian.PutUint32(buf[:], c.Block(x, y, z, 0))
				_, _ = h.Write(buf[:])
			}
		}
	}
	for z := uint8(0); z < 16; z++ {
		for x := uint8(0); x < 16; x++ {
			binary.LittleEndian.PutUint32(buf[:], c.Biome(x, int16(r.Min()), z))
			_, _ = h.Write(buf[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func loadChunkDiffBaseline(t *testing.T) chunkDiffBaseline {
	t.Helper()
	content, err := os.ReadFile(chunkDiffBaselineFile)
	if err != nil {
		t.Fatalf("read baseline %s: %v (run with %s=1 to generate)", chunkDiffBaselineFile, err, chunkDiffUpdateEnv)
	}
	var baseline chunkDiffBaseline
	if err := json.Unmarshal(content, &baseline); err != nil {
		t.Fatalf("parse baseline %s: %v", chunkDiffBaselineFile, err)
	}
	return baseline
}

func saveChunkDiffBaseline(t *testing.T, baseline chunkDiffBaseline) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(chunkDiffBaselineFile), 0o755); err != nil {
		t.Fatalf("create baseline dir: %v", err)
	}
	content, err := json.MarshalIndent(baseline, "", "  ")
	if err != nil {
		t.Fatalf("marshal baseline: %v", err)
	}
	content = append(content, '\n')
	if err := os.WriteFile(chunkDiffBaselineFile, content, 0o644); err != nil {
		t.Fatalf("write baseline %s: %v", chunkDiffBaselineFile, err)
	}
}
