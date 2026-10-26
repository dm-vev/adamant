package player

import (
	"sync"

	"github.com/df-mc/dragonfly/server/entity"
)

// hungerManager holds the food level, saturation, exhaustion and timer of a
// player, as Java's FoodData does.
type hungerManager struct {
	mu              sync.RWMutex
	foodLevel       int
	saturationLevel float64
	exhaustionLevel float64
	// foodTick is FoodData.tickTimer, counting ticks towards the next
	// regeneration or starvation step.
	foodTick int
}

type hungerSnapshot struct {
	foodLevel       int
	saturationLevel float64
	exhaustionLevel float64
	foodTick        int
}

// newHungerManager returns a new hunger manager with the default values for food level, saturation level and
// exhaustion level.
func newHungerManager() *hungerManager {
	return &hungerManager{foodLevel: 20, saturationLevel: 5}
}

// Food returns the current food level of a player. The level returned is guaranteed to always be between 0
// and 20.
func (m *hungerManager) Food() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.foodLevel
}

// SetFood sets the food level of a player. The level passed must be in a range of 0-20. If the level passed
// is negative, the food level will be set to 0. If the level exceeds 20, the food level will be set to 20.
func (m *hungerManager) SetFood(level int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodLevel = max(min(level, 20), 0)
}

// AddFood adds a number of food points to the current food level of a player.
func (m *hungerManager) AddFood(points int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodLevel = max(min(m.foodLevel+points, 20), 0)
}

// setState applies a full hunger state snapshot at once.
func (m *hungerManager) setState(food, foodTick int, exhaustion, saturation float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodLevel = food
	m.foodTick = foodTick
	m.exhaustionLevel = exhaustion
	m.saturationLevel = saturation
}

// snapshot returns a consistent copy of the current hunger state.
func (m *hungerManager) snapshot() hungerSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return hungerSnapshot{
		foodLevel:       m.foodLevel,
		foodTick:        m.foodTick,
		exhaustionLevel: m.exhaustionLevel,
		saturationLevel: m.saturationLevel,
	}
}

// Reset resets the hunger manager to its default values, identical to those set when creating a new manager
// using newHungerManager.
func (m *hungerManager) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodLevel = 20
	m.saturationLevel = 5
	m.exhaustionLevel = 0
	m.foodTick = 0
}

// exhaust ports FoodData.addExhaustion: exhaustion builds up to at most 40
// and is only turned into lost saturation or food by drainExhaustion.
func (m *hungerManager) exhaust(points float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.exhaustionLevel = min(m.exhaustionLevel+points, 40)
}

// drainExhaustion runs the first step of FoodData.tick: above 4 exhaustion, 4
// is removed and a point of saturation is lost, or, without saturation and
// outside Peaceful, a point of food. It returns the food level and whether
// that point of food is to be lost; the caller removes it.
func (m *hungerManager) drainExhaustion(peaceful bool) (food int, loseFood bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.exhaustionLevel <= 4 {
		return m.foodLevel, false
	}
	m.exhaustionLevel -= 4
	if m.saturationLevel > 0 {
		m.saturationLevel = max(m.saturationLevel-1, 0)
		return m.foodLevel, false
	}
	return m.foodLevel, !peaceful && m.foodLevel > 0
}

// advanceTimer counts one tick of the food timer and reports, resetting the
// timer, once it reaches interval.
func (m *hungerManager) advanceTimer(interval int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.foodTick++; m.foodTick >= interval {
		m.foodTick = 0
		return true
	}
	return false
}

// resetTimer sets the food timer back to zero.
func (m *hungerManager) resetTimer() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodTick = 0
}

// regeneratePeaceful applies the saturation half of
// ServerPlayer.tickRegeneration: saturation grows by one up to 20, even past
// the food level.
func (m *hungerManager) regeneratePeaceful() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.saturationLevel < 20 {
		m.saturationLevel++
	}
}

// saturate saturates the player's food and saturation by the amount of points passed. Note that the total
// saturation will never exceed the total food value.
func (m *hungerManager) saturate(food int, saturation float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.foodLevel = max(min(m.foodLevel+food, 20), 0)
	m.saturationLevel = max(min(m.saturationLevel+saturation, float64(m.foodLevel)), 0)
}

// canSprint returns true if the food level of the player is 7 or higher.
func (m *hungerManager) canSprint() bool {
	return m.Food() > 6
}

// StarvationDamageSource is the world.DamageSource passed when a player is
// dealt damage from an empty food bar.
type StarvationDamageSource = entity.StarvationDamageSource
