package entity

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func newPushBenchWorld(b *testing.B, maxCramming int) *world.World {
	b.Helper()
	w := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Entities:        DefaultRegistry,
	}.New()
	b.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().SetGameRule("max_entity_cramming", maxCramming); err != nil {
			b.Fatal(err)
		}
	})
	return w
}

func BenchmarkTickEntityCollisions(b *testing.B) {
	counts := []int{100, 500, 1000, 2000}

	for _, n := range counts {
		b.Run(fmt.Sprintf("Spread/%d", n), func(b *testing.B) {
			w := newPushBenchWorld(b, 24)
			<-w.Exec(func(tx *world.Tx) {
				side := 1
				for side*side < n {
					side++
				}
				for i := range n {
					x := float64(i%side) * 6.0
					z := float64(i/side) * 6.0
					tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}}))
				}
				// Warm up one call so pool/chunk state is in steady state.
				tickEntityCollisions(tx)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					tickEntityCollisions(tx)
				}
				b.StopTimer()
			})
		})
	}

	for _, n := range counts {
		b.Run(fmt.Sprintf("TownCrowd/%d", n), func(b *testing.B) {
			w := newPushBenchWorld(b, 24)
			<-w.Exec(func(tx *world.Tx) {
				const clusterSize = 10
				clusters := (n + clusterSize - 1) / clusterSize
				side := 1
				for side*side < clusters {
					side++
				}
				for i := range n {
					c := i / clusterSize
					k := i % clusterSize
					cx := float64(c%side) * 16.0
					cz := float64(c/side) * 16.0
					// Pack mobs within ~0.4 blocks so all mobs in each cluster overlap.
					ox := float64(k%4) * 0.12
					oz := float64(k/4) * 0.12
					tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{cx + 8 + ox, 64, cz + 8 + oz}}))
				}
				tickEntityCollisions(tx)
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					tickEntityCollisions(tx)
				}
				b.StopTimer()
			})
		})
	}

	b.Run("Cramming/30", func(b *testing.B) {
		w := newPushBenchWorld(b, 24)
		<-w.Exec(func(tx *world.Tx) {
			cows := make([]*Cow, 0, 30)
			for i := range 30 {
				c := tx.AddEntity(NewCow(world.EntitySpawnOpts{
					Position: mgl64.Vec3{float64(i%6) * 0.05, 64, float64(i/6) * 0.05},
				})).(*Cow)
				c.SetMaxHealth(1e9)
				c.Heal(1e9, nil)
				cows = append(cows, c)
			}
			tickEntityCollisions(tx)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tickEntityCollisions(tx)
			}
			b.StopTimer()
			_ = cows
		})
	})

	b.Run("Mixed/1000", func(b *testing.B) {
		w := newPushBenchWorld(b, 24)
		<-w.Exec(func(tx *world.Tx) {
			for i := range 500 {
				x := float64(i%25) * 6.0
				z := float64(i/25) * 6.0
				tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{x, 64, z}}))
			}
			for i := range 300 {
				x := float64(i%25) * 6.0
				z := float64(i/25) * 6.0
				tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{x + 1, 64, z}}, item.NewStack(item.Stick{}, 1)))
			}
			for i := range 100 {
				x := float64(i%10) * 16.0
				z := float64(i/10) * 16.0
				tx.AddEntity(NewBoat(world.EntitySpawnOpts{Position: mgl64.Vec3{x + 2, 64, z}}, 0))
			}
			for i := range 100 {
				x := float64(i%10) * 16.0
				z := float64(i/10) * 16.0
				tx.AddEntity(NewArmourStand(world.EntitySpawnOpts{Position: mgl64.Vec3{x + 4, 64, z}}))
			}
			tickEntityCollisions(tx)
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				tickEntityCollisions(tx)
			}
			b.StopTimer()
		})
	})
}
