package end

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

type endSourceGateway struct {
	Position cube.Pos `json:"position"`
	Exit     cube.Pos `json:"exit"`
	Exact    bool     `json:"exact"`
}

func assertEndSourceGateways(t *testing.T, result world.GenerationResult, expected []endSourceGateway) {
	t.Helper()
	if len(result.BlockEntities) != len(expected) {
		t.Fatalf("gateway actors=%d source=%d", len(result.BlockEntities), len(expected))
	}
	actors := make(map[cube.Pos]map[string]any, len(result.BlockEntities))
	for _, actor := range result.BlockEntities {
		actors[actor.Pos] = actor.Data
	}
	for _, source := range expected {
		actor := actors[source.Position]
		exit, ok := actor["ExitPortal"].(map[string]any)
		exact := uint8(0)
		if source.Exact {
			exact = 1
		}
		if actor["id"] != "EndGateway" || !ok || exit["X"] != int32(source.Exit[0]) || exit["Y"] != int32(source.Exit[1]) || exit["Z"] != int32(source.Exit[2]) || actor["ExactTeleport"] != exact {
			t.Errorf("gateway at %v: payload=%v source exit=%v exact=%v", source.Position, actor, source.Exit, source.Exact)
		}
	}
}

func TestEndGatewayWorldHeightMatchesJava263(t *testing.T) {
	world.DefaultBlockRegistry.Finalize()
	data, err := os.ReadFile("testdata/java263_gateway_bounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Y        int                `json:"y"`
		States   map[string]string  `json:"states"`
		Gateways []endSourceGateway `json:"gateways"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	g := NewEnd(0)
	for _, row := range rows {
		t.Run(fmt.Sprintf("y=%d", row.Y), func(t *testing.T) {
			c := chunk.New(world.DefaultBlockRegistry, world.End.Range())
			var scratch endScratch
			region := newEndFeatureRegion(g, 0, 0, c, &scratch)
			origin := endFeatureOrigin{region: region, chunkX: 0, chunkZ: 0}
			var result world.GenerationResult
			g.generateEndGatewayStructure(origin, 8, row.Y, 8, &result, 0, 0)
			for key, expected := range row.States {
				var pos cube.Pos
				if _, err := fmt.Sscanf(key, "%d,%d,%d", &pos[0], &pos[1], &pos[2]); err != nil {
					t.Fatal(err)
				}
				actual, _, ok := world.DefaultBlockRegistry.RuntimeIDToState(origin.block(pos[0], pos[1], pos[2]))
				if !ok || actual != expected {
					t.Errorf("at %v block=%q source=%q", pos, actual, expected)
				}
			}
			assertEndSourceGateways(t, result, row.Gateways)
		})
	}
}
