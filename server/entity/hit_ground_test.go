package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestMobLandingActivatesSculkSensor drops a pig next to a sculk sensor. As
// Entity.checkFallDamage, the landing is a hit_ground vibration the sensor
// picks up.
func TestMobLandingActivatesSculkSensor(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	loader := world.NewLoader(2, w, world.NopViewer{})
	t.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	sensor := cube.Pos{0, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0, 64, 0})
		loader.Load(tx, 25)
		for x := -6; x <= 6; x++ {
			for z := -6; z <= 6; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(sensor, block.NewSculkSensor(), nil)
	})
	for range 5 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 68, 0.5}}))
	})
	for range 60 {
		w.AdvanceTick()
		var active bool
		<-w.Exec(func(tx *world.Tx) {
			active = tx.Block(sensor).(block.SculkSensor).Phase != block.SculkSensorInactive
		})
		if active {
			return
		}
	}
	t.Fatal("sensor stayed inactive after a pig landed beside it")
}
