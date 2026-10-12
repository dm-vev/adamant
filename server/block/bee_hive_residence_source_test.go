package block_test

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

func TestHiveResidenceMatchesJava263Source(t *testing.T) {
	data, err := os.ReadFile("testdata/java263_beehive_residence_source.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			Name              string `json:"name"`
			Ticks             int32  `json:"ticks"`
			Minimum           int32  `json:"minimum"`
			AfterFirstTick    int32  `json:"afterFirstTick"`
			FirstEligibleTick int    `json:"firstEligibleTick"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				pos := cube.Pos{8, 64, 8}
				exit := pos.Side(cube.FaceNorth)
				tx.SetBlock(exit, block.Stone{}, nil)
				java := map[string]any{"ticks_in_hive": tc.Ticks, "min_ticks_in_hive": tc.Minimum}
				hive := block.Beehive{Facing: cube.North, Occupants: []any{map[string]any{
					"ActorIdentifier": "minecraft:bee", "SaveData": map[string]any{"id": "minecraft:bee"}, "adamant:java": java,
				}}}
				tx.SetBlock(pos, hive, nil)
				random := rand.New(rand.NewPCG(17, 29))
				hive.ScheduledTick(pos, tx, random)
				current := tx.Block(pos).(block.Beehive)
				if len(current.Occupants) != 1 {
					t.Fatalf("blocked exit retained %d bees, want 1", len(current.Occupants))
				}
				saved := current.Occupants[0].(map[string]any)["adamant:java"].(map[string]any)
				if saved["ticks_in_hive"] != tc.AfterFirstTick || saved["min_ticks_in_hive"] != tc.Minimum {
					t.Fatalf("residence after first tick=%v Source={ticks:%d minimum:%d}", saved, tc.AfterFirstTick, tc.Minimum)
				}
				tx.SetBlock(exit, block.Air{}, nil)
				eligible := tc.FirstEligibleTick
				if eligible == 1 {
					eligible = 2
				}
				if tc.AfterFirstTick < 0 {
					eligible = 0
				}
				limit := max(eligible, 2)
				for tick := 2; tick <= limit; tick++ {
					tx.Block(pos).(block.Beehive).ScheduledTick(pos, tx, random)
					occupants := tx.Block(pos).(block.Beehive).Occupants
					want := 1
					if eligible != 0 && tick == eligible {
						want = 0
					}
					if len(occupants) != want {
						t.Fatalf("tick %d retained %d bees, Source=%d", tick, len(occupants), want)
					}
				}
				count := 0
				for e := range tx.Entities() {
					if e.H().Type().EncodeEntity() == "minecraft:bee" {
						count++
					}
				}
				want := 1
				if eligible == 0 {
					want = 0
				}
				if count != want {
					t.Errorf("released bees=%d Source=%d", count, want)
				}
			})
		})
	}
}
