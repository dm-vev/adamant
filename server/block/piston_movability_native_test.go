package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// Default-state native component classifications are separate from world
// movement, directional Push rules, support survival and runtime actor gates.
func TestPistonMovabilityMatchesBDSComponents(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_piston_movability.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete bool
		Blocks   []struct{ Name, Movement, Sticky, ChangedTo string }
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Blocks) != 1477 {
		t.Fatal("incomplete native movability enumeration")
	}
	byName := make(map[string]world.Block)
	for _, b := range world.DefaultBlockRegistry.Blocks() {
		name, _ := b.EncodeBlock()
		if _, ok := byName[name]; !ok {
			byName[name] = b
		}
	}
	matched := 0
	for _, c := range native.Blocks {
		b, ok := byName[c.Name]
		if !ok || c.Movement == "" {
			continue
		}
		matched++
		t.Run(c.Name, func(t *testing.T) {
			if got := isPistonImmovable(b); got != (c.Movement == "Immovable") {
				t.Errorf("immovable=%t, native %s", got, c.Movement)
			}
			if got := breaksWhenMoved(b); got != (c.Movement == "Popped") {
				t.Errorf("popped=%t, native %s", got, c.Movement)
			}
			if got := sticksToPiston(b); got != (c.Sticky == "Same") {
				t.Errorf("sticky=%t, native %s", got, c.Sticky)
			}
		})
	}
	if matched < 1000 {
		t.Fatalf("only %d native component types have registered counterparts", matched)
	}
	t.Logf("compared %d registered native component types; unavailable/changed types remain explicit in fixture", matched)
}
