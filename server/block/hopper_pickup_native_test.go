package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHopperPickupGeometryAndContainerFlagsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_hopper_pickup_geometry.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Position   cube.Pos
			Body       [6]float64
			Intersects bool
		}
		ContainerFlags []struct {
			Block     string
			Container bool
		} `json:"container_flags"`
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 648 || len(native.ContainerFlags) != 18 {
		t.Fatal("incomplete original hopper pickup capture")
	}
	for i, c := range native.Scenarios {
		b := c.Body
		body := cube.Box(b[0], b[1], b[2], b[3], b[4], b[5])
		if got := hopperPickupIntersects(hopperPickupBox(c.Position), body); got != c.Intersects {
			t.Errorf("case %d at %v: intersects=%t, native=%t", i, c.Position, got, c.Intersects)
		}
	}
	blocks := map[string]world.Block{
		"barrel": Barrel{}, "blast_furnace": BlastFurnace{}, "brewing_stand": BrewingStand{},
		"chest": Chest{}, "copper_chest": CopperChest{}, "dispenser": Dispenser{},
		"dropper": Dropper{}, "ender_chest": EnderChest{}, "furnace": Furnace{},
		"hopper": Hopper{}, "lectern": Lectern{}, "shulker_box": ShulkerBox{},
		"smoker": Smoker{}, "crafter": Crafter{}, "chiseled_bookshelf": ChiseledBookshelf{},
		"composter": Composter{}, "decorated_pot": DecoratedPot{}, "jukebox": Jukebox{},
	}
	for _, c := range native.ContainerFlags {
		b, ok := blocks[c.Block[len("minecraft:"):]]
		if !ok {
			t.Fatalf("unmapped native container flag %s", c.Block)
		}
		if got := hopperBlocksWorldPickup(b); got != c.Container {
			t.Errorf("%s container=%t, native=%t", c.Block, got, c.Container)
		}
	}
}
