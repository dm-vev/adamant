package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// hungerTestPlayer adds a survival player standing on stone at the
// difficulty passed and runs f with it.
func hungerTestPlayer(t *testing.T, difficulty world.Difficulty, f func(tx *world.Tx, p *Player)) {
	t.Helper()
	w := world.Config{Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetDifficulty(difficulty)
	<-w.Exec(func(tx *world.Tx) {
		// Away from the chunk border, so ground checks see loaded blocks only.
		tx.SetBlock(cube.Pos{8, 0, 8}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{9, 0, 8}, block.Stone{}, nil)
		pos := mgl64.Vec3{8.5, 1, 8.5}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, Config{Position: pos, GameMode: world.GameModeSurvival})).(*Player)
		f(tx, p)
	})
}

func hungerApprox(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// TestSaturationHealsBySaturationSpent follows FoodData.tick: with a full
// food bar and saturation, every 10 ticks the player heals a sixth of the
// saturation spent (at most 6) and gains that much exhaustion, which costs a
// point of saturation once it exceeds 4.
func TestSaturationHealsBySaturationSpent(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyNormal, func(tx *world.Tx, p *Player) {
		p.hunger.setState(20, 0, 0, 5)
		p.health.AddHealth(-10)
		for range 20 {
			p.tickFood()
		}
		// Tick 10 heals 5/6 and adds 5 exhaustion, tick 11 drains 4 of it for a
		// point of saturation, tick 20 heals 4/6 and adds 4 exhaustion.
		got := p.hunger.snapshot()
		if !hungerApprox(p.Health(), 10+5.0/6+4.0/6) || got.saturationLevel != 4 || !hungerApprox(got.exhaustionLevel, 5) || got.foodLevel != 20 {
			t.Fatalf("health %v, food %v, saturation %v, exhaustion %v; want 11.5, 20, 4, 5", p.Health(), got.foodLevel, got.saturationLevel, got.exhaustionLevel)
		}
	})
}

// TestSlowRegenerationNeedsEightyTicks heals one point every 80 ticks with 18
// or more food and no saturation, costing 6 exhaustion.
func TestSlowRegenerationNeedsEightyTicks(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyNormal, func(tx *world.Tx, p *Player) {
		p.hunger.setState(18, 0, 0, 0)
		p.health.AddHealth(-10)
		for range 79 {
			p.tickFood()
		}
		if p.Health() != 10 {
			t.Fatalf("health after 79 ticks = %v, want 10", p.Health())
		}
		p.tickFood()
		if got := p.hunger.snapshot(); p.Health() != 11 || got.exhaustionLevel != 6 {
			t.Fatalf("health %v, exhaustion %v after 80 ticks; want 11, 6", p.Health(), got.exhaustionLevel)
		}
		p.tickFood()
		if got := p.hunger.snapshot(); got.foodLevel != 17 || got.exhaustionLevel != 2 {
			t.Fatalf("food %v, exhaustion %v after 81 ticks; want 17, 2", got.foodLevel, got.exhaustionLevel)
		}
	})
}

// TestExhaustionDrainsOncePerTick removes at most 4 exhaustion a tick and caps
// it at 40, as FoodData.addExhaustion and tick do.
func TestExhaustionDrainsOncePerTick(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyNormal, func(tx *world.Tx, p *Player) {
		p.hunger.setState(20, 0, 0, 1)
		p.Exhaust(100)
		if got := p.hunger.snapshot(); got.exhaustionLevel != 40 || got.saturationLevel != 1 {
			t.Fatalf("exhaustion %v, saturation %v before ticking; want 40, 1", got.exhaustionLevel, got.saturationLevel)
		}
		for range 3 {
			p.tickFood()
		}
		if got := p.hunger.snapshot(); got.exhaustionLevel != 28 || got.saturationLevel != 0 || got.foodLevel != 18 {
			t.Fatalf("exhaustion %v, saturation %v, food %v after 3 ticks; want 28, 0, 18", got.exhaustionLevel, got.saturationLevel, got.foodLevel)
		}
	})
}

// TestStarvationStopsAtHalfAHeartOnNormal hurts a starving player while its
// health is above 1 on Normal, as FoodData.tick.
func TestStarvationStopsAtHalfAHeartOnNormal(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyNormal, func(tx *world.Tx, p *Player) {
		p.hunger.setState(0, 0, 0, 0)
		p.health.AddHealth(-18.5)
		for range 160 {
			p.tickFood()
		}
		if p.Health() != 0.5 {
			t.Fatalf("health after 160 starving ticks = %v, want 0.5", p.Health())
		}
	})
}

// TestPeacefulRegeneration follows ServerPlayer.tickRegeneration: every 20
// ticks the player heals a point and gains a point of saturation, every 10
// ticks a point of food, and exhaustion still drains saturation but never
// food.
func TestPeacefulRegeneration(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyPeaceful, func(tx *world.Tx, p *Player) {
		p.hunger.setState(10, 0, 0, 0)
		p.health.AddHealth(-10)
		p.Exhaust(5)
		for range 20 {
			p.tickCount++
			p.tickFood()
		}
		got := p.hunger.snapshot()
		if p.Health() != 11 || got.foodLevel != 12 || got.saturationLevel != 1 || got.exhaustionLevel != 1 {
			t.Fatalf("health %v, food %v, saturation %v, exhaustion %v; want 11, 12, 1, 1", p.Health(), got.foodLevel, got.saturationLevel, got.exhaustionLevel)
		}
	})
}

// TestMovementExhaustion follows ServerPlayer.checkMovementStatistics:
// sprinting on the ground costs 0.1 a metre; walking and sprinting through
// the air cost nothing.
func TestMovementExhaustion(t *testing.T) {
	hungerTestPlayer(t, world.DifficultyNormal, func(tx *world.Tx, p *Player) {
		p.Move(mgl64.Vec3{0.5, 0, 0}, 0, 0)
		if got := p.hunger.snapshot().exhaustionLevel; got != 0 {
			t.Fatalf("walking exhaustion = %v, want 0", got)
		}
		p.StartSprinting()
		p.Move(mgl64.Vec3{-0.5, 0, 0}, 0, 0)
		if got := p.hunger.snapshot().exhaustionLevel; !hungerApprox(got, 0.05) {
			t.Fatalf("sprinting exhaustion = %v, want 0.05", got)
		}
		p.Move(mgl64.Vec3{0, 1, 0}, 0, 0)
		p.Move(mgl64.Vec3{0.5, 0, 0}, 0, 0)
		if got := p.hunger.snapshot().exhaustionLevel; !hungerApprox(got, 0.05) {
			t.Fatalf("airborne sprinting exhaustion = %v, want 0.05", got)
		}
	})
}
