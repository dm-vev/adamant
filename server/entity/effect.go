package entity

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"maps"
	"reflect"
	"slices"
	"sync"
)

// EffectManager manages the effects of an entity. The effect manager will only store effects that last for
// a specific duration. Instant effects are applied instantly and not stored.
type EffectManager struct {
	mu             sync.Mutex
	initialEffects []effect.Effect
	effects        map[reflect.Type]effect.Effect
}

// NewEffectManager creates and returns a new initialised EffectManager.
func NewEffectManager(eff ...effect.Effect) *EffectManager {
	return &EffectManager{effects: make(map[reflect.Type]effect.Effect), initialEffects: eff}
}

// Add adds an effect to the manager. If the effect is instant, it is applied to the Living entity passed
// immediately. If not, the effect is added to the EffectManager and is applied to the entity every time the
// Tick method is called.
// Effect levels of 0 or below will not do anything.
// Effect returns the final effect it added to the entity. That might be the effect passed or an effect with
// a higher level/duration than the one passed. Add panics if the effect has a negative duration or level.
func (m *EffectManager) Add(e effect.Effect, entity Living) effect.Effect {
	m.validateEffect(e)

	m.mu.Lock()
	initialActions := m.collectInitialActionsLocked()
	result, action := m.addLocked(e)
	m.mu.Unlock()

	m.applyActions(entity, initialActions)
	m.applyAction(entity, action)
	return result
}

// Remove removes any Effect present in the EffectManager with the type of the effect passed.
func (m *EffectManager) Remove(e effect.Type, entity Living) {
	m.mu.Lock()
	initialActions := m.collectInitialActionsLocked()
	action := m.removeLocked(e)
	m.mu.Unlock()

	m.applyActions(entity, initialActions)
	m.applyAction(entity, action)
}

// Effect returns the effect instance and true if the entity has the effect. If not found, it will return an empty
// effect instance and false.
func (m *EffectManager) Effect(e effect.Type) (effect.Effect, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, eff := range m.initialEffects {
		if eff.Type() == e {
			return eff, true
		}
	}

	existing, ok := m.effects[reflect.TypeOf(e)]
	return existing, ok
}

// Effects returns a list of all effects currently present in the effect manager. This will never include
// effects that have expired.
func (m *EffectManager) Effects() []effect.Effect {
	m.mu.Lock()
	defer m.mu.Unlock()

	effects := slices.Collect(maps.Values(m.effects))
	initial := append([]effect.Effect(nil), m.initialEffects...)
	return append(effects, initial...)
}

// Tick ticks the EffectManager, applying all of its effects to the Living entity passed when applicable and
// removing expired effects.
func (m *EffectManager) Tick(entity Living, tx *world.Tx) {
	update := false

	m.mu.Lock()
	initialActions := m.collectInitialActionsLocked()

	var toApply []effect.Effect
	var toEnd []effect.Effect
	for i, eff := range m.effects {
		if m.expired(eff) {
			delete(m.effects, i)
			toEnd = append(toEnd, eff)
			update = true
			continue
		}
		toApply = append(toApply, eff)
		m.effects[i] = eff.TickDuration()
	}
	m.mu.Unlock()

	m.applyActions(entity, initialActions)
	for _, eff := range toEnd {
		eff.Type().(effect.LastingType).End(entity, eff.Level())
	}
	for _, eff := range toApply {
		eff.Type().Apply(entity, eff)
	}

	if update {
		viewers := tx.Viewers(entity.Position())
		for _, v := range viewers {
			v.ViewEntityState(entity)
		}
		tx.ReleaseViewers(viewers)
	}
}

// expired checks if an Effect has expired.
func (m *EffectManager) expired(e effect.Effect) bool {
	return e.Duration() <= 0 && !e.Infinite()
}

type effectAction struct {
	apply        effect.Effect
	applyInstant bool
	start        effect.LastingType
	startLevel   int
	end          effect.LastingType
	endLevel     int
}

func (m *EffectManager) validateEffect(e effect.Effect) {
	lvl, dur := e.Level(), e.Duration()
	if lvl <= 0 {
		panic(fmt.Sprintf("(*EffectManager).Add: effect cannot have level of 0 or below: %v", lvl))
	}
	if dur < 0 {
		panic(fmt.Sprintf("(*EffectManager).Add: effect cannot have negative duration: %v", dur))
	}
}

func (m *EffectManager) collectInitialActionsLocked() []effectAction {
	if len(m.initialEffects) == 0 {
		return nil
	}
	initialEffects := m.initialEffects
	m.initialEffects = nil

	actions := make([]effectAction, 0, len(initialEffects))
	for _, eff := range initialEffects {
		m.validateEffect(eff)
		_, action := m.addLocked(eff)
		actions = append(actions, action)
	}
	return actions
}

func (m *EffectManager) addLocked(e effect.Effect) (effect.Effect, effectAction) {
	t, ok := e.Type().(effect.LastingType)
	if !ok {
		return e, effectAction{apply: e, applyInstant: true}
	}
	typ := reflect.TypeOf(e.Type())
	existing, ok := m.effects[typ]
	if !ok {
		m.effects[typ] = e
		return e, effectAction{start: t, startLevel: e.Level()}
	}
	if existing.Level() > e.Level() || (existing.Level() == e.Level() && ((existing.Duration() > e.Duration() && !e.Infinite()) || existing.Infinite())) {
		return existing, effectAction{}
	}
	m.effects[typ] = e
	return e, effectAction{
		end:        existing.Type().(effect.LastingType),
		endLevel:   existing.Level(),
		start:      t,
		startLevel: e.Level(),
	}
}

func (m *EffectManager) removeLocked(e effect.Type) effectAction {
	t := reflect.TypeOf(e)
	if existing, ok := m.effects[t]; ok {
		delete(m.effects, t)
		return effectAction{
			end:      existing.Type().(effect.LastingType),
			endLevel: existing.Level(),
		}
	}
	return effectAction{}
}

func (m *EffectManager) applyActions(entity Living, actions []effectAction) {
	for _, action := range actions {
		m.applyAction(entity, action)
	}
}

func (m *EffectManager) applyAction(entity Living, action effectAction) {
	if action.applyInstant {
		action.apply.Type().Apply(entity, action.apply)
	}
	if action.end != nil {
		action.end.End(entity, action.endLevel)
	}
	if action.start != nil {
		action.start.Start(entity, action.startLevel)
	}
}
