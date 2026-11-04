package session_test

import "testing"

// menuRequestScenarios are the scenarios of item stack requests that
// TestGoldenMenuRequests ran through the handlers the session had and as
// batches of server/menu, and compared, while both existed. Each D commit
// of Phase 4 moved the handlers of one menu into server/menu and added the
// cases that reach them. The inventory, menus, refusals and multicraft
// scenarios, which it ran too, are golden tests of their own.
var menuRequestScenarios = []struct {
	name  string
	steps func(s *scenario)
}{
	// Move, Swap, Drop, Destroy, Touch and CreateCreative.
	{"moves", (*scenario).movesScenario},
	// Craft and CraftDynamic.
	{"crafts", (*scenario).craftsScenario},
	// MultiCraft at the crafting and cartography tables.
	{"special", (*scenario).specialScenario},
	// AutoCraft, the crafts of the recipe book.
	{"autocraft", (*scenario).autoCraftScenario},

	// The enchanting table, the anvil, the grindstone and the smithing
	// table.
	{"enchanting", (*scenario).enchantingCases},
	{"enchanting twice", (*scenario).enchantingTwiceCases},
	{"enchanting in creative", (*scenario).enchantingCreativeCases},
	{"anvil", (*scenario).anvilCases},
	{"grindstone", (*scenario).grindstoneCases},
	{"smithing", (*scenario).smithingCases},

	// The loom, the stonecutter, the beacon and merchants.
	{"loom", (*scenario).loomRequestsScenario},
	{"stonecutter", (*scenario).stonecutterRequestsScenario},
	{"beacon", (*scenario).beaconRequestsScenario},
	{"merchant", (*scenario).merchantRequestsScenario},
}

// TestMenuRequestScenarios runs the scenarios of item stack requests as the
// session handles them, in a process of its own as the golden tests run:
// with the handlers of the session gone, which judged them, every request a
// scenario expects accepted must be accepted and every one it expects
// refused refused, and nothing may panic. What the client is sent is not
// compared with a snapshot.
func TestMenuRequestScenarios(t *testing.T) {
	if isolated(t) {
		return
	}
	for _, sc := range menuRequestScenarios {
		t.Run(sc.name, func(t *testing.T) {
			sc.steps(newScenario(t, sc.name, scenarioConfig{}))
		})
	}
}
