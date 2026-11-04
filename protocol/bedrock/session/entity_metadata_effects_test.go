package session

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
)

// TestVisibleEffectsPackInIDOrder gives two pigs speed, regeneration and
// poison in opposite orders, and night vision without particles to one: BDS
// 1.26.61.1 shows both as 35366, speed, regeneration and poison from the
// highest bits down, and the same value on every update.
func TestVisibleEffectsPackInIDOrder(t *testing.T) {
	t.Parallel()
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	viewer := testSession("effects")
	viewer.br = world.DefaultBlockRegistry
	const minute = time.Minute
	orders := [][]effect.Effect{
		{effect.New(effect.Poison, 1, minute), effect.New(effect.Speed, 1, minute), effect.New(effect.Regeneration, 1, minute)},
		{effect.New(effect.Regeneration, 1, minute), effect.New(effect.Speed, 1, minute), effect.New(effect.Poison, 1, minute),
			effect.New(effect.NightVision, 1, minute).WithoutParticles()},
	}
	<-w.Exec(func(tx *world.Tx) {
		for i, order := range orders {
			pig := tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}})).(entity.Living)
			for _, e := range order {
				pig.AddEffect(e)
			}
			for range 50 {
				if got := viewer.parseEntityMetadata(pig)[mc.EntityDataKeyVisibleMobEffects]; got != int64(35366) {
					t.Fatalf("pig %d shows effects %v, want 35366", i, got)
				}
			}
		}
	})
	if got, want := packVisibleEffects([]effect.Effect{effect.NewAmbient(effect.Speed, 1, minute)}), int64(3); got != want {
		t.Errorf("ambient speed packs to %d, want %d", got, want)
	}
}
