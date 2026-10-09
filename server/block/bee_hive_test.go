package block_test

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

type hiveExitObserver struct {
	world.NopHandler
	tx                 *world.Tx
	exits, beesAtSound int
}

func (h *hiveExitObserver) HandleSound(_ *world.Context, s world.Sound, _ mgl64.Vec3) {
	if event, ok := s.(sound.NativeEvent); ok && event.SoundType == "block.beehive.exit" {
		h.exits++
		for e := range h.tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:bee" {
				h.beesAtSound++
			}
		}
	}
}

func TestHiveEmergencyRelease(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		name := "open exit"
		if blocked {
			name = "blocked exit"
		}
		t.Run(name, func(t *testing.T) {
			w := world.Config{Synchronous: true, Generator: world.NopGenerator{}, Entities: entity.DefaultRegistry, RandomTickSpeed: -1}.New()
			defer w.Close()
			observer := &hiveExitObserver{}
			w.Handle(observer)
			<-w.Exec(func(tx *world.Tx) {
				observer.tx = tx
				pos := cube.Pos{8, 64, 8}
				sensor := pos.Side(cube.FaceEast).Side(cube.FaceEast)
				tx.SetBlock(sensor, block.SculkSensor{}, nil)
				hive := block.Beehive{Facing: cube.North, Occupants: []any{map[string]any{"ActorIdentifier": "minecraft:bee", "SaveData": map[string]any{}}}}
				tx.SetBlock(pos, hive, nil)
				if blocked {
					tx.SetBlock(pos.Side(cube.FaceNorth), block.Stone{}, nil)
				}
				hive.JavaEmergencyRelease(pos, tx)
				count := 0
				var bee world.Entity
				for e := range tx.Entities() {
					if e.H().Type().EncodeEntity() == "minecraft:bee" {
						bee = e
						count++
					}
				}
				if count != 1 {
					t.Errorf("released bees = %d, want 1", count)
					return
				}
				// Java 26.3 EntityTypes.BEE is 0.55F wide and 0.5F high. BeehiveBlockEntity
				// subtracts half-height and adds 0.55 + half-width only for an open exit.
				want := pos.Vec3Centre()
				want[1] -= float64(float32(.5) / 2)
				if !blocked {
					want[2] -= .55 + float64(float32(.55)/2)
				}
				for axis := range 3 {
					if math.Abs(bee.Position()[axis]-want[axis]) > 1e-12 {
						t.Errorf("release position = %v, want %v", bee.Position(), want)
						break
					}
				}
				if observer.exits != 1 || observer.beesAtSound != 0 {
					t.Errorf("exit sounds = %d, bees already inserted at sound = %d", observer.exits, observer.beesAtSound)
				}
				data := tx.Block(sensor).(world.VibrationListener).VibrationData()
				if data == nil || !data.Selector.HasCandidate || data.Selector.Candidate.Event != world.GameEventBlockChange || data.Selector.Candidate.SourceID != bee.H().UUID() {
					t.Error("release did not emit a block-change vibration attributed to the bee")
				}
				if occupants := tx.Block(pos).(block.Beehive).Occupants; len(occupants) != 0 {
					t.Errorf("released bee remains in hive: %v", occupants)
				}
			})
		})
	}
}
