package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	_ "github.com/df-mc/dragonfly/server/world/biome"
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
			fillBiomeCommand: fillBiomeCommand{X1: 0, Y1: 64, Z1: 0, X2: 1, Y2: 64, Z2: 0, Biome: "minecraft:desert"},
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
