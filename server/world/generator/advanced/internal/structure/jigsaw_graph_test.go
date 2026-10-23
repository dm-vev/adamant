package structure

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
)

// Captured from mapped Java 26.3 JigsawPlacement.addPieces with vanilla world
// registries and StructureTemplateManager; raw Legacy seeds, no start-height draw.
// Rows cover the depth-zero stub, ordinary depths 1/2, depth-7 internal expansion
// (including priority ordering and terminal fallback), padding rejection, and empty root.

type graphFixture struct {
	Scenario         string              `json:"scenario"`
	Anchor           *string             `json:"anchor"`
	ExpansionHack    bool                `json:"expansion_hack"`
	ProjectHeightmap *string             `json:"project_heightmap"`
	MaxDistance      [2]int32            `json:"max_distance"`
	HeightRange      [2]int32            `json:"height_range"`
	Padding          [2]int32            `json:"padding"`
	CheckBounds      bool                `json:"check_bounds"`
	FirstFreeHeight  int32               `json:"first_free_height"`
	Seed             int64               `json:"seed"`
	Position         [3]int32            `json:"position"`
	Depth            int                 `json:"depth"`
	Pool             string              `json:"start_pool"`
	Stub             bool                `json:"stub"`
	StubPosition     [3]int32            `json:"stub_position"`
	Pieces           []graphPieceFixture `json:"pieces"`
	NextLong         int64               `json:"next_long"`
}

type graphPieceFixture struct {
	Templates   []string           `json:"templates"`
	Parts       []graphPartFixture `json:"parts"`
	Origin      [3]int32           `json:"origin"`
	Rotation    int                `json:"rotation"`
	Box         [6]int32           `json:"box"`
	GroundDelta int32              `json:"ground_delta"`
	Projection  string             `json:"projection"`
	Junctions   [][]any            `json:"junctions"`
}

type graphPartFixture struct {
	Template string `json:"template"`
	Feature  string `json:"feature"`
	Empty    bool   `json:"empty"`
}

func TestJigsawGraphOracle(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_jigsaw_graphs.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		HorizontalVerticalDistance int32          `json:"horizontal_vertical_distance"`
		DimensionPadding           int32          `json:"dimension_padding"`
		HeightAccessor             [2]int32       `json:"height_accessor"`
		Graphs                     []graphFixture `json:"graphs"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	config, err := gamedata.LoadJSON("structure", "trial_chambers")
	if err != nil {
		t.Fatal(err)
	}
	loaded := map[string]*Template{}
	pointerNames := map[*Template]string{}
	for rowIndex, row := range fixture.Graphs {
		t.Run(fmt.Sprintf("row_%d_seed_%d_depth_%d", rowIndex, row.Seed, row.Depth), func(t *testing.T) {
			random := vanilla.NewLegacyRandom(row.Seed)
			options := JigsawOptions{
				Pool: row.Pool, Origin: row.Position, MaxDepth: row.Depth,
				MaxDistance: fixture.HorizontalVerticalDistance, MaxVerticalDistance: fixture.HorizontalVerticalDistance,
				HeightRange:   cube.Range{int(fixture.HeightAccessor[0]), int(fixture.HeightAccessor[0] + fixture.HeightAccessor[1] - 1)},
				PaddingBottom: fixture.DimensionPadding, PaddingTop: fixture.DimensionPadding, CheckBounds: true, Seed: row.Seed,
			}
			if row.Scenario == "" {
				options.PoolAliases, err = ResolvePoolAliases(config["pool_aliases"], row.Position, row.Seed)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				options.MaxDistance, options.MaxVerticalDistance = row.MaxDistance[0], row.MaxDistance[1]
				options.HeightRange = cube.Range{int(row.HeightRange[0]), int(row.HeightRange[1])}
				options.PaddingBottom, options.PaddingTop = row.Padding[0], row.Padding[1]
				options.CheckBounds, options.ExpansionHack = row.CheckBounds, row.ExpansionHack
				if row.Anchor != nil {
					options.StartJigsaw = *row.Anchor
				}
				options.ProjectStartToHeightmap = row.ProjectHeightmap != nil
				// The source oracle uses a real 128-layer FlatLevelSource.
				options.HeightAt = func(x, z int32) int32 { return row.FirstFreeHeight }
			}
			got, position, hasStub, err := buildPreparedJigsaw(options, &random)
			if err != nil {
				t.Fatal(err)
			}
			if hasStub != row.Stub || (hasStub && position != row.StubPosition) {
				t.Fatalf("stub = (%v, %v), want (%v, %v)", hasStub, position, row.Stub, row.StubPosition)
			}
			if len(got.Pieces) != len(row.Pieces) {
				t.Fatalf("piece count = %d, want %d", len(got.Pieces), len(row.Pieces))
			}
			for pieceIndex, piece := range got.Pieces {
				want := row.Pieces[pieceIndex]
				name := fmt.Sprintf("piece_%03d", pieceIndex)
				if piece.Origin != want.Origin || int(piece.Rotation) != want.Rotation {
					t.Errorf("%s origin/rotation = %v/%d, want %v/%d", name, piece.Origin, piece.Rotation, want.Origin, want.Rotation)
				}
				box := [6]int32{piece.Bounds.Min[0], piece.Bounds.Min[1], piece.Bounds.Min[2], piece.Bounds.Max[0], piece.Bounds.Max[1], piece.Bounds.Max[2]}
				if box != want.Box {
					t.Errorf("%s bounds = %v, want %v", name, box, want.Box)
				}
				if piece.GroundLevelDelta != want.GroundDelta {
					t.Errorf("%s ground delta = %d, want %d", name, piece.GroundLevelDelta, want.GroundDelta)
				}
				projection := "rigid"
				if piece.Projection == TerrainMatching {
					projection = "terrain_matching"
				}
				if projection != want.Projection {
					t.Errorf("%s projection = %q, want %q", name, projection, want.Projection)
				}
				parts := piece.parts
				if len(parts) == 0 && piece.Template != nil {
					parts = []jigsawPart{{template: piece.Template}}
				}
				partCount := len(want.Parts)
				if want.Parts == nil {
					partCount = len(want.Templates)
				}
				if len(parts) != partCount {
					t.Errorf("%s part count = %d, want %d", name, len(parts), partCount)
				} else {
					for partIndex, part := range parts {
						var expectedPart graphPartFixture
						if want.Parts != nil {
							expectedPart = want.Parts[partIndex]
						} else {
							expectedPart.Template = want.Templates[partIndex]
						}
						if expectedPart.Empty {
							if part.template != nil || part.feature != nil {
								t.Errorf("%s part %d is not empty", name, partIndex)
							}
							continue
						}
						if expectedPart.Feature != "" {
							feature, ok := part.feature.(string)
							if !ok || feature != expectedPart.Feature || part.template != nil {
								t.Errorf("%s part %d feature = %v, want %s", name, partIndex, part.feature, expectedPart.Feature)
							}
							continue
						}
						if part.feature != nil {
							t.Errorf("%s part %d has unexpected feature %v", name, partIndex, part.feature)
						}
						wantName := strings.TrimPrefix(expectedPart.Template, "minecraft:")
						expected := loaded[wantName]
						if expected == nil {
							expected, err = LoadOrCreate(wantName)
							if err != nil {
								t.Fatalf("load expected template %q: %v", wantName, err)
							}
							loaded[wantName] = expected
						}
						if pointerNames[part.template] != wantName {
							if other, exists := pointerNames[part.template]; exists && other != wantName {
								t.Errorf("%s part %d reused template pointer for %q, want %q", name, partIndex, other, wantName)
							}
							if !reflect.DeepEqual(part.template, expected) {
								t.Errorf("%s part %d template contents differ from %q", name, partIndex, wantName)
							}
							pointerNames[part.template] = wantName
						}
					}
				}
				if len(piece.Junctions) != len(want.Junctions) {
					t.Errorf("%s junction count = %d, want %d", name, len(piece.Junctions), len(want.Junctions))
				} else {
					for junctionIndex, junction := range piece.Junctions {
						wantJunction := want.Junctions[junctionIndex]
						if len(wantJunction) != 5 {
							t.Fatalf("%s junction %d malformed: %v", name, junctionIndex, wantJunction)
						}
						projection, ok := wantJunction[4].(string)
						if !ok {
							t.Fatalf("%s junction %d projection malformed: %v", name, junctionIndex, wantJunction)
						}
						values := [4]int32{}
						for i := range values {
							n, ok := wantJunction[i].(float64)
							if !ok {
								t.Fatalf("%s junction %d field %d malformed: %v", name, junctionIndex, i, wantJunction[i])
							}
							values[i] = int32(n)
						}
						gotProjection := "rigid"
						if junction.DestProjection == TerrainMatching {
							gotProjection = "terrain_matching"
						}
						if [5]any{junction.X, junction.Y, junction.Z, junction.DeltaY, gotProjection} != [5]any{values[0], values[1], values[2], values[3], projection} {
							t.Errorf("%s junction %d = (%d,%d,%d,%d,%s), want (%d,%d,%d,%d,%s)", name, junctionIndex, junction.X, junction.Y, junction.Z, junction.DeltaY, gotProjection, values[0], values[1], values[2], values[3], projection)
						}
					}
				}
			}
			if gotNext := random.NextLong(); gotNext != row.NextLong {
				t.Errorf("following RNG = %d, want %d", gotNext, row.NextLong)
			}
		})
	}
}
