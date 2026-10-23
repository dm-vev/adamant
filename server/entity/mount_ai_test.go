package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func mountOf(tx *world.Tx, h *world.EntityHandle) interface {
	world.Entity
	Tamed() bool
	Temper() int
	InLove() bool
	Baby() bool
	Health() float64
	MaxHealth() float64
	Speed() float64
	JumpStrength() float64
} {
	e, _ := h.Entity(tx)
	return e.(interface {
		world.Entity
		Tamed() bool
		Temper() int
		InLove() bool
		Baby() bool
		Health() float64
		MaxHealth() float64
		Speed() float64
		JumpStrength() float64
	})
}

// rideHorse mounts a survival player on the horse and returns the player.
func rideHorse(t *testing.T, w *world.World, h *world.EntityHandle) *world.EntityHandle {
	t.Helper()
	var rider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		e, _ := h.Entity(tx)
		if !e.(interface {
			Interact(*world.Tx, item.User, *item.UseContext) bool
		}).Interact(tx, p, &item.UseContext{}) {
			t.Fatal("player could not get on the untamed horse")
		}
		rider = p.H()
	})
	return rider
}

func riding(tx *world.Tx, rider *world.EntityHandle) bool {
	e, _ := rider.Entity(tx)
	return e.(entity.Rider).Riding() != nil
}

// TestHorsesAreTamedByRiding checks RunAroundLikeCrazyGoal: an untamed horse
// bucks its rider off and grows more tempered, until its temper makes it
// accept the rider as its owner.
func TestHorsesAreTamedByRiding(t *testing.T) {
	w := traderTestWorld(t)
	var calm, wild *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		wild = spawnAnimal(t, tx, "minecraft:horse", .5, nil)
		calm = spawnAnimal(t, tx, "minecraft:horse", 10.5, map[string]any{"Temper": int32(100)})
	})
	wildRider, calmRider := rideHorse(t, w, wild), rideHorse(t, w, calm)
	bucked, tamed := false, false
	for range 600 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			bucked = bucked || !riding(tx, wildRider)
			tamed = mountOf(tx, calm).Tamed()
		})
		if bucked && tamed {
			break
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		if !bucked {
			t.Fatal("untamed horse with no temper never bucked its rider off")
		}
		if h := mountOf(tx, wild); h.Tamed() || h.Temper() != 5 {
			t.Fatalf("bucking horse tamed=%v temper=%d, want untamed with a temper of 5", h.Tamed(), h.Temper())
		}
		if h := mountOf(tx, calm); !h.Tamed() || !riding(tx, calmRider) {
			t.Fatalf("horse at full temper tamed=%v, rider riding=%v, want it tamed with its rider on", h.Tamed(), riding(tx, calmRider))
		}
	})
}

// TestFeedingCalmsUntamedHorses checks AbstractHorse.handleEating: sugar
// raises the temper of an untamed horse.
func TestFeedingCalmsUntamedHorses(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		h := spawnAnimal(t, tx, "minecraft:horse", .5, nil)
		p := addSurvivalPlayer(tx, mgl64.Vec3{2.5, 64, .5})
		p.SetHeldItems(item.NewStack(item.Sugar{}, 2), item.Stack{})
		horse := mountOf(tx, h)
		horse.(interface {
			Interact(*world.Tx, item.User, *item.UseContext) bool
		}).Interact(tx, p, &item.UseContext{})
		if horse.Temper() != 3 {
			t.Fatalf("temper after sugar = %d, want 3", horse.Temper())
		}
	})
}

// TestTamedHorsesBreed checks the breeding of horses: tamed adults at full
// health fed golden carrots fall in love and have an untamed foal, and a
// horse and a donkey have a mule.
func TestTamedHorsesBreed(t *testing.T) {
	cases := []struct{ mother, father, child string }{
		{"minecraft:horse", "minecraft:horse", "minecraft:horse"},
		{"minecraft:horse", "minecraft:donkey", "minecraft:mule"},
	}
	for _, c := range cases {
		t.Run(c.child, func(t *testing.T) {
			w := traderTestWorld(t)
			var parents []*world.EntityHandle
			<-w.Exec(func(tx *world.Tx) {
				p := addSurvivalPlayer(tx, mgl64.Vec3{.5, 64, 5.5})
				for i, id := range []string{c.mother, c.father} {
					h := spawnAnimal(t, tx, id, float64(i*2)+.5, map[string]any{"Tame": byte(1)})
					p.SetHeldItems(item.NewStack(item.GoldenCarrot{}, 1), item.Stack{})
					e, _ := h.Entity(tx)
					e.(interface {
						Interact(*world.Tx, item.User, *item.UseContext) bool
					}).Interact(tx, p, &item.UseContext{})
					if !mountOf(tx, h).InLove() {
						t.Fatalf("tamed %s fed a golden carrot is not in love", id)
					}
					parents = append(parents, h)
				}
			})
			for range 200 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				var foal *world.EntityHandle
				for e := range tx.Entities() {
					if e.H().Type().EncodeEntity() == c.child && e.H() != parents[0] && e.H() != parents[1] {
						foal = e.H()
					}
				}
				if foal == nil {
					t.Fatalf("%s and %s in love had no %s", c.mother, c.father, c.child)
				}
				f := mountOf(tx, foal)
				if !f.Baby() || f.Tamed() {
					t.Fatalf("foal baby=%v tamed=%v, want an untamed baby", f.Baby(), f.Tamed())
				}
				if f.MaxHealth() < 15 || f.MaxHealth() > 30 || f.Speed() < .1125 || f.Speed() > .3375 || f.JumpStrength() < .4 || f.JumpStrength() > 1 {
					t.Fatalf("foal health=%.2f speed=%.3f jump=%.3f, want them within the horse ranges", f.MaxHealth(), f.Speed(), f.JumpStrength())
				}
			})
		})
	}
}

// TestHorsesFinalizeRandomAttributes checks Horse.randomizeAttributes: a
// spawned horse gets 15 to 30 health, and a speed and jump strength within
// the horse ranges, which differ between horses.
func TestHorsesFinalizeRandomAttributes(t *testing.T) {
	w := traderTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		speeds := map[float64]bool{}
		for i := range 8 {
			h := tx.AddEntity(entity.NewHorse(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i*3) - 10, 64, .5}, SpawnReason: "spawn_egg", SpawnSeed: int64(i + 1)})).H()
			horse := mountOf(tx, h)
			if horse.MaxHealth() < 15 || horse.MaxHealth() > 30 || horse.Health() != horse.MaxHealth() {
				t.Fatalf("spawned horse health %.1f/%.1f, want full health between 15 and 30", horse.Health(), horse.MaxHealth())
			}
			if horse.Speed() < .1125 || horse.Speed() > .3375 || horse.JumpStrength() < .4 || horse.JumpStrength() > 1 {
				t.Fatalf("spawned horse speed=%.3f jump=%.3f, want them within the horse ranges", horse.Speed(), horse.JumpStrength())
			}
			speeds[horse.Speed()] = true
		}
		if len(speeds) < 2 {
			t.Fatal("eight spawned horses all have the same speed")
		}
	})
}

// TestRiddenHorsesRunAtJavaSpeed checks AbstractHorse.travelRidden: a horse
// with the default movement speed of 0.225 that its rider steers forward
// covers 0.225/(1-0.546) blocks per tick, about ten blocks a second.
func TestRiddenHorsesRunAtJavaSpeed(t *testing.T) {
	w := traderTestWorld(t)
	var h, rider *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		h = spawnAnimal(t, tx, "minecraft:horse", .5, map[string]any{"Tame": byte(1), "Saddled": byte(1), "Health": float32(53)})
	})
	rider = rideHorse(t, w, h)
	steer := func() {
		<-w.Exec(func(tx *world.Tx) {
			e, _ := h.Entity(tx)
			p, _ := rider.Entity(tx)
			e.(entity.VehicleInputWithRider).SetVehicleInputFrom(p, 0, 1)
		})
	}
	var start mgl64.Vec3
	for range 20 {
		steer()
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) { start = entityPos(tx, h) })
	for range 20 {
		steer()
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		want := 20 * .225 / (1 - .6*.91)
		if d := horizontalDistance(entityPos(tx, h), start); d < want*.9 || d > want*1.1 {
			t.Fatalf("ridden horse covered %.2f blocks in a second, want %.2f", d, want)
		}
	})
}

// TestRiddenHorsesStepUpBlocks checks LivingEntity.getMaxUpStep and the
// step height of horses: a horse its rider steers into a block one high
// walks up onto it without jumping.
func TestRiddenHorsesStepUpBlocks(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := 4; x <= 12; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		h = spawnAnimal(t, tx, "minecraft:horse", .5, map[string]any{"Tame": byte(1), "Saddled": byte(1)})
	})
	rider := rideHorse(t, w, h)
	<-w.Exec(func(tx *world.Tx) {
		p, _ := rider.Entity(tx)
		// A yaw of -90 looks towards positive x.
		p.(*player.Player).Move(mgl64.Vec3{}, -90-p.Rotation().Yaw(), -p.Rotation().Pitch())
	})
	stepped := false
	for range 60 {
		<-w.Exec(func(tx *world.Tx) {
			e, _ := h.Entity(tx)
			p, _ := rider.Entity(tx)
			e.(entity.VehicleInputWithRider).SetVehicleInputFrom(p, 0, 1)
			if pos := entityPos(tx, h); pos[0] > 6 && pos[0] < 11 && pos[1] > 64.99 {
				stepped = true
			}
		})
		w.AdvanceTick()
	}
	if !stepped {
		t.Fatal("ridden horse did not walk up onto a step one block high")
	}
}

// TestHorsesPathAroundWallsToGoldenCarrots checks the TemptGoal of horses
// runs on their pathfinding: a horse walks around a wall to a player holding
// a golden carrot.
func TestHorsesPathAroundWallsToGoldenCarrots(t *testing.T) {
	w := traderTestWorld(t)
	var h *world.EntityHandle
	playerPos := mgl64.Vec3{8.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		for z := -3; z <= 3; z++ {
			for y := 64; y <= 66; y++ {
				tx.SetBlock(cube.Pos{4, y, z}, block.Stone{}, nil)
			}
		}
		h = spawnAnimal(t, tx, "minecraft:horse", .5, nil)
		addSurvivalPlayer(tx, playerPos).SetHeldItems(item.NewStack(item.GoldenCarrot{}, 1), item.Stack{})
	})
	closest := 100.0
	for range 400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { closest = min(closest, horizontalDistance(entityPos(tx, h), playerPos)) })
	}
	if closest > 3.5 {
		t.Fatalf("horse came no closer than %.2f blocks to a player holding a golden carrot behind a wall", closest)
	}
}
