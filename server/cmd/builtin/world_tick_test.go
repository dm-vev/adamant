package builtin

import (
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
	"github.com/go-gl/mathgl/mgl64"
)

func TestFillBiomeChangesOnlyMatchingBiomeCells(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		original, ok := world.BiomeByName("plains")
		if !ok {
			t.Fatal("plains biome was not registered")
		}
		desert, ok := world.BiomeByName("desert")
		if !ok {
			t.Fatal("desert biome was not registered")
		}
		tx.SetBiome(cube.Pos{0, 64, 0}, original)
		tx.SetBiome(cube.Pos{1, 64, 0}, desert)
		out := &cmd.Output{}
		(fillBiomeReplaceCommand{
			fillBiomeCommand: fillBiomeCommand{From: mgl64.Vec3{0, 64, 0}, To: mgl64.Vec3{1, 64, 0}, Biome: "minecraft:desert"},
			Filter:           "minecraft:plains",
		}).Run(nil, out, tx)
		if out.ErrorCount() != 0 {
			t.Fatalf("fillbiome errors: %v", out.Errors())
		}
		if got := tx.Biome(cube.Pos{0, 64, 0}).EncodeBiome(); got != desert.EncodeBiome() {
			t.Fatalf("filled biome id = %d, want %d", got, desert.EncodeBiome())
		}
		if got := tx.Biome(cube.Pos{1, 64, 0}).EncodeBiome(); got != desert.EncodeBiome() {
			t.Fatalf("unmatched biome changed to %d, want %d", got, desert.EncodeBiome())
		}
	})
}

// TestTickQueryReportsTickTimes checks tick query tells the state of the
// world, the average time of its last ticks against the target and their
// percentiles, as Java's does.
func TestTickQueryReportsTickTimes(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	for range 150 {
		w.AdvanceTick()
	}
	if average, samples := w.TickTimes(); len(samples) != 100 || average <= 0 {
		t.Fatalf("tick times: average %v of %d samples, want 100 samples", average, len(samples))
	}
	<-w.Exec(func(tx *world.Tx) {
		out := &cmd.Output{}
		tickQueryCommand{}.Run(nil, out, tx)
		var lines []string
		for _, m := range out.Messages() {
			lines = append(lines, m.String())
		}
		if len(lines) != 3 || lines[0] != "The game is running normally" ||
			!strings.HasPrefix(lines[1], "Target tick rate: 20.0 per second.\nAverage time per tick: ") || !strings.HasSuffix(lines[1], "ms (Target: 50.0ms)") ||
			!strings.HasPrefix(lines[2], "Percentiles: P50: ") || !strings.HasSuffix(lines[2], "ms. Sample: 100") {
			t.Fatalf("tick query printed %q", lines)
		}
	})
}
