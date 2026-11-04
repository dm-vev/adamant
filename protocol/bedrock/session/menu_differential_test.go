package session_test

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// useMenuRequests has the scenarios of the process handle item stack
// requests as batches of server/menu (session.UseMenuRequests).
var useMenuRequests bool

// The environment variables a process run by TestGoldenMenuRequests reads:
// the path its requests take ("session" or "menu"), and the directory it
// writes what each scenario recorded to.
const (
	menuRequestsPath = "GOLDEN_MENU_REQUESTS"
	menuRequestsOut  = "GOLDEN_MENU_REQUESTS_OUT"
)

// menuRequestScenarios are the scenarios TestGoldenMenuRequests runs both
// ways: those of the golden tests that make item stack requests. Each D
// commit of Phase 4 moves the handlers of one menu into server/menu and
// adds the cases that reach them.
var menuRequestScenarios = []struct {
	name  string
	steps func(s *scenario)
}{
	{"inventory", (*scenario).inventoryScenario},
	{"menus", (*scenario).menusScenario},
	{"refusals", (*scenario).refusalsScenario},
	{"multicraft", (*scenario).multiCraftScenario},

	// D6–D9: the enchanting table, the anvil, the grindstone and the
	// smithing table.
	{"enchanting", (*scenario).enchantingCases},
	{"enchanting twice", (*scenario).enchantingTwiceCases},
	{"enchanting in creative", (*scenario).enchantingCreativeCases},
	// D2: Move, Swap, Drop, Destroy, Touch and CreateCreative.
	{"moves", (*scenario).movesScenario},
	// D3: Craft and CraftDynamic.
	{"crafts", (*scenario).craftsScenario},
	// D4: MultiCraft at the crafting and cartography tables.
	{"special", (*scenario).specialScenario},
	// D5: AutoCraft, the crafts of the recipe book.
	{"autocraft", (*scenario).autoCraftScenario},

	// The stations of D10 to D13: the loom, the stonecutter, the beacon
	// and merchants.
	{"loom", (*scenario).loomRequestsScenario},
	{"stonecutter", (*scenario).stonecutterRequestsScenario},
	{"beacon", (*scenario).beaconRequestsScenario},
	{"merchant", (*scenario).merchantRequestsScenario},
}

// TestGoldenMenuRequests runs the scenarios of item stack requests twice,
// each time in a process of its own as the golden tests run: once as the
// session handles the requests, and once as batches of server/menu. The
// batch takes the place of the undo log, the pending results and the
// comparator updates of the session, and every slot the session sets goes
// through it; the operations that moved into the menu run there. What the
// client is sent, byte for byte as the snapshots record it, and the state
// left must be the same: every slot of the player, the experience level,
// the menu open and the window the session shows, the blocks around the
// menus with what their containers hold, and the entities of the world.
// The snapshots themselves judge the path of the session.
func TestGoldenMenuRequests(t *testing.T) {
	if path := os.Getenv(menuRequestsPath); path != "" {
		runMenuRequests(t, path, os.Getenv(menuRequestsOut))
		return
	}
	dir := t.TempDir()
	for _, path := range []string{"session", "menu"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestGoldenMenuRequests$", "-test.count=1", "-test.timeout=5m")
		cmd.Env = append(os.Environ(), menuRequestsPath+"="+path, menuRequestsOut+"="+filepath.Join(dir, path))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the scenarios through the %s in a process of their own: %v\n%s", path, err, out)
		}
	}
	for _, sc := range menuRequestScenarios {
		want, err := os.ReadFile(filepath.Join(dir, "session", sc.name))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join(dir, "menu", sc.name))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: the requests through server/menu differ from those of the session:\n%s", sc.name, snapshotDiff(string(want), string(got)))
		}
	}
}

// runMenuRequests runs the scenarios through the path passed and writes
// what each recorded, and the state it left, to a file of dir.
func runMenuRequests(t *testing.T, path, dir string) {
	useMenuRequests = path == "menu"
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, sc := range menuRequestScenarios {
		t.Run(sc.name, func(t *testing.T) {
			s := newScenario(t, sc.name, scenarioConfig{})
			sc.steps(s)
			var state string
			s.run(func(tx *world.Tx, p *player.Player) { state = s.state(tx, p) })
			if err := os.WriteFile(filepath.Join(dir, sc.name), []byte(s.out.String()+state), 0o644); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// stateArea is the part of the world of a scenario whose blocks the state of
// TestGoldenMenuRequests holds: the menus of the scenarios are placed in it.
var stateArea = cube.Box(-8, -60, -8, 8, -57, 8)

// state returns the state a scenario left, as TestGoldenMenuRequests
// compares it: every slot of the player, its experience level, the menu
// open against the window the session shows, the blocks of stateArea with
// what their containers hold, and the entities of the world, but for the
// velocity of experience orbs, which they are spawned with at random.
func (s *scenario) state(tx *world.Tx, p *player.Player) string {
	var b strings.Builder
	b.WriteString("== the state at the end\n")
	inv, offHand, enderChest, ui, armour := s.sess.Inventories()
	for _, i := range []struct {
		name string
		inv  *inventory.Inventory
	}{{"inventory", inv}, {"off hand", offHand}, {"ender chest", enderChest}, {"ui", ui}, {"armour", armour.Inventory()}} {
		writeSlots(&b, i.name, i.inv)
	}
	fmt.Fprintf(&b, "experience level %d, game mode %T\n", p.ExperienceLevel(), p.GameMode())
	m, id, containerType := s.sess.Window()
	if pm := p.Menu(); pm != m {
		fmt.Fprintf(&b, "the player has %p open, the session shows %p\n", pm, m)
	}
	if m != nil {
		fmt.Fprintf(&b, "menu %v at %v, entity %v\n", m.Kind(), m.Pos(), m.Entity() != nil)
	}
	fmt.Fprintf(&b, "window %d of type %d\n", id, containerType)

	min, max := stateArea.Min(), stateArea.Max()
	for x := int(min[0]); x <= int(max[0]); x++ {
		for y := int(min[1]); y <= int(max[1]); y++ {
			for z := int(min[2]); z <= int(max[2]); z++ {
				pos := cube.Pos{x, y, z}
				bl := tx.Block(pos)
				if _, air := bl.(block.Air); air {
					continue
				}
				name, props := bl.EncodeBlock()
				fmt.Fprintf(&b, "block %v %s %v\n", pos, name, props)
				if c, ok := bl.(block.Container); ok {
					writeSlots(&b, "  holds", c.Inventory(tx, pos))
				}
			}
		}
	}
	var entities []string
	for e := range tx.Entities() {
		line := fmt.Sprintf("entity %s at %.3f", e.H().Type().EncodeEntity(), e.Position())
		if orb, ok := e.(interface{ Experience() int }); ok {
			line += fmt.Sprintf(", %d experience", orb.Experience())
		}
		if it, ok := e.(interface{ Item() item.Stack }); ok {
			line += " " + stackState(it.Item())
		}
		entities = append(entities, line)
	}
	slices.SortFunc(entities, cmp.Compare)
	for _, e := range entities {
		b.WriteString(e + "\n")
	}
	return b.String()
}

// writeSlots writes the stacks of the slots of inv that hold any.
func writeSlots(b *strings.Builder, name string, inv *inventory.Inventory) {
	for slot, it := range inv.Slots() {
		if !it.Empty() {
			fmt.Fprintf(b, "%s %d: %s\n", name, slot, stackState(it))
		}
	}
}

// stackState returns a stack as the state holds it: its count and its NBT.
func stackState(it item.Stack) string {
	return fmt.Sprintf("%d %v", it.Count(), item.WriteNBT(it))
}
