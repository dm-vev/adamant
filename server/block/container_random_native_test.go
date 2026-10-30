package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
)

func TestContainerRandomSlotsMatchNativeLoops(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_container_random_slots.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Mask     uint16
			Words    []uint32
			Selected int
			Bounds   []uint32
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 2048 {
		t.Fatal("incomplete native slot-selection corpus")
	}
	inv := inventory.New(9, nil)
	for n, c := range native.Scenarios {
		inv.Clear()
		for slot := range 9 {
			if c.Mask&(1<<slot) != 0 {
				_ = inv.SetItem(slot, item.NewStack(item.Diamond{}, slot+1))
			}
		}
		used := 0
		got := randomContainerSlot(inv, func() uint32 {
			if used >= len(c.Bounds) || c.Bounds[used] != uint32(used+1) {
				t.Fatalf("case %d consumed an unexpected draw %d", n, used)
			}
			word := c.Words[used]
			used++
			return word
		})
		if got != c.Selected || used != len(c.Bounds) {
			t.Fatalf("case %d mask=%09b selected=%d draws=%d, native=%d/%d", n, c.Mask, got, used, c.Selected, len(c.Bounds))
		}
	}
}
