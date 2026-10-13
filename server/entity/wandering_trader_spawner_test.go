package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestWanderingTraderStateSurvivesSaving(t *testing.T) {
	conf := wanderingTraderConfig{
		Health: 13, healthSet: true, DespawnDelay: 4321,
		WanderTarget: cube.Pos{5, 70, -3}, HasWanderTarget: true, Home: mobHome{pos: cube.Pos{5, 70, -3}, radius: 16},
		Offers: []MerchantOffer{{Input: item.NewStack(item.Emerald{}, 3), Output: item.NewStack(item.Bucket{}, 1),
			Uses: 2, MaxUses: 6, XP: 1, RewardXP: true, PriceMultiplier: .05}},
		OffersGenerated: true,
	}
	data := &world.EntityData{Data: conf.New()}
	decoded := &world.EntityData{}
	WanderingTraderType.DecodeNBT(WanderingTraderType.EncodeNBT(data), decoded)
	a, b := data.Data.(*wanderingTraderBehaviour), decoded.Data.(*wanderingTraderBehaviour)
	if b.health.Health() != 13 || b.despawnDelay != 4321 || b.wanderTarget != a.wanderTarget || !b.hasWanderTarget || b.home != a.home {
		t.Fatalf("restored trader %+v, want %+v", b, a)
	}
	if !b.offersGenerated || len(b.offers) != 1 || b.offers[0].Uses != 2 || !b.offers[0].Output.Equal(a.offers[0].Output) {
		t.Fatalf("restored offers %+v, want %+v", b.offers, a.offers)
	}

	fresh := &world.EntityData{}
	WanderingTraderType.DecodeNBT(WanderingTraderType.EncodeNBT(&world.EntityData{Data: wanderingTraderConfig{}.New()}), fresh)
	if f := fresh.Data.(*wanderingTraderBehaviour); f.offersGenerated || f.hasWanderTarget || f.home.set() {
		t.Fatalf("a fresh trader restored with %+v", f)
	}
}

func TestMerchantOfferTiersSurviveSaving(t *testing.T) {
	offers := []MerchantOffer{{Input: item.NewStack(item.Emerald{}, 1), Output: item.NewStack(item.Bread{}, 6), MaxUses: 16, Tier: 3}}
	if got := readMerchantOffers(map[string]any{"Offers": writeMerchantOffers(offers)}); got[0].Tier != 3 {
		t.Fatalf("restored tier %d, want 3", got[0].Tier)
	}
}

// roll returns a random whose first roll out of 100 satisfies ok.
func roll(ok func(int32) bool) java26.Random {
	for seed := int64(0); ; seed++ {
		r := java26.NewLegacyRandom(seed)
		if ok(r.NextInt(100)) {
			return java26.NewLegacyRandom(seed)
		}
	}
}

func TestWanderingTraderChanceGrowsUntilATraderSpawns(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	s := &wanderingTraderSpawner{tickDelay: 1, data: &wanderingTraderData{SpawnDelay: 1200, SpawnChance: 25}}
	wanderingTraderSpawners.Store(w, s)
	day := func(random java26.Random) {
		s.tickDelay, s.data.SpawnDelay = 1, 1200
		<-w.Exec(func(tx *world.Tx) { wanderingTraderSpawnTick(tx, &random) })
	}
	for _, want := range []int{50, 75, 75} {
		day(roll(func(v int32) bool { return v > 75 }))
		if s.data.SpawnChance != want || s.data.SpawnDelay != 24000 {
			t.Fatalf("after a failed day: %+v, want chance %d and a new day", *s.data, want)
		}
	}
	// Without players the spawn attempt succeeds trivially.
	day(roll(func(v int32) bool { return v <= 75 }))
	if s.data.SpawnChance != 25 {
		t.Fatalf("chance %d after a spawn, want 25", s.data.SpawnChance)
	}

	s.tickDelay, s.data.SpawnDelay = 1, 1200
	if err := w.SetGameRule("spawn_wandering_traders", false); err != nil {
		t.Fatal(err)
	}
	random := java26.NewLegacyRandom(0)
	<-w.Exec(func(tx *world.Tx) { wanderingTraderSpawnTick(tx, &random) })
	if s.data.SpawnDelay != 1200 {
		t.Fatal("spawner counted down with spawn_wandering_traders off")
	}
}
