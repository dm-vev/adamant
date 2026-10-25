package overworld

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// TestStructureLocateMatchesJava checks the structures the locate command of
// the Java 26.3 server found from three origins on two seeds. Starts used to
// skip the biome checks of woodland mansions, monuments, mineshafts and
// ruined portals, so mansions and portals stood in every biome and mesa
// mineshafts never started; pillager outposts ignored the exclusion zone of
// villages; ruined portals were mirrored the wrong way round and buried at
// y 0; and locate took the nearest start on a ring rather than the first.
func TestStructureLocateMatchesJava(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_locate.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Seed      int64   `json:"seed"`
		Origin    [2]int  `json:"origin"`
		Structure string  `json:"structure"`
		Position  *[2]int `json:"position"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	generators := map[int64]*Overworld{}
	for _, c := range cases {
		g := generators[c.Seed]
		if g == nil {
			g = New(c.Seed)
			generators[c.Seed] = g
		}
		t.Run(fmt.Sprintf("seed_%d_%s_from_%d_%d", c.Seed, c.Structure, c.Origin[0], c.Origin[1]), func(t *testing.T) {
			loc, found, err := g.FindNearestMapFeature(c.Structure, cube.Pos{c.Origin[0], 64, c.Origin[1]}, 100, nil)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.Position == nil && found:
				t.Fatalf("found %s at %v, Java found none", c.Structure, loc.Position)
			case c.Position != nil && !found:
				t.Fatalf("found no %s, Java found one at %v", c.Structure, *c.Position)
			case c.Position != nil && (loc.Position[0] != c.Position[0] || loc.Position[2] != c.Position[1]):
				t.Fatalf("found %s at %d, %d, Java at %v", c.Structure, loc.Position[0], loc.Position[2], *c.Position)
			}
		})
	}
}
