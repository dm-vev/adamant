package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

type configuredStartSource struct {
	modernTerrainSource
	biome  string
	height int32
}

func (s configuredStartSource) BiomeAt(x, y, z int32) string                 { return s.biome }
func (s configuredStartSource) NoiseBiomeAt(x, y, z int32) string            { return s.biome }
func (s configuredStartSource) BaseHeight(x, z int32, oceanFloor bool) int32 { return s.height }

func configuredStartSet(t *testing.T, planner *modernStructures, name string) modernStructureSet {
	t.Helper()
	for _, set := range planner.sets {
		if set.name == name {
			return set
		}
	}
	t.Fatalf("missing structure set %q", name)
	return modernStructureSet{}
}

func TestConfiguredStructureStartsMatchReference(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_structure_starts.json")
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Graphs []struct {
			Seed      int64    `json:"seed"`
			Biome     string   `json:"biome"`
			Set       string   `json:"set"`
			Chunk     [2]int32 `json:"chunk"`
			Height    int32    `json:"first_free_height"`
			Structure string   `json:"structure"`
			Pieces    []struct {
				Box        [6]int32 `json:"box"`
				Origin     [3]int32 `json:"origin"`
				Rotation   int      `json:"rotation"`
				Ground     int32    `json:"ground_delta"`
				Projection string   `json:"projection"`
				Template   string   `json:"template"`
			} `json:"pieces"`
		} `json:"graphs"`
	}
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	expectedTemplates := map[string]*structure.Template{}
	checked := map[*structure.Template]string{}
	for _, row := range input.Graphs {
		t.Run(fmt.Sprintf("%s/%s/%d/%v", row.Set, row.Biome, row.Seed, row.Chunk), func(t *testing.T) {
			// Java ChunkGenerator.createStructures, using its real flat generator,
			// placement, biome validation and weighted retries; not direct jigsaw calls.
			planner := newModernStructures(row.Seed, configuredStartSource{biome: row.Biome, height: row.Height})
			set := configuredStartSet(t, planner, row.Set)
			got, valid, err := planner.buildStart(set, row.Chunk[0], row.Chunk[1])
			if err != nil {
				t.Fatal(err)
			}
			if valid != (row.Structure != "") || got.name != row.Structure || len(got.pieces) != len(row.Pieces) {
				t.Fatalf("start valid=%v name=%s pieces=%d; source name=%s pieces=%d", valid, got.name, len(got.pieces), row.Structure, len(row.Pieces))
			}
			for i, p := range got.pieces {
				piece := p.(modernJigsawPiece).piece
				want := row.Pieces[i]
				box := [6]int32{piece.Bounds.Min[0], piece.Bounds.Min[1], piece.Bounds.Min[2], piece.Bounds.Max[0], piece.Bounds.Max[1], piece.Bounds.Max[2]}
				projection := "rigid"
				if piece.Projection == structure.TerrainMatching {
					projection = "terrain_matching"
				}
				if box != want.Box || piece.Origin != want.Origin || int(piece.Rotation) != want.Rotation || piece.GroundLevelDelta != want.Ground || projection != want.Projection {
					t.Fatalf("piece %d: box=%v origin=%v rotation=%v ground=%d projection=%s; source=%+v", i, box, piece.Origin, piece.Rotation, piece.GroundLevelDelta, projection, want)
				}
				if want.Template != "" {
					name := strings.TrimPrefix(want.Template, "minecraft:")
					if checked[piece.Template] != name {
						expected := expectedTemplates[name]
						if expected == nil {
							expected, err = structure.LoadOrCreate(name)
							if err != nil {
								t.Fatal(err)
							}
							expectedTemplates[name] = expected
						}
						if !reflect.DeepEqual(piece.Template, expected) {
							t.Fatalf("piece %d did not select source template %s", i, name)
						}
						checked[piece.Template] = name
					}
				}
			}
		})
	}
}
