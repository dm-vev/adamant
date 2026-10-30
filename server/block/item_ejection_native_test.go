package block

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type ejectionNativeWords struct {
	words  []uint32
	used   int
	active bool
}

func (s *ejectionNativeWords) Uint64() uint64 {
	if !s.active {
		return 0
	}
	if s.used == len(s.words) {
		panic("ejection consumed an unobserved native word")
	}
	w := s.words[s.used]
	s.used++
	return uint64(w) << 32
}

func TestItemEjectionVelocityMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_ejection_velocities.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Seed   uint32
			Face   int
			Frames []struct {
				Face          int
				Words         []uint32
				WordsConsumed int `json:"words_consumed"`
				Velocity      [3]float32
			}
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 36 {
		t.Fatal("incomplete native ejection velocity corpus")
	}
	for _, c := range native.Scenarios {
		t.Run(fmt.Sprintf("seed%d/face%d", c.Seed, c.Face), func(t *testing.T) {
			if len(c.Frames) != 40 {
				t.Fatal("missing native consecutive Gaussian-cache frames")
			}
			source := &ejectionNativeWords{}
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, RandSource: source}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				for i, f := range c.Frames {
					if f.Face != c.Face || len(f.Words) != f.WordsConsumed {
						t.Fatal("invalid native ejection controls")
					}
					source.words, source.used, source.active = f.Words, 0, true
					got := nativeItemEjectionVelocity(cube.Face(c.Face), tx)
					if source.used != len(f.Words) {
						t.Fatalf("frame %d consumed %d words, native=%d", i, source.used, len(f.Words))
					}
					for axis, want := range f.Velocity {
						if math.Float32bits(float32(got[axis])) != math.Float32bits(want) {
							t.Fatalf("frame %d axis %d velocity=%.9g, native=%.9g", i, axis, got[axis], want)
						}
					}
				}
				source.active = false
			})
		})
	}
}

func TestCrafterEjectionOriginMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_ejection_velocities.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Origins  []struct {
			Position    cube.Pos
			Orientation int
			Origin      [3]float32
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Origins) != 48 {
		t.Fatal("incomplete native crafter origins")
	}
	faces := [12]cube.Face{cube.FaceDown, cube.FaceDown, cube.FaceDown, cube.FaceDown, cube.FaceUp, cube.FaceUp, cube.FaceUp, cube.FaceUp, cube.FaceWest, cube.FaceEast, cube.FaceNorth, cube.FaceSouth}
	for _, c := range native.Origins {
		if c.Orientation < 0 || c.Orientation >= len(faces) {
			t.Fatal("invalid native orientation")
		}
		got := nativeItemEjectionPosition(c.Position, faces[c.Orientation])
		for i, want := range c.Origin {
			if math.Float32bits(float32(got[i])) != math.Float32bits(want) {
				t.Fatalf("position %v orientation %d axis %d origin=%.9g, native=%.9g", c.Position, c.Orientation, i, got[i], want)
			}
		}
	}
}
