package bedrockdata_test

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/internal/testfixture"
	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	"github.com/dm-vev/adamant/net/nbt"
)

// The oracles of testdata/vNNNN are what BDS of each protocol sends, taken
// by scripts/bedrock-data.sh, and the block palette of each, from Geyser.
// The binary embeds none of them: tests hold the data of vNNNN, and later
// what the session sends, to them.
var (
	oraclesFrom  = flag.String("oracles-from", "", "write testdata/vNNNN/{creative,crafting}.json from the dumps scripts/bedrock-data.sh wrote to DIR/vNNNN")
	palettesFrom = flag.String("palettes-from", "", "write testdata/vNNNN/palette.nbt from Geyser's block_palette.26_50.nbt and .26_60.nbt in DIR")
)

// geyserPalettes names the palette files of Geyser by protocol.
var geyserPalettes = map[int32]string{2193: "block_palette.26_50.nbt", 2225: "block_palette.26_60.nbt"}

// TestWriteOracles writes the oracles from the dumps and palettes the
// flags name, through testfixture; it does nothing without them.
func TestWriteOracles(t *testing.T) {
	if *oraclesFrom == "" && *palettesFrom == "" {
		t.Skip("neither -oracles-from nor -palettes-from is set")
	}
	for _, p := range bedrockdata.Protocols() {
		dir := filepath.Join("testdata", fmt.Sprintf("v%d", p))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if *oraclesFrom != "" {
			for _, name := range []string{"creative.json", "crafting.json"} {
				b, err := os.ReadFile(filepath.Join(*oraclesFrom, fmt.Sprintf("v%d", p), name))
				if err != nil {
					t.Fatal(err)
				}
				if err := testfixture.Write(filepath.Join(dir, name), b); err != nil {
					t.Fatal(err)
				}
			}
		}
		if *palettesFrom != "" {
			f, err := os.Open(filepath.Join(*palettesFrom, geyserPalettes[p]))
			if err != nil {
				t.Fatal(err)
			}
			r, err := gzip.NewReader(f)
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(r)
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err := testfixture.Write(filepath.Join(dir, "palette.nbt"), b); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// state is a block state as a palette holds it: a name and its properties,
// as one string that compares.
type state string

func stateOf(name string, props map[string]any) state {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		fmt.Fprintf(&b, " %s=%v", k, props[k])
	}
	return state(b.String())
}

func (s state) name() string { n, _, _ := strings.Cut(string(s), " "); return n }

// palette reads the oracle palette of a protocol: Geyser's, big-endian NBT
// of the blocks of its order.
func palette(t *testing.T, p int32) []state {
	t.Helper()
	b, err := testfixture.Read(filepath.Join("testdata", fmt.Sprintf("v%d", p), "palette.nbt"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := nbt.UnmarshalEncoding(b, &root, nbt.BigEndian); err != nil {
		t.Fatal(err)
	}
	blocks, _ := root["blocks"].([]any)
	out := make([]state, len(blocks))
	for i, s := range blocks {
		block, _ := s.(map[string]any)
		name, _ := block["name"].(string)
		states, _ := block["states"].(map[string]any)
		out[i] = stateOf(name, states)
	}
	return out
}

// gameStates reads the block states of the game, server/world's
// block_states.nbt.
func gameStates(t *testing.T) []state {
	t.Helper()
	b, err := os.ReadFile("../../../../server/world/block_states.nbt")
	if err != nil {
		t.Fatal(err)
	}
	dec := nbt.NewDecoder(bytes.NewReader(b))
	var out []state
	for {
		var s struct {
			Name    string         `nbt:"name"`
			States  map[string]any `nbt:"states"`
			Version int32          `nbt:"version"`
		}
		if err := dec.Decode(&s); err != nil {
			break
		}
		out = append(out, stateOf(s.Name, s.States))
	}
	return out
}

// TestGameBlocksAre2193 holds the block states of the game to those of
// 1.26.50, the palette of protocol 2193, but for the chalkboard of
// Education Edition, of 4 states in the game and 16 in BDS, which the game
// never places.
func TestGameBlocksAre2193(t *testing.T) {
	game, want := gameStates(t), palette(t, 2193)
	if len(game) < 22000 || len(want) < 22000 {
		t.Fatalf("%d states of the game, %d of 2193", len(game), len(want))
	}
	in := func(l []state) map[state]bool {
		m := make(map[state]bool, len(l))
		for _, s := range l {
			m[s] = true
		}
		return m
	}
	g, w := in(game), in(want)
	for s := range w {
		if !g[s] && s.name() != "minecraft:chalkboard" {
			t.Errorf("the game has no state %s of 2193", s)
		}
	}
	for s := range g {
		if !w[s] && s.name() != "minecraft:chalkboard" {
			t.Errorf("2193 has no state %s of the game", s)
		}
	}
}

// upgradeSchema is the part of a schema of pmmp's BedrockBlockUpgradeSchema
// blocks.json holds: properties added with their values.
type upgradeSchema struct {
	AddedProperties map[string]map[string]map[string]any `json:"addedProperties"`
}

// TestSchema0361TurnsStatesOf2193Into2225 applies v2225/blocks.json, pmmp's
// 0361_1.26.50_to_1.26.60, to every state of 2193's palette: each becomes
// a state of 2225's. What 2225 has past them are the connection states of
// redstone wire, fire and chorus plants and the three blocks it adds.
func TestSchema0361TurnsStatesOf2193Into2225(t *testing.T) {
	b, err := os.ReadFile("v2225/blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema upgradeSchema
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(func(yield func(string) bool) {
		for k := range schema.AddedProperties {
			if !yield(k) {
				return
			}
		}
	}); strings.Join(got, " ") != "minecraft:chorus_plant minecraft:fire minecraft:redstone_wire" {
		t.Fatalf("the schema adds properties to %v", got)
	}
	newer := map[state]bool{}
	for _, s := range palette(t, 2225) {
		newer[s] = true
	}
	reached := map[state]bool{}
	for _, s := range palette(t, 2193) {
		up := upgrade(t, s, schema)
		if !newer[up] {
			t.Errorf("%s of 2193 is %s, no state of 2225", s, up)
		}
		reached[up] = true
	}
	added := map[string]int{}
	for s := range newer {
		if !reached[s] {
			added[s.name()]++
		}
	}
	want := map[string]int{
		"minecraft:redstone_wire": 1296 - 16, "minecraft:fire": 512 - 16, "minecraft:chorus_plant": 64 - 1,
		"minecraft:disc_press": 4, "minecraft:ice_crystal": 6, "minecraft:icicle": 20,
	}
	if fmt.Sprint(added) != fmt.Sprint(want) {
		t.Errorf("2225 adds the states %v, want %v", added, want)
	}
}

// upgrade applies the added properties of a schema to a state of a palette.
func upgrade(t *testing.T, s state, schema upgradeSchema) state {
	t.Helper()
	add, ok := schema.AddedProperties[s.name()]
	if !ok {
		return s
	}
	extra := make([]string, 0, len(add))
	for k, v := range add {
		var val any
		for kind, x := range v {
			switch kind {
			case "byte":
				val = uint8(x.(float64))
			case "string":
				val = x.(string)
			default:
				t.Fatalf("a value of kind %s", kind)
			}
		}
		extra = append(extra, fmt.Sprintf("%s=%v", k, val))
	}
	parts := slices.Concat(strings.Fields(string(s))[1:], extra)
	sort.Strings(parts)
	return state(strings.Join(append([]string{s.name()}, parts...), " "))
}

// TestProtocolsOfBDS checks the data of 2225 against that of 2193: the 4
// items 1.26.60 adds, the 82 whose runtime IDs it moves up by one, the ice
// ball BDS lists among its entities, the same biomes and block
// definitions.
func TestProtocolsOfBDS(t *testing.T) {
	if got := bedrockdata.Protocols(); fmt.Sprint(got) != "[2193 2225]" {
		t.Fatalf("protocols %v", got)
	}
	a, _ := bedrockdata.VanillaOf(2193)
	b, ok := bedrockdata.VanillaOf(2225)
	if !ok || len(b.Items()) != 2080 || len(b.Blocks()) != 98 {
		t.Fatalf("2225: %v, %d items, %d blocks", ok, len(b.Items()), len(b.Blocks()))
	}
	var added []string
	shifted := 0
	for name, it := range b.Items() {
		old, ok := a.Items()[name]
		switch {
		case !ok:
			added = append(added, name)
		case old.RuntimeID != it.RuntimeID:
			if it.RuntimeID != old.RuntimeID+1 || old.RuntimeID < 791 {
				t.Errorf("%s: %d in 2193, %d in 2225", name, old.RuntimeID, it.RuntimeID)
			}
			shifted++
		}
	}
	slices.Sort(added)
	if strings.Join(added, " ") != "minecraft:disc_press minecraft:frostbite_spawn_egg minecraft:ice_crystal minecraft:icicle" || shifted != 82 {
		t.Errorf("2225 adds %v and moves %d items", added, shifted)
	}
	if fmt.Sprint(b.Blocks()) != fmt.Sprint(a.Blocks()) {
		t.Error("the block definitions of 2225 differ from 2193's")
	}
	if !slices.Equal(a.Biomes, b.Biomes) || len(a.Biomes) != 89 {
		t.Errorf("biomes: %d of 2193, %d of 2225", len(a.Biomes), len(b.Biomes))
	}
	extra := slices.DeleteFunc(slices.Clone(b.Entities), func(e string) bool { return slices.Contains(a.Entities, e) })
	if len(a.Entities) != 137 || fmt.Sprint(extra) != "[minecraft:ice_ball]" {
		t.Errorf("%d entities of 2193; 2225 adds %v", len(a.Entities), extra)
	}
}

// auxName matches an input of a recipe of 1.26.61 that names its item with
// its aux value too, "1 minecraft:acacia_planks:0:0", which 1.26.52 names
// "1 minecraft:acacia_planks:0".
var auxName = regexp.MustCompile(`^(\d+ \S+):(\d+):(\d+)$`)

// TestRecipesAndCreativeOfBDS compares what BDS of each protocol sends
// beside its item registry: the same creative inventory by name, and the
// same recipes but for two arrows tipped with potions 1.26.60 adds, once
// the names of their inputs are alike: BDS 1.26.61 names the item of a
// default descriptor with its aux value.
func TestRecipesAndCreativeOfBDS(t *testing.T) {
	read := func(p int32, name string) []byte {
		b, err := testfixture.Read(filepath.Join("testdata", fmt.Sprintf("v%d", p), name))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if !bytes.Equal(read(2193, "creative.json"), read(2225, "creative.json")) {
		t.Error("the creative inventories differ")
	}
	type recipe struct {
		Kind, ID, Block string
		Input, Output   []string
	}
	recipes := func(p int32) map[string]bool {
		var l []recipe
		if err := json.Unmarshal(read(p, "crafting.json"), &l); err != nil {
			t.Fatal(err)
		}
		out := make(map[string]bool, len(l))
		for _, r := range l {
			for i, in := range r.Input {
				if m := auxName.FindStringSubmatch(in); m != nil && m[2] == m[3] {
					r.Input[i] = m[1] + ":" + m[3]
				}
			}
			out[fmt.Sprint(r)] = true
		}
		return out
	}
	a, b := recipes(2193), recipes(2225)
	var added []string
	for r := range b {
		if !a[r] {
			added = append(added, r)
		}
	}
	for r := range a {
		if !b[r] {
			t.Errorf("2225 lacks %s", r)
		}
	}
	slices.Sort(added)
	if len(a) < 4900 || len(added) != 2 || !strings.Contains(added[0], "weapon_arrow_recipe_47") || !strings.Contains(added[1], "weapon_arrow_recipe_48") {
		t.Errorf("%d recipes of 2193; 2225 adds %d: %.300v", len(a), len(added), added)
	}
}

// parseState returns the name and properties of a state of a palette, as
// stateOf wrote them: the values of properties as strings.
func parseState(s state) (string, map[string]string) {
	parts := strings.Fields(string(s))
	props := make(map[string]string, len(parts)-1)
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(p, "=")
		props[k] = v
	}
	return parts[0], props
}

// typed returns the properties of a state of the game, of the types the
// game holds them in, from those of a palette.
func typed(t *testing.T, props map[string]string, like map[string]any) map[string]any {
	t.Helper()
	out := make(map[string]any, len(props))
	for k, v := range props {
		out[k] = v
		if _, ok := like[k].(uint8); ok {
			var n uint8
			fmt.Sscan(v, &n)
			out[k] = n
		}
	}
	return out
}

// TestMapStateTo2225 maps every state of the game to 2225: each is a state
// of 2225's palette, and only the 33 of redstone wire, fire and chorus
// plants change, gaining their connections as none.
func TestMapStateTo2225(t *testing.T) {
	if !bedrockdata.NamesBlocksAsTheGame(2193) || bedrockdata.NamesBlocksAsTheGame(2225) {
		t.Fatal("the protocols that name blocks as the game does are not 2193 alone")
	}
	newer := map[state]bool{}
	for _, s := range palette(t, 2225) {
		newer[s] = true
	}
	changed := map[string]int{}
	for _, s := range gameStates(t) {
		name, props := parseState(s)
		in := make(map[string]any, len(props))
		for k, v := range props {
			in[k] = v
		}
		if step, _ := bedrockdata.BlockStepOf(2225); step.Added[name] != nil {
			in = typed(t, props, step.Added[name])
		}
		outName, out := bedrockdata.MapState(2225, name, in)
		up := stateOf(outName, out)
		if !newer[up] && name != "minecraft:chalkboard" {
			t.Errorf("%s is %s in 2225, no state of its palette", s, up)
		}
		if up != s {
			changed[name]++
		}
	}
	if fmt.Sprint(changed) != "map[minecraft:chorus_plant:1 minecraft:fire:16 minecraft:redstone_wire:16]" {
		t.Errorf("the states 2225 names otherwise: %v", changed)
	}
}

// TestBlockStepDownTo2193 takes every state of 2225's palette back through
// the step: each is a state of 2193's palette, the three blocks 2193 lacks
// through their fallbacks.
func TestBlockStepDownTo2193(t *testing.T) {
	step, ok := bedrockdata.BlockStepOf(2225)
	if _, first := bedrockdata.BlockStepOf(2193); !ok || first {
		t.Fatal("2225 has no step, or 2193 has one")
	}
	older := map[state]bool{}
	for _, s := range palette(t, 2193) {
		older[s] = true
	}
	fallen := map[string]bool{}
	for _, s := range palette(t, 2225) {
		name, props := parseState(s)
		in := make(map[string]any, len(props))
		for k, v := range props {
			in[k] = v
		}
		outName, out := step.Down(name, in)
		if _, ok := step.Fallbacks[name]; ok {
			fallen[name] = true
		}
		if down := stateOf(outName, out); !older[down] {
			t.Errorf("%s of 2225 is %s, no state of 2193", s, down)
		}
	}
	if fmt.Sprint(fallen) != "map[minecraft:disc_press:true minecraft:ice_crystal:true minecraft:icicle:true]" {
		t.Errorf("fallbacks taken for %v", fallen)
	}
}
