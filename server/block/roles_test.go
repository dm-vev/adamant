package block

import (
	"fmt"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// TestRoleClassesOfBlocks checks IsSwitch and IsContainer over every block
// of the registry: the doors, trapdoors, fence gates, buttons and levers
// are switches and nothing else is; every block that holds items, and the
// work blocks that open a window, are containers.
func TestRoleClassesOfBlocks(t *testing.T) {
	switches, containers := map[string]bool{}, map[string]bool{}
	for _, b := range world.Blocks() {
		name := fmt.Sprintf("%T", b)
		name = name[strings.LastIndex(name, ".")+1:]
		switchName := strings.HasSuffix(name, "Door") || strings.HasSuffix(name, "Trapdoor") ||
			strings.HasSuffix(name, "FenceGate") || name == "Button" || name == "Lever"
		if IsSwitch(b) != switchName {
			t.Errorf("%s: IsSwitch %v", name, IsSwitch(b))
		}
		if _, ok := b.(Container); ok && !IsContainer(b) {
			t.Errorf("%s holds items but is no container", name)
		}
		if IsSwitch(b) && IsContainer(b) {
			t.Errorf("%s is both a switch and a container", name)
		}
		if IsSwitch(b) {
			switches[name] = true
		}
		if IsContainer(b) {
			containers[name] = true
		}
	}
	for _, name := range []string{"WoodDoor", "IronDoor", "CopperDoor", "WoodTrapdoor", "IronTrapdoor", "CopperTrapdoor", "WoodFenceGate", "Button", "Lever"} {
		if !switches[name] {
			t.Errorf("%s is not a switch", name)
		}
	}
	for _, name := range []string{"Chest", "Barrel", "Furnace", "BlastFurnace", "Smoker", "Hopper", "Dropper", "Dispenser", "ShulkerBox", "BrewingStand", "EnderChest", "CraftingTable", "Anvil"} {
		if !containers[name] {
			t.Errorf("%s is not a container", name)
		}
	}
	for _, name := range []string{"Stone", "Dirt", "NoteBlock", "Bed"} {
		if containers[name] || switches[name] {
			t.Errorf("%s is a switch or a container", name)
		}
	}
}
