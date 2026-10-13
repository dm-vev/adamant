package block

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestContainerLootFillMatchesJava263Source(t *testing.T) {
	type slot struct {
		Slot  int    `json:"slot"`
		Item  string `json:"item"`
		Count int    `json:"count"`
	}
	var fixture struct {
		Table string `json:"table"`
		Cases []struct {
			Name     string `json:"name"`
			Seed     int64  `json:"seed"`
			Occupied []slot `json:"occupied"`
			Slots    []slot `json:"slots"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/java263_container_loot_source.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			job := w.Do(func(tx *world.Tx) {
				inv := NewBarrel().inventory
				for _, s := range tc.Occupied {
					it, ok := world.ItemByName(s.Item, 0)
					if !ok {
						t.Fatalf("Source occupied item missing: %s", s.Item)
					}
					if err := inv.SetItem(s.Slot, item.NewStack(it, s.Count)); err != nil {
						t.Fatal(err)
					}
				}
				if err := fillContainerLoot(tx, cube.Pos{0, 64, 0}, inv, fixture.Table, tc.Seed, "minecraft:barrel"); err != nil {
					t.Fatal(err)
				}
				expected := make(map[int]slot, len(tc.Slots))
				for _, s := range tc.Slots {
					expected[s.Slot] = s
				}
				for index, stack := range inv.Slots() {
					want, exists := expected[index]
					if !exists {
						if !stack.Empty() {
							t.Errorf("unexpected stack at %d: %v", index, stack)
						}
						continue
					}
					if stack.Empty() {
						t.Errorf("missing Source stack at %d: %+v", index, want)
						continue
					}
					name, _ := stack.Item().EncodeItem()
					if name != want.Item || stack.Count() != want.Count {
						t.Errorf("slot%d=%s x%d Source=%s x%d", index, name, stack.Count(), want.Item, want.Count)
					}
				}
			})
			<-job.Done()
			if err := job.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
