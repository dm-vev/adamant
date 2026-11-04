package advanced

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// updateGenHash rewrites testdata/genhash.txt with the hashes generation gives
// now. Use it only for a deliberate change of generation, never to make a
// refactor pass, and say in the commit why generation changed:
//
//	go test ./server/world/generator/advanced -run TestGenerationIsUnchanged -update-genhash
//
// An update always generates the full set.
var updateGenHash = flag.Bool("update-genhash", false, "rewrite testdata/genhash.txt; only for a deliberate change of generation, with the reason in the commit")

// genHashFullEnv set to 1 makes TestGenerationIsUnchanged generate every
// centre. Without it only the first centre of each dimension is generated,
// since the full set takes about 85 s of CPU, most of it in the Overworld.
const genHashFullEnv = "ADAMANT_GENHASH_FULL"

const genHashFile = "testdata/genhash.txt"

// genHashSeed is the seed of every dimension in TestGenerationIsUnchanged.
const genHashSeed = 12345

type genHashDimension struct {
	name    string
	dim     world.Dimension
	gen     func() world.Generator
	centres [][2]int32
	radius  int32
}

func genHashDimensions() []genHashDimension {
	return []genHashDimension{
		{"overworld", world.Overworld, func() world.Generator { return NewOverworld(genHashSeed) },
			[][2]int32{{0, 0}, {100, 100}, {-151, 42}, {1000, -1000}, {-500, -2000}}, 3},
		{"nether", world.Nether, func() world.Generator { return NewNether(genHashSeed) },
			[][2]int32{{0, 0}, {40, -40}, {-200, 120}}, 2},
		{"end", world.End, func() world.Generator { return NewEnd(genHashSeed) },
			[][2]int32{{0, 0}, {60, 60}, {-100, 30}}, 2},
	}
}

// TestGenerationIsUnchanged generates the chunks around a few centres in every
// dimension and compares a SHA-256 hash of each centre's chunks with
// testdata/genhash.txt. The hash covers both block layers and the biome of
// every block, and the entities, block entities, scheduled ticks and
// post-processing entries the generator returns. A refactor of generation must
// keep every hash.
//
// By default only the first centre of each dimension is generated. Set
// ADAMANT_GENHASH_FULL=1 to generate all of them and check the totals.
func TestGenerationIsUnchanged(t *testing.T) {
	full := *updateGenHash || os.Getenv(genHashFullEnv) == "1"
	world.DefaultBlockRegistry.Finalize()

	want := map[string]string{}
	if !*updateGenHash {
		var err error
		if want, err = readGenHashes(genHashFile); err != nil {
			t.Fatalf("read %s: %v", genHashFile, err)
		}
	}

	var lines []string
	for _, d := range genHashDimensions() {
		centres := d.centres
		if !full {
			centres = centres[:1]
		}
		g := d.gen()
		total := sha256.New()
		for _, ctr := range centres {
			key := fmt.Sprintf("%s %d,%d", d.name, ctr[0], ctr[1])
			sum := hashGenerationCentre(g, d.dim, ctr, d.radius)
			total.Write(sum)
			got := hex.EncodeToString(sum)
			lines = append(lines, key+" "+got)
			if *updateGenHash {
				continue
			}
			if w, ok := want[key]; !ok {
				t.Errorf("%s: no hash in %s", key, genHashFile)
			} else if got != w {
				t.Errorf("%s dimension, centre (%d, %d), radius %d: generation changed: hash %s, want %s",
					d.name, ctr[0], ctr[1], d.radius, got, w)
			}
		}
		if !full {
			continue
		}
		key := d.name + " total"
		got := hex.EncodeToString(total.Sum(nil))
		lines = append(lines, key+" "+got)
		if w := want[key]; !*updateGenHash && got != w {
			t.Errorf("%s dimension: total %s, want %s", d.name, got, w)
		}
	}

	if *updateGenHash {
		if err := writeGenHashes(genHashFile, lines); err != nil {
			t.Fatalf("write %s: %v", genHashFile, err)
		}
		t.Logf("wrote %d hashes to %s", len(lines), genHashFile)
	}
}

// hashGenerationCentre generates the chunks within radius of centre in order
// of x, then z, and returns the hash of all of them.
func hashGenerationCentre(g world.Generator, dim world.Dimension, centre [2]int32, radius int32) []byte {
	r := dim.Range()
	h := sha256.New()
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			pos := world.ChunkPos{centre[0] + dx, centre[1] + dz}
			c := chunk.New(world.DefaultBlockRegistry, r)
			res := g.GenerateChunk(pos, c, world.GenerationContext{})
			hashGeneratedChunk(h, c, res, r)
		}
	}
	return h.Sum(nil)
}

func hashGeneratedChunk(h hash.Hash, c *chunk.Chunk, res world.GenerationResult, r cube.Range) {
	put := func(v uint64) {
		var b [8]byte
		binary.LittleEndian.PutUint64(b[:], v)
		h.Write(b[:])
	}
	for y := int16(r.Min()); y <= int16(r.Max()); y++ {
		for x := range uint8(16) {
			for z := range uint8(16) {
				put(uint64(c.Block(x, y, z, 0)))
				put(uint64(c.Block(x, y, z, 1)))
				put(uint64(c.Biome(x, y, z)))
			}
		}
	}
	put(uint64(len(res.Entities)))
	put(uint64(len(res.BlockEntities)))
	for _, be := range res.BlockEntities {
		_, _ = fmt.Fprintf(h, "%v%v", be.Pos, be.Data)
	}
	put(uint64(len(res.ScheduledTicks)))
	for _, tick := range res.ScheduledTicks {
		_, _ = fmt.Fprintf(h, "%v", tick)
	}
	for _, s := range res.PostProcessing {
		put(uint64(len(s)))
		for _, p := range s {
			put(uint64(p))
		}
	}
	if res.SpawnOriginalMobs {
		put(1)
	}
}

// readGenHashes reads lines of "<dimension> <x>,<z> <hash>" and
// "<dimension> total <hash>", keyed by everything before the hash. Lines
// starting with # are comments.
func readGenHashes(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m := map[string]string{}
	s := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; s.Scan(); n++ {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		if i < 0 {
			return nil, fmt.Errorf("line %d: %q has no hash", n, line)
		}
		m[line[:i]] = line[i+1:]
	}
	return m, s.Err()
}

func writeGenHashes(path string, lines []string) error {
	var b strings.Builder
	b.WriteString("# Hashes of generated chunks, checked by TestGenerationIsUnchanged.\n")
	b.WriteString("# Rewrite with -update-genhash only for a deliberate change of generation,\n")
	b.WriteString("# and say in the commit why generation changed.\n")
	for _, l := range lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
