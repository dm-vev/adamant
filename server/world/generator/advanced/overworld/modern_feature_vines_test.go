package overworld

import (
	"encoding/json"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"os"
	"testing"
)

func TestVinesMatchJava263(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_vines.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name   string
		Placed bool
		Next   int64
		Result any
		Blocks []struct {
			Position [3]int32
			State    any
		}
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			region := countLayerTestRegion()
			origin := featurePos{8, 10, 8}
			for _, input := range tc.Blocks {
				p := featurePos{origin.x + input.Position[0], origin.y + input.Position[1], origin.z + input.Position[2]}
				block, ok := featureStateBlock(input.State)
				if !ok {
					t.Fatal("invalid fixture block")
				}
				state, err := block.ResolveNative(region.c.BlockRegistry())
				if err != nil {
					t.Fatal(err)
				}
				region.SetNativeBlock(p, state, block.NativeBlock)
			}
			expected, ok := featureStateBlock(tc.Result)
			if !ok {
				t.Fatal("invalid source result")
			}
			want, err := expected.ResolveNative(region.c.BlockRegistry())
			if err != nil {
				t.Fatal(err)
			}
			random := java26.NewLegacyRandom(123)
			got := region.f.placeConfiguredData(region, "vines", nil, origin, &random)
			if got != tc.Placed {
				t.Errorf("placed=%v, Java wants %v", got, tc.Placed)
			}
			if state := region.nativeState(origin); state != want {
				t.Errorf("native vine attachment=%+v, source projection=%+v", state, want)
			}
			if next := random.NextLong(); next != tc.Next {
				t.Errorf("following stream=%d, Java wants %d", next, tc.Next)
			}
		})
	}
}

func TestLeavesAttachVinesButDoNotSupportPiles(t *testing.T) {
	region := countLayerTestRegion()
	p := featurePos{8, 10, 8}
	block, _ := featureStateBlock("minecraft:oak_leaves")
	state, err := block.ResolveNative(region.c.BlockRegistry())
	if err != nil {
		t.Fatal(err)
	}
	region.Set(p, state)
	if featurePredicate(region, p, map[string]any{"type": "has_sturdy_face", "direction": "up"}) {
		t.Fatal("Java leaves do not provide a sturdy upper face")
	}
	random := java26.NewLegacyRandom(123)
	if !placeModernVines(region, featurePos{8, 10, 9}) {
		t.Fatal("Java vine attaches to leaf collision face")
	}
	blockPileTryPlace(region, featurePos{8, 11, 8}, map[string]any{"id": "minecraft:gold_block"}, &random)
	if !featureRegionAir(region, featurePos{8, 11, 8}) {
		t.Fatal("pile placed on non-sturdy leaves")
	}
}
