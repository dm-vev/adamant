package end

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/internal/java26"
)

func TestEndPlacementMatchesJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_placement.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Kind           string
		Seed, NextLong int64
		Xoroshiro      bool
		Count          int
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s/seed=%d/xor=%v", tc.Kind, tc.Seed, tc.Xoroshiro), func(t *testing.T) {
			random := newEndRand(tc.Seed)
			if tc.Xoroshiro {
				random = newEndRandWithRandom(java26.NewXoroshiroRandom(tc.Seed))
			}
			var count int
			switch tc.Kind {
			case "island":
				count = random.islandCount()
			case "gateway":
				if random.OneIn700() {
					count = 1
				}
			default:
				t.Fatalf("unknown feature %q", tc.Kind)
			}
			if count != tc.Count {
				t.Errorf("count=%d source=%d", count, tc.Count)
			}
			if next := random.Long(); next != tc.NextLong {
				t.Errorf("following RNG=%d source=%d", next, tc.NextLong)
			}
		})
	}
}
