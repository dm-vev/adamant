package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

const completeCatalog = `ADAMANT_BLOCK_CATALOG {"event":"start","schema":2,"expectedTypes":1}
ADAMANT_BLOCK_CATALOG {"event":"type-start","type":"minecraft:test","properties":4,"defaultType":"minecraft:test","defaultStates":{"active":true,"inactive":false,"level":-2,"mode":"stone"}}
ADAMANT_BLOCK_CATALOG {"event":"state","type":"minecraft:test","resolvedType":"minecraft:test","states":{"active":true,"inactive":false,"level":-2,"mode":"stone"}}
ADAMANT_BLOCK_CATALOG {"event":"type-complete","type":"minecraft:test","generated":1,"rejected":0,"states":1}
ADAMANT_BLOCK_CATALOG {"event":"structure","id":"adamant_catalog:blocks_0","states":1}
ADAMANT_BLOCK_CATALOG {"event":"complete","expectedTypes":1,"completedTypes":1,"incompleteTypes":0,"states":1,"rejectedCombinations":0,"structures":1,"worldStates":0}
ADAMANT_BLOCK_CATALOG {"event":"world-complete","states":0}
`

func TestParseCatalogPreservesPropertyTypes(t *testing.T) {
	got, err := parseCatalog(strings.NewReader(completeCatalog))
	if err != nil {
		t.Fatal(err)
	}
	states := got.States[0].States
	if states["active"] != uint8(1) || states["inactive"] != uint8(0) || states["level"] != int32(-2) || states["mode"] != "stone" {
		t.Fatalf("property conversions: %#v", states)
	}
}

func TestParseCatalogRejectsIncompleteCapture(t *testing.T) {
	for _, input := range []string{
		strings.Split(completeCatalog, `ADAMANT_BLOCK_CATALOG {"event":"complete"`)[0],
		strings.Split(completeCatalog, `ADAMANT_BLOCK_CATALOG {"event":"world-complete"`)[0],
		strings.Replace(completeCatalog, `"incompleteTypes":0`, `"incompleteTypes":1`, 1),
		strings.Replace(completeCatalog, `"structures":1`, `"structures":2`, 1),
	} {
		if _, err := parseCatalog(strings.NewReader(input)); err == nil {
			t.Fatal("incomplete capture accepted")
		}
	}
}

func TestParseCatalogRejectsInvalidPropertyTypes(t *testing.T) {
	for _, value := range []string{`1.5`, `2147483648`, `null`, `[]`} {
		input := strings.ReplaceAll(completeCatalog, `"level":-2`, `"level":`+value)
		if _, err := parseCatalog(strings.NewReader(input)); err == nil {
			t.Errorf("invalid property %s accepted", value)
		}
	}
}

func TestParseCatalogAcceptsAlreadyEnumeratedAlias(t *testing.T) {
	input := `ADAMANT_BLOCK_CATALOG {"event":"start","schema":2,"expectedTypes":2}
ADAMANT_BLOCK_CATALOG {"event":"type-start","type":"minecraft:a","properties":1,"defaultType":"minecraft:a","defaultStates":{"material":"a"}}
ADAMANT_BLOCK_CATALOG {"event":"state","type":"minecraft:a","resolvedType":"minecraft:a","states":{"material":"a"}}
ADAMANT_BLOCK_CATALOG {"event":"state","type":"minecraft:a","resolvedType":"minecraft:b","states":{"material":"b"}}
ADAMANT_BLOCK_CATALOG {"event":"type-complete","type":"minecraft:a","generated":2,"rejected":0,"states":2}
ADAMANT_BLOCK_CATALOG {"event":"type-start","type":"minecraft:b","properties":1,"defaultType":"minecraft:b","defaultStates":{"material":"b"}}
ADAMANT_BLOCK_CATALOG {"event":"type-complete","type":"minecraft:b","generated":2,"rejected":0,"states":0}
ADAMANT_BLOCK_CATALOG {"event":"structure","id":"adamant_catalog:blocks_0","states":2}
ADAMANT_BLOCK_CATALOG {"event":"complete","expectedTypes":2,"completedTypes":2,"incompleteTypes":0,"states":2,"rejectedCombinations":0,"structures":1,"worldStates":0}
ADAMANT_BLOCK_CATALOG {"event":"world-complete","states":0}`
	got, err := parseCatalog(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.States) != 2 || got.States[0].Name != "minecraft:a" || got.States[1].Name != "minecraft:b" {
		t.Fatalf("resolved identities lost: %#v", got.States)
	}
}

func TestCanonicalStatesUsesPersistedPropertiesAndActualDefault(t *testing.T) {
	const version int32 = 18168865
	c := catalog{
		States: []blockState{
			{Name: "minecraft:oak_log", States: map[string]any{"old_log_type": "oak", "pillar_axis": "x"}},
			{Name: "minecraft:oak_log", States: map[string]any{"old_log_type": "oak", "pillar_axis": "y"}},
		},
		Defaults:   map[string]map[string]any{"minecraft:oak_log": {"old_log_type": "oak", "pillar_axis": "y"}},
		Structures: []savedStructure{{"adamant_catalog:blocks_0", 2}},
	}
	s := emptyStructure(map[int]int32{0: 0, 256: 1})
	s.Structure.Palette.Default.Blocks = []blockState{
		{Name: "minecraft:oak_log", States: map[string]any{"pillar_axis": "x"}, Version: version},
		{Name: "minecraft:oak_log", States: map[string]any{"pillar_axis": "y"}, Version: version},
	}
	data, err := nbt.MarshalEncoding(s, nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	got, err := canonicalStates(c, nil, version, func(string) ([]byte, error) { return data, nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].States["pillar_axis"] != "y" || got[1].States["pillar_axis"] != "x" {
		t.Fatalf("registry default or structure coordinates lost: %#v", got)
	}
	for _, state := range got {
		if _, ok := state.States["old_log_type"]; ok {
			t.Fatal("Script API compatibility property leaked into canonical NBT")
		}
		if state.Version != version {
			t.Fatalf("persisted version changed: %d", state.Version)
		}
	}
}

func TestCanonicalStatesRequiresWorldEvidenceForStructureHoles(t *testing.T) {
	const version int32 = 18168865
	state := blockState{Name: "minecraft:structure_void", States: map[string]any{}, Version: version}
	c := catalog{States: []blockState{state}, Defaults: map[string]map[string]any{state.Name: {}}, Structures: []savedStructure{{"adamant_catalog:blocks_0", 1}}, WorldStates: map[int]bool{0: true}}
	data, err := nbt.MarshalEncoding(emptyStructure(nil), nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	load := func(string) ([]byte, error) { return data, nil }
	if _, err := canonicalStates(c, nil, version, load); err == nil {
		t.Fatal("missing world evidence accepted")
	}
	got, err := canonicalStates(c, map[string][]blockState{state.Name: {state}}, version, load)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != state.Name || got[0].Version != version {
		t.Fatalf("world-only state lost: %#v", got)
	}
	if propertiesMatch(map[string]any{"bit": uint8(1)}, map[string]any{"bit": int32(1)}) {
		t.Fatal("different NBT property types treated as equivalent")
	}
}

func emptyStructure(values map[int]int32) structureFile {
	var s structureFile
	s.FormatVersion = 2
	s.Size = []int32{16, 16, 16}
	s.WorldOrigin = []int32{0, 0, 0}
	var indices [4096]int32
	for i := range indices {
		indices[i] = -1
	}
	for i, value := range values {
		indices[i] = value
	}
	s.Structure.BlockIndices = []any{indices}
	s.Structure.Palette.Default.BlockPositionData = map[string]any{}
	return s
}

func TestReadWorldStatesDistinguishesEOFAndTruncation(t *testing.T) {
	state := blockState{Name: "minecraft:test", States: map[string]any{"level": int32(1)}, Version: 18168865}
	first, err := nbt.MarshalEncoding(state, nbt.NetworkLittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	state.States["level"] = int32(2)
	second, err := nbt.MarshalEncoding(state, nbt.NetworkLittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	data := append(first, second...)
	got, err := readWorldStates(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	variants := got[state.Name]
	if len(variants) != 2 || variants[0].States["level"] != int32(1) || variants[1].States["level"] != int32(2) {
		t.Fatalf("concatenated palette states lost: %#v", got)
	}
	for _, end := range []int{len(first) - 1, len(data) - 1} {
		if _, err := readWorldStates(bytes.NewReader(data[:end])); err == nil {
			t.Fatalf("truncated compound at byte %d accepted as clean EOF", end)
		}
	}
}
