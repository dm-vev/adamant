package structure

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"reflect"
	"strings"
	"testing"
)

func TestJigsawMissingRootAliasRetainsOriginalStart(t *testing.T) {
	options := JigsawOptions{Pool: "village/plains/town_centers", Origin: [3]int32{0, 80, 0}, MaxDepth: 1, HeightRange: cube.Range{-64, 319}, HeightAt: func(x, z int32) int32 { return 80 }}
	originalRandom := vanilla.NewLegacyRandom(0)
	original, _, originalHasStub, err := buildPreparedJigsaw(options, &originalRandom)
	if err != nil {
		t.Fatal(err)
	}
	options.PoolAliases = map[string]string{"village/plains/town_centers": "missing_root_alias_target"}
	aliasedRandom := vanilla.NewLegacyRandom(0)
	got, _, gotHasStub, err := buildPreparedJigsaw(options, &aliasedRandom)
	if err != nil {
		t.Fatal(err)
	}
	if !originalHasStub || !gotHasStub || !reflect.DeepEqual(got, original) {
		t.Fatal("missing alias target did not retain the original root placement")
	}
	if got, want := aliasedRandom.NextLong(), originalRandom.NextLong(); got != want {
		t.Fatalf("missing root alias changed RNG: %d, want %d", got, want)
	}
}

func TestJigsawMissingConnectorAliasSkipsAttachment(t *testing.T) {
	options := JigsawOptions{Pool: "village/plains/town_centers", Origin: [3]int32{0, 80, 0}, MaxDepth: 1, HeightRange: cube.Range{-64, 319}, HeightAt: func(x, z int32) int32 { return 80 }}
	random := vanilla.NewLegacyRandom(0)
	root, _, hasStub, err := buildPreparedJigsaw(options, &random)
	if err != nil {
		t.Fatal(err)
	}
	if !hasStub {
		t.Fatal("unaliased fixture must have a valid root stub")
	}
	if len(root.Pieces) < 2 {
		t.Fatalf("unaliased fixture must attach children: got %d pieces", len(root.Pieces))
	}
	piece := root.Pieces[0]
	options.PoolAliases = map[string]string{}
	connectors, err := decodeJigsawConnectors(piece.Template.Palettes[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, joint := range connectors {
		if joint.pool != "" {
			options.PoolAliases[strings.TrimPrefix(joint.pool, "minecraft:")] = "missing_connector_alias_target"
		}
	}
	if len(options.PoolAliases) == 0 {
		t.Fatal("builtin fixture has no connector pools")
	}
	root.Pieces = root.Pieces[:1]
	root.Pieces[0].Junctions = nil
	root.Junctions = nil
	random = vanilla.NewLegacyRandom(0)
	result, _, hasStub, err := buildPreparedJigsaw(options, &random)
	if err != nil || !hasStub {
		t.Fatalf("missing connector pool should be skipped: stub=%v err=%v", hasStub, err)
	}
	if !reflect.DeepEqual(result, root) {
		t.Fatal("missing connector targets changed the root-only result")
	}
}

func TestJigsawEmptyAliasedRootConsumesSourceRotation(t *testing.T) {
	random := vanilla.NewLegacyRandom(0)
	result, _, hasStub, err := buildPreparedJigsaw(JigsawOptions{
		Pool:        "village/plains/town_centers",
		PoolAliases: map[string]string{"village/plains/town_centers": "empty"},
	}, &random)
	if err != nil {
		t.Fatal(err)
	}
	if hasStub || len(result.Pieces) != 0 {
		t.Fatal("empty aliased pool produced a root")
	}
	// Executed Java JigsawPlacement.addPieces, WorldgenRandom(LegacyRandomSource(0)).
	if got := random.NextLong(); got != -3109364765729502342 {
		t.Fatalf("following source stream = %d, want -3109364765729502342", got)
	}
}
func buildPreparedJigsaw(options JigsawOptions, random *vanilla.Random) (JigsawResult, [3]int32, bool, error) {
	start, hasStub, err := PrepareJigsaw(options, random)
	if err != nil || !hasStub {
		return JigsawResult{}, [3]int32{}, hasStub, err
	}
	position := start.Position()
	result, err := start.Build(random)
	return result, position, true, err
}
