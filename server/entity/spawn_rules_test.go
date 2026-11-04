package entity

import (
	"bufio"
	"crypto/sha256"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// updateSpawnRules rewrites the golden file of TestSpawnRulesMatch to what checkSpawnRules decides now.
var updateSpawnRules = flag.Bool("update-spawn-rules", false, "rewrite testdata/spawn_rules.txt to what checkSpawnRules decides of each entity type now")

// spawnRulesGolden holds, for every entity type, how many of the checks of TestSpawnRulesMatch let it spawn and
// a hash of every result with the next number of its random after the check.
const spawnRulesGolden = "testdata/spawn_rules.txt"

// spawnRuleCell is a column of the test world of TestSpawnRulesMatch: what it is built of, its biome and the
// heights checked in it.
type spawnRuleCell struct {
	name  string
	biome string
	build func(tx *world.Tx, x int)
	ys    []int
}

// spawnRuleGround is the height of the ground of the cells: sea level is 63.
const spawnRuleGround = 64

// floor builds a column of b under the ground, with air above it.
func floor(b world.Block) func(tx *world.Tx, x int) {
	return func(tx *world.Tx, x int) {
		for y := spawnRuleGround - 3; y < spawnRuleGround; y++ {
			tx.SetBlock(cube.Pos{x, y, 0}, b, nil)
		}
	}
}

// pool builds water (or lava) from y lo to hi on a stone floor.
func pool(liquid world.Liquid, lo, hi int) func(tx *world.Tx, x int) {
	return func(tx *world.Tx, x int) {
		tx.SetBlock(cube.Pos{x, lo - 1, 0}, block.Stone{}, nil)
		for y := lo; y <= hi; y++ {
			tx.SetBlock(cube.Pos{x, y, 0}, liquid, nil)
		}
	}
}

var (
	still = block.Water{Still: true, Depth: 8}
	lava  = block.Lava{Still: true, Depth: 8}
)

// spawnRuleCells are the columns of the test world: the blocks Java's spawn predicates look at, in biomes whose
// tags they read, lit and dark, dry and wet.
var spawnRuleCells = []spawnRuleCell{
	{"grass", "plains", floor(block.Grass{}), []int{64, 65, 70}},
	{"grass forest", "forest", floor(block.Grass{}), []int{64}},
	{"grass jungle", "jungle", floor(block.Grass{}), []int{64}},
	{"grass meadow", "meadow", floor(block.Grass{}), []int{64}},
	{"grass mushroom", "mushroom_island", floor(block.Grass{}), []int{64}},
	{"mycelium", "mushroom_island", floor(block.Mycelium{}), []int{64}},
	{"sand", "desert", floor(block.Sand{}), []int{64}},
	{"sand beach", "beach", floor(block.Sand{}), []int{64, 66, 67}},
	{"red sand", "mesa", floor(block.Sand{Red: true}), []int{64}},
	{"terracotta", "mesa", floor(block.Terracotta{}), []int{64}},
	{"podzol", "mega_taiga", floor(block.Podzol{}), []int{64}},
	{"coarse dirt", "savanna", floor(block.Dirt{Coarse: true}), []int{64}},
	{"snow", "ice_plains", floor(block.Snow{}), []int{64}},
	{"ice", "ice_plains", floor(block.Ice{}), []int{64}},
	{"packed ice frozen ocean", "frozen_ocean", floor(block.PackedIce{}), []int{64}},
	{"stone", "plains", floor(block.Stone{}), []int{64}},
	{"stone peaks", "stony_peaks", floor(block.Stone{}), []int{64}},
	{"clay", "lush_caves", floor(block.Clay{}), []int{64}},
	{"mud", "mangrove_swamp", floor(block.Mud{}), []int{64}},
	{"moss", "lush_caves", floor(block.MossBlock{}), []int{64}},
	{"leaves", "jungle", floor(block.Leaves{Type: block.JungleLeaves(), Persistent: true}), []int{64}},
	{"nether wart", "hell", floor(block.NetherWartBlock{}), []int{64}},
	{"netherrack", "hell", floor(block.Netherrack{}), []int{64}},
	{"soul sand", "soulsand_valley", floor(block.SoulSand{}), []int{64}},
	{"glowstone", "plains", floor(block.Glowstone{}), []int{64}},
	{"cave", "plains", func(tx *world.Tx, x int) {
		floor(block.Stone{})(tx, x)
		for y := spawnRuleGround + 2; y < spawnRuleGround+5; y++ {
			tx.SetBlock(cube.Pos{x, y, 0}, block.Stone{}, nil)
		}
	}, []int{64}},
	{"powder snow", "grove", func(tx *world.Tx, x int) {
		floor(block.Snow{})(tx, x)
		tx.SetBlock(cube.Pos{x, spawnRuleGround + 1, 0}, block.PowderSnow{}, nil)
		tx.SetBlock(cube.Pos{x, spawnRuleGround + 2, 0}, block.PowderSnow{}, nil)
	}, []int{64}},
	{"ocean", "ocean", pool(still, 50, 62), []int{50, 55, 61, 62, 63}},
	{"warm ocean", "warm_ocean", pool(still, 50, 62), []int{52, 61, 62}},
	{"deep ocean", "deep_ocean", pool(still, 40, 62), []int{41, 62}},
	{"lukewarm ocean", "lukewarm_ocean", pool(still, 55, 62), []int{56, 62}},
	{"river", "river", pool(still, 58, 62), []int{59, 62}},
	{"swamp water", "swampland", pool(still, 60, 62), []int{61, 62}},
	{"dripstone water", "dripstone_caves", pool(still, 10, 30), []int{11, 20, 30}},
	{"deep water", "plains", pool(still, 5, 25), []int{6, 25}},
	{"lava", "hell", pool(lava, 60, 63), []int{60, 63, 64}},
	{"lava under wart", "crimson_forest", func(tx *world.Tx, x int) {
		pool(lava, 60, 62)(tx, x)
		tx.SetBlock(cube.Pos{x, 63, 0}, block.NetherWartBlock{}, nil)
	}, []int{62, 64}},
	{"slime depth", "swampland", func(tx *world.Tx, x int) {
		tx.SetBlock(cube.Pos{x, 20, 0}, block.Stone{}, nil)
	}, []int{21}},
	{"void", "the_end", func(*world.Tx, int) {}, []int{64}},
}

// spawnRuleCheck runs checkSpawnRules for an entity type at a position with a fresh random, and writes the
// result with the next number of the random after it.
func spawnRuleCheck(tx *world.Tx, id string, pos cube.Pos, seed int64) string {
	r := vanilla.NewLegacyRandom(seed)
	ok := checkSpawnRules(tx, id, pos, &r)
	return fmt.Sprintf("%t %d", ok, r.NextLong())
}

// spawnRuleIDs returns every identifier the checks of TestSpawnRulesMatch run for: the entity types of the
// default registry, the mobs of the spawn table, the mob categories, the types not spawned on peaceful, the
// keys of the table of spawn rules and a few unknown ones.
func spawnRuleDomain() []string {
	var ids []string
	for _, t := range DefaultRegistry.Types() {
		ids = append(ids, t.EncodeEntity())
	}
	ids = append(ids, spawnTableIDs()...)
	for id := range mobCategories {
		ids = append(ids, id)
	}
	for id := range notInPeaceful {
		ids = append(ids, id)
	}
	ids = append(ids, spawnRuleIDs()...)
	ids = append(ids, "", "minecraft:unknown", "cow", "minecraft:Cow")
	slices.Sort(ids)
	return slices.Compact(ids)
}

// spawnRuleResults runs every check of TestSpawnRulesMatch and returns a line for each identifier:
// "<id>" <checks that let it spawn>/<checks> <hash of every result and random>.
func spawnRuleResults(t *testing.T) []string {
	t.Helper()
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	defer func() { _ = w.Close() }()
	w.StopTime()

	type check struct {
		pos        cube.Pos
		difficulty world.Difficulty
		time       int
	}
	var checks []check
	<-w.Exec(func(tx *world.Tx) {
		for i, c := range spawnRuleCells {
			x := i * 4
			b, ok := world.BiomeByName(c.biome)
			if !ok {
				t.Fatalf("unknown biome %s", c.biome)
			}
			for y := 0; y < 80; y++ {
				tx.SetBiome(cube.Pos{x, y, 0}, b)
			}
			c.build(tx, x)
			for _, y := range c.ys {
				for _, d := range []world.Difficulty{world.DifficultyNormal, world.DifficultyPeaceful, world.DifficultyHard} {
					for _, time := range []int{6000, 18000} {
						checks = append(checks, check{cube.Pos{x, y, 0}, d, time})
					}
				}
			}
		}
	})

	var lines []string
	for _, id := range spawnRuleDomain() {
		h := sha256.New()
		spawns := 0
		for i, c := range checks {
			w.SetDifficulty(c.difficulty)
			w.SetTime(c.time)
			var res string
			<-w.Exec(func(tx *world.Tx) { res = spawnRuleCheck(tx, id, c.pos, int64(i)) })
			if strings.HasPrefix(res, "true") {
				spawns++
			}
			fmt.Fprintf(h, "%v %v %d %s\n", c.pos, c.difficulty, c.time, res)
		}
		lines = append(lines, fmt.Sprintf("%q %d/%d %x", id, spawns, len(checks), h.Sum(nil)[:12]))
	}
	return lines
}

// TestSpawnRulesMatch runs the spawn rules of every entity type over the cells of a test world, at three
// difficulties and two times of day, and requires the results, and what each check drew from its random, to be
// those of the golden file. The golden file was written by the switch that the table of spawn rules replaced.
func TestSpawnRulesMatch(t *testing.T) {
	current := spawnRuleResults(t)
	if *updateSpawnRules {
		var b strings.Builder
		b.WriteString("# What checkSpawnRules decides of each entity type: \"<id>\" <checks that let it spawn>/<checks> <hash of every result and random>.\n")
		b.WriteString("# The checks are those of spawn_rules_test.go: each cell of its test world, at three difficulties and two times of day.\n")
		b.WriteString("# Regenerate with: go test -run TestSpawnRulesMatch -update-spawn-rules ./server/entity\n\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		if err := os.WriteFile(spawnRulesGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(spawnRulesGolden)
	if err != nil {
		t.Fatalf("read the golden file: %v (generate it with -update-spawn-rules)", err)
	}
	defer f.Close()
	golden := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := sc.Text(); l != "" && !strings.HasPrefix(l, "#") {
			q, _ := strconv.QuotedPrefix(l)
			golden[q] = l
		}
	}
	for _, l := range current {
		q, _ := strconv.QuotedPrefix(l)
		switch want, ok := golden[q]; {
		case !ok:
			t.Errorf("%s is not in %s: %s", q, spawnRulesGolden, l)
		case want != l:
			t.Errorf("spawn rules of %s: %s, want %s", q, l, want)
		}
	}
}

// spawnRuleIDs returns the identifiers of the table of spawn rules, sorted.
func spawnRuleIDs() []string { return slices.Sorted(maps.Keys(spawnRules)) }
