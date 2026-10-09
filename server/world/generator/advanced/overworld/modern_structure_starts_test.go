package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

type configuredStartSource struct {
	modernTerrainSource
	biome  string
	height int32
}

func (s configuredStartSource) JavaBiomeAt(x, y, z int32) string             { return s.biome }
func (s configuredStartSource) JavaNoiseBiomeAt(x, y, z int32) string        { return s.biome }
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

func TestConfiguredStructureStartsMatchJava(t *testing.T) {
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
	expectedTemplates := map[string]*javastructure.Template{}
	checked := map[*javastructure.Template]string{}
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
				if piece.Projection == javastructure.TerrainMatching {
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
							expected, err = javastructure.LoadOrCreate(name)
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

type configuredStartGate struct {
	configuredStartSource
	entered, release chan struct{}
	once             sync.Once
}

func (s *configuredStartGate) JavaNoiseBiomeAt(x, y, z int32) string {
	s.once.Do(func() { close(s.entered); <-s.release })
	return s.biome
}

func TestConcurrentStructureStartReadersRetainCompleteGraph(t *testing.T) {
	source := configuredStartSource{biome: "minecraft:plains", height: 64}
	planner := newModernStructures(0, source)
	set := configuredStartSet(t, planner, "trial_chambers")
	expected, valid, err := planner.buildStart(set, 14, 5)
	if err != nil || !valid {
		t.Fatalf("real trial start: valid=%v err=%v", valid, err)
	}
	gate := &configuredStartGate{configuredStartSource: source, entered: make(chan struct{}), release: make(chan struct{})}
	planner.source = gate
	const readers = 17
	results := make([]modernStructureStart, readers)
	validity := make([]bool, readers)
	var done, ready sync.WaitGroup
	done.Add(readers)
	go func() { defer done.Done(); results[0], validity[0] = planner.makeStart(set, 14, 5) }()
	// Hold the actual graph build at biome validation while other generators join.
	<-gate.entered
	ready.Add(readers - 1)
	for i := 1; i < readers; i++ {
		go func() { defer done.Done(); ready.Done(); results[i], validity[i] = planner.makeStart(set, 14, 5) }()
	}
	ready.Wait()
	runtime.Gosched()
	close(gate.release)
	done.Wait()
	for i := range results {
		if !validity[i] || !reflect.DeepEqual(results[i], expected) {
			t.Errorf("reader %d lost or changed the completed trial graph", i)
		}
	}
	cached, valid := planner.makeStart(set, 14, 5)
	if !valid || !reflect.DeepEqual(cached, expected) {
		t.Fatal("cached start differs from the completed trial graph")
	}
}
