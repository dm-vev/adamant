package world

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// updateJavaStates rewrites testdata/java_states.txt.gz with what the Java
// block-state codec gives now. Use it only for a deliberate change of the
// mapping, and say in the commit which states changed and why:
//
//	go test ./server/world -run TestJavaStatesAreStable -update-java-states
var updateJavaStates = flag.Bool("update-java-states", false, "rewrite testdata/java_states.txt.gz; only for a deliberate change of the Java block-state mapping")

const javaStatesFile = "testdata/java_states.txt.gz"

// TestJavaStatesAreStable pins what every Java block state becomes in the
// native palette, and what every native state becomes in Java, errors
// included. The forward domain is every state ID gamedata knows; the reverse
// domain is every native state without and with a water layer, every native
// liquid as a free fluid, and every layer pair the forward map produces.
func TestJavaStatesAreStable(t *testing.T) {
	t.Parallel()
	registry := NewBlockRegistry()
	registry.Finalize()
	lines := javaStateLines(registry)

	if *updateJavaStates {
		if err := writeJavaStates(javaStatesFile, lines); err != nil {
			t.Fatalf("write %s: %v", javaStatesFile, err)
		}
		t.Logf("wrote %d lines to %s", len(lines), javaStatesFile)
		return
	}
	want, err := readJavaStates(javaStatesFile)
	if err != nil {
		t.Fatalf("read %s: %v", javaStatesFile, err)
	}
	if slices.Equal(lines, want) {
		return
	}
	missing, extra := sortedDifference(want, lines), sortedDifference(lines, want)
	t.Errorf("the Java block-state mapping changed: %d lines lost, %d lines new (of %d); rerun with -update-java-states only if the change is deliberate", len(missing), len(extra), len(want))
	for i, line := range missing {
		if i == 20 {
			t.Errorf("... and %d more lost", len(missing)-i)
			break
		}
		t.Errorf("lost: %s", line)
	}
	for i, line := range extra {
		if i == 20 {
			t.Errorf("... and %d more new", len(extra)-i)
			break
		}
		t.Errorf("new:  %s", line)
	}
}

// javaStateLines returns one sorted line per state of both domains of
// TestJavaStatesAreStable.
func javaStateLines(registry chunk.BlockRegistry) []string {
	air := registry.AirRuntimeID()
	water, ok := registry.StateToRuntimeID("minecraft:water", waterSourceProperties)
	if !ok {
		panic("native water source state is missing")
	}
	var lines []string
	reverse := map[NativeState]struct{}{}
	for id := uint32(1); ; id++ {
		name, properties, ok := gamedata.BlockStateFromID(id)
		if !ok {
			break
		}
		source := formatJavaStateLine(name, properties)
		native, err := ResolveState(registry, BlockState{Name: name, Properties: properties})
		if err != nil {
			lines = append(lines, "java "+source+" | "+err.Error())
			continue
		}
		line := "java " + source + " -> " + formatNativeStateLine(registry, native)
		if native.StateID != 0 {
			line += " retained"
		}
		lines = append(lines, line)
		reverse[NativeState{Block: native.Block, Liquid: native.Liquid}] = struct{}{}
	}
	for rid := range uint32(registry.BlockCount()) {
		reverse[NativeState{Block: rid, Liquid: air}] = struct{}{}
		reverse[NativeState{Block: rid, Liquid: water}] = struct{}{}
		if name, _, _ := registry.RuntimeIDToState(rid); registry.LiquidBlock(rid) || name == "minecraft:water" || name == "minecraft:flowing_water" || name == "minecraft:lava" || name == "minecraft:flowing_lava" {
			reverse[NativeState{Block: air, Liquid: rid}] = struct{}{}
		}
	}
	for native := range reverse {
		line := "native " + formatNativeStateLine(registry, native)
		if source, err := reverseNativeState(registry, native); err != nil {
			line += " | " + err.Error()
		} else {
			line += " -> " + formatJavaStateLine(source.Name, source.Properties)
		}
		lines = append(lines, line)
	}
	slices.Sort(lines)
	return lines
}

// formatNativeStateLine writes the block layer of a native state and, unless
// it is air, its liquid layer.
func formatNativeStateLine(registry chunk.BlockRegistry, state NativeState) string {
	name, properties, _ := registry.RuntimeIDToState(state.Block)
	line := formatJavaStateLine(name, properties)
	if state.Liquid != registry.AirRuntimeID() {
		name, properties, _ := registry.RuntimeIDToState(state.Liquid)
		line += " + " + formatJavaStateLine(name, properties)
	}
	return line
}

// formatJavaStateLine writes a state with its properties sorted by key, and
// each value with its type where the type is not evident.
func formatJavaStateLine(name string, properties map[string]any) string {
	var b strings.Builder
	b.WriteString(name)
	b.WriteByte('[')
	for i, key := range slices.Sorted(maps.Keys(properties)) {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(key)
		b.WriteByte('=')
		switch value := properties[key].(type) {
		case string:
			fmt.Fprintf(&b, "%q", value)
		case bool:
			fmt.Fprintf(&b, "%t", value)
		case int32:
			fmt.Fprintf(&b, "%d", value)
		default:
			fmt.Fprintf(&b, "%T(%v)", value, value)
		}
	}
	b.WriteByte(']')
	return b.String()
}

// sortedDifference returns the lines of a not in b, both sorted.
func sortedDifference(a, b []string) []string {
	var out []string
	for _, line := range a {
		if _, found := slices.BinarySearch(b, line); !found {
			out = append(out, line)
		}
	}
	return out
}

func readJavaStates(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	r, err := gzip.NewReader(file)
	if err != nil {
		return nil, err
	}
	var lines []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, r.Close()
}

func writeJavaStates(path string, lines []string) error {
	var buf bytes.Buffer
	w, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}
