package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func farmer(t *testing.T, tx *world.Tx, xp int32) *entity.Villager {
	t.Helper()
	h, err := entity.NewFromStructureNBT(map[string]any{"id": "minecraft:villager", "Xp": xp,
		"VillagerData": map[string]any{"type": "minecraft:plains", "profession": "minecraft:farmer", "level": int32(1)}},
		world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 64, .5}, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	return tx.AddEntity(h).(*entity.Villager)
}

func TestVillagerTradesDropExperience(t *testing.T) {
	cases := []struct {
		name         string
		xp           int32
		minXP, maxXP int
	}{
		{name: "trade", minXP: 3, maxXP: 6},
		// Reaching apprentice adds five experience to the trade's.
		{name: "promotion", xp: 9, minXP: 8, maxXP: 11},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := traderTestWorld(t)
			<-w.Exec(func(tx *world.Tx) {
				v := farmer(t, tx, c.xp)
				p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
				offers, err := v.MerchantOffers()
				if err != nil || len(offers) == 0 {
					t.Fatalf("farmer has no offers: %v", err)
				}
				if ok, err := v.CompleteMerchantTrade(tx, p, 0); !ok || err != nil {
					t.Fatalf("trade rejected: %v", err)
				}
				if xp, _ := deathDrops(tx); xp < c.minXP || xp > c.maxXP {
					t.Fatalf("trade dropped %d experience, want %d-%d", xp, c.minXP, c.maxXP)
				}
			})
		})
	}
}

func TestVillagerTradeScreenShowsItsProfessionAndUnlockedTier(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		v := farmer(t, tx, 0)
		if v.MerchantName() != "entity.villager.farmer" {
			t.Errorf("trade screen title %q, want entity.villager.farmer", v.MerchantName())
		}
		offers, _ := v.MerchantOffers()
		for _, o := range offers {
			// Bedrock unlocks offers up to the villager's level minus one.
			if o.Tier != v.MerchantLevel()-1 {
				t.Errorf("novice offer has tier %d, want %d", o.Tier, v.MerchantLevel()-1)
			}
		}
	})
}
