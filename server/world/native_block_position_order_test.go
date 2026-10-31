package world

import (
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

func TestNativeBlockPositionOrderMatchesClientSet(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/client_1_26_51_wind_bursts.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Cases    []struct {
			Input []cube.Pos `json:"insertion_visits"`
			Order []cube.Pos `json:"affected_order"`
		}
		Controls []struct{ Input, Order []cube.Pos } `json:"order_controls"`
	}
	if err = json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Cases) != 64 || len(native.Controls) != 9 {
		t.Fatal("incomplete executed client affected-set corpus")
	}
	for i, c := range native.Cases {
		if got := NativeBlockPositionOrder(c.Input); !slices.Equal(got, c.Order) {
			t.Errorf("affected set %d differs", i)
		}
	}
	for i, c := range native.Controls {
		if got := NativeBlockPositionOrder(c.Input); !slices.Equal(got, c.Order) {
			t.Errorf("bucket boundary %d differs", i)
		}
	}
}
