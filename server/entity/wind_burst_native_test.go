package entity

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestWindBurstRaysMatchExecutedClient(t *testing.T) {
	testWindBurstRaysNative(t, "client_1_26_51_wind_bursts.json", 64)
}

func TestBreezeWindBurstRaysMatchExecutedClient(t *testing.T) {
	testWindBurstRaysNative(t, "client_1_26_51_breeze_wind_bursts.json", 16)
}

func TestBreezeWindBurstInputScopesMatchNativeEntry(t *testing.T) {
	testWindBurstInputScopesNative(t, "bedrock_1_26_51_breeze_switch_grids_hardware.json", 16, 3)
}

func TestWindBurstInputScopesMatchNativeEntry(t *testing.T) {
	testWindBurstInputScopesNative(t, "bedrock_1_26_51_wind_switch_grids_hardware.json", 8, 1.2)
}

func testWindBurstInputScopesNative(t *testing.T, file string, count int, power float32) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Entry     string `json:"hardware_explosion_rva"`
		Scenarios []struct {
			RandomInput struct{ Words []uint32 } `json:"random_input"`
			Explosion   struct {
				Radius float32
				Words  []uint32
				Flags  []uint8
			}
		}
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || native.Entry != "0x795690" || len(native.Scenarios) != count {
		t.Fatal("incomplete native breeze explosion entry capture")
	}
	for _, c := range native.Scenarios {
		if c.Explosion.Radius != power || len(c.Explosion.Words) != 1352 || len(c.RandomInput.Words) < 1432 || len(c.Explosion.Flags) != 9 || c.Explosion.Flags[2] != 1 || c.Explosion.Flags[3] != 1 || !slices.Equal(c.Explosion.Words, c.RandomInput.Words[80:1432]) {
			t.Fatal("scoped breeze ray inputs do not match the native entry and validated level stream")
		}
	}
}

func testWindBurstRaysNative(t *testing.T, file string, count int) {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Cases    []struct {
			Position []float64
			Radius   float32
			Cells    []cube.Pos
			Words    []uint32
			Order    []cube.Pos `json:"affected_order"`
			Matches  bool       `json:"matches_bds"`
		}
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Cases) != count {
		t.Fatal("incomplete executed client wind ray corpus")
	}
	for i, c := range native.Cases {
		if len(c.Position) != 3 || len(c.Words) != 1352 || !c.Matches {
			t.Fatal("invalid executed wind ray controls")
		}
		cells := map[cube.Pos]bool{}
		for _, p := range c.Cells {
			cells[p] = true
		}
		used := 0
		got := windBurstAffectedBlocks([3]float64(c.Position), c.Radius, func(p cube.Pos) bool { return cells[p] }, func() float32 { word := c.Words[used]; used++; return float32(float64(word) / 4294967296) })
		if used != 1352 || !slices.Equal(got, c.Order) {
			t.Errorf("case %d: affected=%v, native=%v, draws=%d", i, got, c.Order, used)
		}
	}
}
