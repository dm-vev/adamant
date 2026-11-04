package loot

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
)

// updateLootTables rewrites the golden file of TestLootTablesMatch to what every loot table generates now.
var updateLootTables = flag.Bool("update-loot-tables", false, "rewrite testdata/loot_tables.txt to what every loot table generates now")

// lootTablesGolden holds, for every loot table, how many stacks the generations of TestLootTablesMatch made and
// a hash of the stacks and errors.
const lootTablesGolden = "testdata/loot_tables.txt"

// burningLootEntity is a loot entity on fire, as the furnace_smelt functions of mobs ask.
type burningLootEntity struct{ lootEntity }

func (burningLootEntity) OnFireDuration() time.Duration { return time.Second }

// lootMatchSeeds is the number of seeds every loot table generates with in each context.
const lootMatchSeeds = 8

// lootMatchContexts are the contexts every loot table generates in: none, a player kill with a pickaxe of
// Fortune III and Looting III, shears of Silk Touch in an explosion, fire damage in open water, and a burning
// cow killed by a player.
func lootMatchContexts() []Context {
	fortune := item.NewStack(item.Pickaxe{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.Fortune, 3))
	silk := item.NewStack(item.Shears{}, 1).WithEnchantments(item.NewEnchantment(enchantment.SilkTouch, 1))
	return []Context{
		{},
		{Tool: fortune.Item(), ToolStack: fortune, Looting: 3, PlayerKilled: true, Luck: 1, EnchantmentLevel: 30},
		{Tool: silk.Item(), ToolStack: silk, ExplosionRadius: 4, Luck: -1},
		{
			Entity: "minecraft:zombie", OpenWater: true, EntityProperties: map[string]any{"flags": map[string]any{"is_on_fire": true}},
			DamageTags: map[string]bool{"minecraft:is_fire": true, "minecraft:is_explosion": false}, PlayerKilled: true,
		},
		{ThisEntity: burningLootEntity{lootEntity{id: "minecraft:cow"}}, PlayerKilled: true, Looting: 1},
	}
}

// lootTableLine generates a loot table with lootMatchSeeds seeds in each of lootMatchContexts, and writes the table, the
// stacks made and a hash of their structure NBT and of the errors and panics.
func lootTableLine(id string) string {
	h := sha256.New()
	stacks := 0
	for ci, ctx := range lootMatchContexts() {
		for seed := int64(1); seed <= lootMatchSeeds; seed++ {
			fmt.Fprintf(h, "%d %d:", ci, seed)
			out, err := generateRecovered(id, seed, ctx)
			if err != nil {
				fmt.Fprintf(h, " error %v\n", err)
				continue
			}
			for _, s := range out {
				stacks++
				nbt, err := item.StackToStructureNBT(s)
				if err != nil {
					fmt.Fprintf(h, " stack %v error %v", s, err)
					continue
				}
				h.Write([]byte(" "))
				writeCanonical(h, reflect.ValueOf(nbt))
			}
			h.Write([]byte("\n"))
		}
	}
	return fmt.Sprintf("%q %d %x", id, stacks, h.Sum(nil)[:12])
}

// generateRecovered runs Generate, turning a panic into an error.
func generateRecovered(id string, seed int64, ctx Context) (out []item.Stack, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return Generate(id, seed, ctx)
}

// writeCanonical writes a value of structure NBT with the keys of its maps sorted.
func writeCanonical(w interface{ Write([]byte) (int, error) }, v reflect.Value) {
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Map:
		keys := v.MapKeys()
		slices.SortFunc(keys, func(a, b reflect.Value) int { return strings.Compare(fmt.Sprint(a), fmt.Sprint(b)) })
		fmt.Fprintf(w, "%s{", v.Type())
		for _, k := range keys {
			fmt.Fprintf(w, "%q:", fmt.Sprint(k))
			writeCanonical(w, v.MapIndex(k))
			fmt.Fprint(w, ",")
		}
		fmt.Fprint(w, "}")
	case reflect.Slice, reflect.Array:
		fmt.Fprintf(w, "%s[", v.Type())
		for i := range v.Len() {
			writeCanonical(w, v.Index(i))
			fmt.Fprint(w, ",")
		}
		fmt.Fprint(w, "]")
	case reflect.Invalid:
		fmt.Fprint(w, "nil")
	default:
		fmt.Fprintf(w, "%s(%v)", v.Type(), v)
	}
}

// TestLootTablesMatch generates every loot table with lootMatchSeeds seeds in each context of lootMatchContexts and
// requires the stacks, errors and panics to be those of the golden file, which the switch of applyModifiers
// wrote before its cases became the handlers of lootFunctions.
func TestLootTablesMatch(t *testing.T) {
	var current []string
	for _, id := range slices.Sorted(maps.Keys(tables)) {
		current = append(current, lootTableLine(id))
	}
	if *updateLootTables {
		var b strings.Builder
		b.WriteString("# What every loot table generates: \"<table>\" <stacks> <hash of the stacks and errors>.\n")
		b.WriteString("# Each table generates with 8 seeds in each context of loot_functions_match_test.go. Regenerate with:\n")
		b.WriteString("# go test -run TestLootTablesMatch -update-loot-tables ./server/loot\n\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		if err := os.WriteFile(lootTablesGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(lootTablesGolden)
	if err != nil {
		t.Fatalf("read the golden file: %v (generate it with -update-loot-tables)", err)
	}
	defer f.Close()
	want := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := sc.Text(); l != "" && !strings.HasPrefix(l, "#") {
			q, _ := strconv.QuotedPrefix(l)
			want[q] = l
		}
	}
	if len(want) != len(current) {
		t.Errorf("%d loot tables, the golden file has %d", len(current), len(want))
	}
	for _, l := range current {
		q, _ := strconv.QuotedPrefix(l)
		if w, ok := want[q]; !ok || w != l {
			t.Errorf("loot table %s: %s, want %s", q, l, w)
		}
	}
}

// lootFunctionTypes returns every loot function type of the loot tables, with the number of times it appears.
func lootFunctionTypes() map[string]int {
	types := map[string]int{}
	var mods func([]modifier)
	var entries func([]entry)
	mods = func(ms []modifier) {
		for _, m := range ms {
			types[m.Type]++
		}
	}
	entries = func(es []entry) {
		for _, e := range es {
			mods(e.Functions)
			mods(e.Modifier)
			entries(e.Children)
			entries(e.Entries)
			if e.Inline != nil {
				mods(e.Inline.Modifier)
				for _, p := range e.Inline.Pools {
					mods(p.Modifier)
					entries(p.Entries)
				}
			}
		}
	}
	for _, t := range tables {
		mods(t.Modifier)
		for _, p := range t.Pools {
			mods(p.Modifier)
			entries(p.Entries)
		}
	}
	return types
}

// TestLootFunctionTypes logs which loot function types the loot tables use, so that TestLootTablesMatch is seen
// to run each.
func TestLootFunctionTypes(t *testing.T) {
	types := lootFunctionTypes()
	for _, k := range slices.Sorted(maps.Keys(types)) {
		t.Logf("%s %d", k, types[k])
	}
}

// updateLootSamples rewrites the golden file of TestLootFunctionSamples to what the loot functions do now.
var updateLootSamples = flag.Bool("update-loot-samples", false, "rewrite testdata/loot_function_samples.txt to what every loot function of the loot tables does alone now")

// lootSamplesGolden holds, for every loot function of the loot tables with the item it is applied to, a hash of
// what applyModifiers makes of the item with it alone.
const lootSamplesGolden = "testdata/loot_function_samples.txt"

// lootSample is a loot function of a loot table with an item it is applied to there.
type lootSample struct {
	id  string
	mod modifier
	key string
}

// lootSamples returns every distinct loot function of the loot tables with each item it is applied to there:
// the item of its entry, or each item entry of the pool or table it is on. Entries that are no item give a stick.
func lootSamples() []lootSample {
	seen := map[string]bool{}
	var out []lootSample
	add := func(id string, ms []modifier) {
		for _, m := range ms {
			raw, err := json.Marshal(m.Raw)
			if err != nil {
				panic(err)
			}
			key := id + " " + string(raw)
			if !seen[key] {
				seen[key] = true
				out = append(out, lootSample{id: id, mod: m, key: key})
			}
		}
	}
	var items func([]entry) []string
	items = func(es []entry) (ids []string) {
		for _, e := range es {
			if e.Type == "minecraft:item" && e.Name != "" {
				ids = append(ids, normalize(e.Name))
			}
			ids = append(ids, items(e.Children)...)
			ids = append(ids, items(e.Entries)...)
		}
		return ids
	}
	var entries func([]entry)
	entries = func(es []entry) {
		for _, e := range es {
			id := "minecraft:stick"
			if e.Type == "minecraft:item" && e.Name != "" {
				id = normalize(e.Name)
			}
			add(id, e.Functions)
			add(id, e.Modifier)
			entries(e.Children)
			entries(e.Entries)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		t := tables[name]
		var all []string
		for _, p := range t.Pools {
			ids := items(p.Entries)
			all = append(all, ids...)
			for _, id := range ids {
				add(id, p.Modifier)
			}
			entries(p.Entries)
		}
		for _, id := range all {
			add(id, t.Modifier)
		}
	}
	slices.SortFunc(out, func(a, b lootSample) int { return strings.Compare(a.key, b.key) })
	return out
}

// lootSampleLine applies a loot function alone to its item, with a count of 1 and of 7, four seeds of its own
// and each of lootMatchContexts, and writes the item, the function type, a hash of the sample and a hash of the
// stacks and errors. The seeds follow from the sample, so that samples of the same function draw differently.
func lootSampleLine(s lootSample) string {
	k := sha256.Sum256([]byte(s.key))
	first := int64(binary.BigEndian.Uint64(k[:8]))
	h := sha256.New()
	for ci, ctx := range lootMatchContexts() {
		for seed := first; seed < first+4; seed++ {
			for _, count := range []int{1, 7} {
				r := vanilla.NewLegacyRandom(seed)
				ctx.Random = &r
				fmt.Fprintf(h, "%d %d %d:", ci, seed, count)
				stack, err := applyRecovered(s, count, ctx)
				if err != nil {
					fmt.Fprintf(h, " error %v\n", err)
					continue
				}
				nbt, err := item.StackToStructureNBT(stack)
				if err != nil {
					fmt.Fprintf(h, " stack %v error %v\n", stack, err)
					continue
				}
				writeCanonical(h, reflect.ValueOf(nbt))
				h.Write([]byte("\n"))
			}
		}
	}
	return fmt.Sprintf("%s %s %x %x", s.id, s.mod.Type, k[:6], h.Sum(nil)[:12])
}

// applyRecovered runs applyModifiers for a sample, turning a panic into an error.
func applyRecovered(s lootSample, count int, ctx Context) (stack item.Stack, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return applyModifiers(s.id, count, map[string]any{}, []modifier{s.mod}, &ctx)
}

// TestLootFunctionSamples applies every loot function of the loot tables alone to each item it is applied to
// there, and requires the stacks, errors and panics to be those of the golden file, which the switch of
// applyModifiers wrote before its cases became the handlers of lootFunctions. It sees what the tables hide,
// such as the names set on the exploration maps, which need a world to be made.
func TestLootFunctionSamples(t *testing.T) {
	var current []string
	for _, s := range lootSamples() {
		current = append(current, lootSampleLine(s))
	}
	if *updateLootSamples {
		var b strings.Builder
		b.WriteString("# What every loot function of the loot tables does alone to an item it is applied to there:\n")
		b.WriteString("# <item> <function> <hash of the sample> <hash of the stacks and errors> (loot_functions_match_test.go).\n")
		b.WriteString("# Regenerate with: go test -run TestLootFunctionSamples -update-loot-samples ./server/loot\n\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		if err := os.WriteFile(lootSamplesGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(lootSamplesGolden)
	if err != nil {
		t.Fatalf("read the golden file: %v (generate it with -update-loot-samples)", err)
	}
	defer f.Close()
	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := sc.Text(); l != "" && !strings.HasPrefix(l, "#") {
			want = append(want, l)
		}
	}
	if !slices.Equal(current, want) {
		for i := range max(len(current), len(want)) {
			switch {
			case i >= len(current):
				t.Errorf("sample gone: %s", want[i])
			case i >= len(want):
				t.Errorf("new sample: %s", current[i])
			case current[i] != want[i]:
				t.Errorf("sample %s, want %s", current[i], want[i])
			}
		}
	}
}
