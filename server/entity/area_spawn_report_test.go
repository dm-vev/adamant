package entity_test

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/overworld"
	"github.com/go-gl/mathgl/mgl64"
)

// This file is a probe of how many entities a fresh area of a generated world
// holds and why, for comparing with Java's. It runs only when
// ADAMANT_AREA_REPORT is set:
//
//	ADAMANT_AREA_REPORT=1 ADAMANT_AREA_SEED=42 ADAMANT_AREA_RADIUS=10 \
//	ADAMANT_AREA_TICKS=2400 ADAMANT_AREA_OUT=/home/agent/work/area.txt \
//	  go test ./server/entity -run TestAreaSpawnReport -v
//
// The area is the square of 2*RADIUS+1 chunks around chunk (CX, CZ), (0, 0) by
// default. Its chunks are generated, which spawns the creatures of their
// biomes, and are counted by type, with the item an item entity holds, and by
// spawn reason; with TICKS above zero a survival player is then put on the
// surface at the middle of the area and the world runs that many ticks, with
// the natural spawning of every tick, and is counted again every 600; with
// ADAMANT_AREA_PLAYER=0 no player is put there, as the Java server that
// forceloads an area has none. OUT, if
// set, receives a line "chunkX chunkZ entity-type spawn-reason" for every
// entity after generation.

func areaEnv(key string, def int64) int64 {
	if v, err := strconv.ParseInt(os.Getenv(key), 10, 64); err == nil {
		return v
	}
	return def
}

// areaCounts counts the entities within the chunks of the square of radius
// around centre by type and by spawn reason.
func areaCounts(tx *world.Tx, centre world.ChunkPos, radius int32, out *bufio.Writer) (byType, byReason map[string]int, total int) {
	byType, byReason = map[string]int{}, map[string]int{}
	for e := range tx.Entities() {
		pos := e.Position()
		cx, cz := int32(math.Floor(pos[0]/16)), int32(math.Floor(pos[2]/16))
		if cx < centre[0]-radius || cx > centre[0]+radius || cz < centre[1]-radius || cz > centre[1]+radius {
			continue
		}
		name := e.H().Type().EncodeEntity()
		if ent, ok := e.(*entity.Ent); ok {
			if drop, ok := ent.Behaviour().(*entity.ItemBehaviour); ok {
				id, _ := drop.Item().Item().EncodeItem()
				name += "[" + id + "]"
			}
		}
		reason := e.H().SpawnReason()
		if reason == "" {
			reason = "(none)"
		}
		byType[name]++
		byReason[reason]++
		total++
		if out != nil {
			_, _ = fmt.Fprintf(out, "%d %d %s %s\n", cx, cz, name, reason)
		}
	}
	return
}

func sortedCounts(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return m[keys[i]] > m[keys[j]] || m[keys[i]] == m[keys[j]] && keys[i] < keys[j] })
	lines := make([]string, len(keys))
	for i, k := range keys {
		lines[i] = fmt.Sprintf("%5d %s", m[k], k)
	}
	return lines
}

func TestAreaSpawnReport(t *testing.T) {
	if os.Getenv("ADAMANT_AREA_REPORT") == "" {
		t.Skip("set ADAMANT_AREA_REPORT=1 to measure the entities of a fresh area")
	}
	seed, radius := areaEnv("ADAMANT_AREA_SEED", 42), int32(areaEnv("ADAMANT_AREA_RADIUS", 10))
	ticks := int(areaEnv("ADAMANT_AREA_TICKS", 0))
	centre := world.ChunkPos{int32(areaEnv("ADAMANT_AREA_CX", 0)), int32(areaEnv("ADAMANT_AREA_CZ", 0))}
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{Entities: entity.DefaultRegistry, Generator: overworld.NewOverworld(seed), Seed: seed, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(world.DifficultyNormal)

	var out *bufio.Writer
	if path := os.Getenv("ADAMANT_AREA_OUT"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		out = bufio.NewWriter(f)
		defer out.Flush()
	}

	// One ring more than the area is generated, so that the area is
	// complete: the chunks around it are what its decoration reads.
	<-w.Exec(func(tx *world.Tx) {
		for cx := centre[0] - radius - 1; cx <= centre[0]+radius+1; cx++ {
			for cz := centre[1] - radius - 1; cz <= centre[1]+radius+1; cz++ {
				tx.Block(cube.Pos{int(cx) * 16, 0, int(cz) * 16})
			}
		}
		byType, byReason, total := areaCounts(tx, centre, radius, out)
		chunks := int((2*radius + 1) * (2*radius + 1))
		t.Logf("after generation: %d entities in %d chunks (%.3f per chunk)", total, chunks, float64(total)/float64(chunks))
		t.Logf("by type:\n%s", joinLines(sortedCounts(byType)))
		t.Logf("by reason:\n%s", joinLines(sortedCounts(byReason)))
	})
	if ticks == 0 {
		return
	}
	if areaEnv("ADAMANT_AREA_PLAYER", 1) != 0 {
		<-w.Exec(func(tx *world.Tx) {
			x, z := int(centre[0])*16+8, int(centre[1])*16+8
			y := tx.HighestBlock(x, z) + 1
			addSurvivalPlayer(tx, mgl64.Vec3{float64(x) + .5, float64(y), float64(z) + .5})
		})
	}
	for tick := 1; tick <= ticks; tick++ {
		w.AdvanceTick()
		if tick%600 == 0 || tick == ticks {
			<-w.Exec(func(tx *world.Tx) {
				byType, byReason, total := areaCounts(tx, centre, radius, nil)
				t.Logf("tick %d: %d entities", tick, total)
				t.Logf("by type:\n%s", joinLines(sortedCounts(byType)))
				t.Logf("by reason:\n%s", joinLines(sortedCounts(byReason)))
			})
		}
	}
}

func joinLines(lines []string) string {
	s := ""
	for _, l := range lines {
		s += "  " + l + "\n"
	}
	return s
}
