package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestTNTWireStatesMatchNativePackets(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_tnt_states.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Accepted bool
			API      struct {
				ID     string
				States map[string]any
			} `json:"block"`
			Wire struct {
				ID     string
				States map[string]any
			} `json:"wire_block"`
			NetworkHash uint32 `json:"network_hash"`
			Updates     []struct{ NewBlockRuntimeID uint32 }
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 10 {
		t.Fatal("incomplete native TNT state/packet corpus")
	}
	world.DefaultBlockRegistry.Finalize()
	packets := 0
	for i, c := range native.Scenarios {
		if c.Accepted != (i != 4 && i != 5) {
			t.Fatal("native TNT command/legacy structure controls differ")
		}
		if !c.Accepted {
			continue
		}
		underwater := c.API.ID == "minecraft:underwater_tnt"
		if c.API.ID != c.Wire.ID || c.API.States["allow_underwater_bit"] != underwater || len(c.API.States) != 2 || len(c.Wire.States) != 1 || c.API.States["explode_bit"] != c.Wire.States["explode_bit"] {
			t.Fatal("native TNT SDK/wire alias controls differ")
		}
		rid, ok := world.DefaultBlockRegistry.StateToRuntimeID(c.Wire.ID, c.Wire.States)
		if !ok {
			t.Fatal("native TNT wire permutation missing")
		}
		hash, ok := world.DefaultBlockRegistry.RuntimeIDToHash(rid)
		if !ok || hash != c.NetworkHash {
			t.Fatalf("scene %d network hash=%d, native=%d", i, hash, c.NetworkHash)
		}
		if len(c.Updates) == 0 {
			t.Fatal("missing native TNT wire update")
		}
		for _, p := range c.Updates {
			if p.NewBlockRuntimeID != hash {
				t.Fatal("native TNT update differs")
			}
			packets++
		}
	}
	if packets != 12 {
		t.Fatal("incomplete native TNT packet controls")
	}
}
