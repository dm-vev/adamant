package block

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// updateShapeNames rewrites the golden file of TestShapeNamesMatch to the names slabs, stairs and walls take now.
var updateShapeNames = flag.Bool("update-shape-names", false, "rewrite testdata/shape_names.txt to the names slabs, stairs and walls of each material take now")

// shapeNamesGolden holds the names of the slab, double slab, stairs and wall of every material that one of
// them is made of, or - for a shape that has none of the material.
const shapeNamesGolden = "testdata/shape_names.txt"

// shapeName returns what name gives for a material, or "-" if it panics, as it does for a material that the
// shape has no block of.
func shapeName(name func(world.Block) string, b world.Block) (s string) {
	defer func() {
		if recover() != nil {
			s = "-"
		}
	}()
	return name(b)
}

// shapeNameLines returns a line for every material of SlabBlocks, StairsBlocks and WallBlocks, in that order and
// once each, with the names of its slab, double slab, stairs and wall.
func shapeNameLines() []string {
	var materials []world.Block
	for _, b := range slices.Concat(SlabBlocks(), StairsBlocks(), WallBlocks()) {
		if !slices.Contains(materials, b) {
			materials = append(materials, b)
		}
	}
	slab := func(b world.Block) string { return slabName(b, false) }
	double := func(b world.Block) string { return slabName(b, true) }
	var lines []string
	for _, b := range materials {
		lines = append(lines, fmt.Sprintf("%T%+v slab=%s double=%s stairs=%s wall=%s", b, b,
			shapeName(slab, b), shapeName(double, b), shapeName(encodeStairsBlock, b), shapeName(encodeWallBlock, b)))
	}
	return lines
}

// TestShapeNamesMatch requires the names of the slabs, stairs and walls of every material that any of them is
// made of to be those of the golden file, which the switches that the table of names replaced wrote.
func TestShapeNamesMatch(t *testing.T) {
	current := shapeNameLines()
	if *updateShapeNames {
		var b strings.Builder
		b.WriteString("# The names of the slab, double slab, stairs and wall of each material, without the namespace, or - for none.\n")
		b.WriteString("# Regenerate with: go test -run TestShapeNamesMatch -update-shape-names ./server/block\n\n")
		for _, l := range current {
			b.WriteString(l + "\n")
		}
		if err := os.WriteFile(shapeNamesGolden, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(shapeNamesGolden)
	if err != nil {
		t.Fatalf("read the golden file: %v (generate it with -update-shape-names)", err)
	}
	defer f.Close()
	var want []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if l := sc.Text(); l != "" && !strings.HasPrefix(l, "#") {
			want = append(want, l)
		}
	}
	for i := range max(len(current), len(want)) {
		switch {
		case i >= len(current):
			t.Errorf("material gone: %s", want[i])
		case i >= len(want):
			t.Errorf("new material: %s", current[i])
		case current[i] != want[i]:
			t.Errorf("names = %s, want %s", current[i], want[i])
		}
	}
}
