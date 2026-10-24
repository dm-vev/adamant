package entity

import (
	"math"
	"slices"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
)

// javaAttribute ports an AttributeInstance: a base value and the modifiers
// that change it, kept as Java saves them in attributes.
type javaAttribute struct {
	ID        string
	Base      float64
	Modifiers []javaAttributeModifier
}

// javaAttributeModifier ports an AttributeModifier.
type javaAttributeModifier struct {
	ID        string
	Amount    float64
	Operation string
}

// value ports AttributeInstance.calculateValue, clamped to the range of the
// attribute.
func (a *javaAttribute) value(minValue, maxValue float64) float64 {
	v := a.Base
	for _, m := range a.Modifiers {
		if m.Operation == "add_value" {
			v += m.Amount
		}
	}
	added := v
	for _, m := range a.Modifiers {
		if m.Operation == "add_multiplied_base" {
			v += added * m.Amount
		}
	}
	for _, m := range a.Modifiers {
		if m.Operation == "add_multiplied_total" {
			v *= 1 + m.Amount
		}
	}
	return max(minValue, min(maxValue, v))
}

// modifier returns the amount of the modifier with the id passed, if the
// attribute has one.
func (a *javaAttribute) modifier(id string) (float64, bool) {
	for _, m := range a.Modifiers {
		if m.ID == id {
			return m.Amount, true
		}
	}
	return 0, false
}

// setModifier ports AttributeInstance.addOrReplacePermanentModifier.
func (a *javaAttribute) setModifier(id string, amount float64, operation string) {
	for i := range a.Modifiers {
		if a.Modifiers[i].ID == id {
			a.Modifiers[i] = javaAttributeModifier{ID: id, Amount: amount, Operation: operation}
			return
		}
	}
	a.Modifiers = append(a.Modifiers, javaAttributeModifier{ID: id, Amount: amount, Operation: operation})
}

// javaAttributes holds the Java attributes of a mob by their ids.
type javaAttributes map[string]*javaAttribute

// get returns the attribute with the id passed, with the base def if the
// mob has none yet.
func (m *javaAttributes) get(id string, def float64) *javaAttribute {
	if *m == nil {
		*m = javaAttributes{}
	}
	if a, ok := (*m)[id]; ok {
		return a
	}
	a := &javaAttribute{ID: id, Base: def}
	(*m)[id] = a
	return a
}

// decodeJavaAttributes reads the attributes list Java saves.
func decodeJavaAttributes(raw any) javaAttributes {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := javaAttributes{}
	for _, v := range list {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		if id == "" {
			continue
		}
		a := &javaAttribute{ID: id, Base: oceanNumber(entry["base"])}
		if mods, ok := entry["modifiers"].([]any); ok {
			for _, mv := range mods {
				m, ok := mv.(map[string]any)
				if !ok {
					continue
				}
				mid, _ := m["id"].(string)
				op, _ := m["operation"].(string)
				a.Modifiers = append(a.Modifiers, javaAttributeModifier{ID: mid, Amount: oceanNumber(m["amount"]), Operation: op})
			}
		}
		out[id] = a
	}
	return out
}

// encode writes the attributes as Java saves them.
func (m javaAttributes) encode() []any {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		a := m[id]
		entry := map[string]any{"id": id, "base": a.Base}
		if len(a.Modifiers) != 0 {
			mods := make([]any, 0, len(a.Modifiers))
			for _, mod := range a.Modifiers {
				mods = append(mods, map[string]any{"id": mod.ID, "amount": mod.Amount, "operation": mod.Operation})
			}
			entry["modifiers"] = mods
		}
		out = append(out, entry)
	}
	return out
}

// handleZombieAttributes ports Zombie.handleAttributes: a zombie spawns with
// a small chance to call reinforcements, a knockback resistance and follow
// range bonus, and now and then as a leader with more health, a greater
// chance of reinforcements and the strength to break doors.
func (b *hostileState) handleZombieAttributes(m *HostileMob, special float32, r *vanilla.Random) {
	b.attrs.get("minecraft:spawn_reinforcements", 0).Base = r.NextDouble() * float64(float32(.1))
	b.attrs.get("minecraft:knockback_resistance", 0).setModifier("minecraft:random_spawn_bonus", r.NextDouble()*float64(float32(.05)), "add_value")
	if bonus := r.NextDouble() * 1.5 * float64(special); bonus > 1 {
		b.attrs.get("minecraft:follow_range", 0).setModifier("minecraft:zombie_random_spawn_bonus", bonus, "add_multiplied_total")
	}
	if r.NextFloat() < special*.05 {
		b.attrs.get("minecraft:spawn_reinforcements", 0).setModifier("minecraft:leader_zombie_bonus", r.NextDouble()*.25+.5, "add_value")
		health := b.attrs.get("minecraft:max_health", 20)
		health.setModifier("minecraft:leader_zombie_bonus", r.NextDouble()*3+1, "add_multiplied_total")
		maxHealth := health.value(1, 1024)
		b.health.SetMaxHealth(maxHealth)
		b.health.AddHealth(maxHealth)
		b.canBreakDoors = true
	}
}

// zombieKind reports whether a hostile mob kind is a Zombie in Java.
func zombieKind(kind string) bool {
	return kind == "minecraft:zombie" || kind == "minecraft:husk" || kind == "minecraft:zombie_villager"
}

// callReinforcements ports the reinforcements of Zombie.hurtServer: on hard,
// a zombie fighting a target may call another zombie of its kind to a spot
// seven to forty blocks away out of sight of players, each call lowering
// the chance of the next for both.
func (m *HostileMob) callReinforcements(src world.DamageSource) {
	b := m.state()
	tx := m.tx
	mob, ok := mobOf(m)
	if !ok {
		return
	}
	target := mob.Target()
	if target == nil {
		if attacker := damageAttacker(src); attacker != nil {
			if _, living := attacker.(Living); living {
				target = attacker
			}
		}
	}
	r := mob.Random()
	chance := b.attrs.get("minecraft:spawn_reinforcements", 0)
	if target == nil || tx.World().Difficulty() != world.DifficultyHard || float64(r.NextFloat()) >= chance.value(0, 1) || !tx.World().SpawnMobsEnabled() {
		return
	}
	spawn := tx.World().EntityRegistry().Config().SpawnMob
	if spawn == nil {
		return
	}
	p := m.Position()
	x, y, z := int(math.Floor(p[0])), int(math.Floor(p[1])), int(math.Floor(p[2]))
	nextInt := func(lo, hi int) int { return lo + int(r.NextInt(int32(hi-lo+1))) }
	t, ok := tx.World().EntityRegistry().Lookup(b.kind)
	if !ok {
		return
	}
	for range 50 {
		pos := cube.Pos{x + nextInt(7, 40)*nextInt(-1, 1), y + nextInt(7, 40)*nextInt(-1, 1), z + nextInt(7, 40)*nextInt(-1, 1)}
		if !spawnPositionOk(tx, b.kind, pos) || !checkSpawnRules(tx, b.kind, pos, r) {
			continue
		}
		at := pos.Vec3()
		box := t.BBox(nil).Translate(at)
		if nearbyAlivePlayer(tx, at, 7) || livingEntityIn(tx, box) || !spawnNoCollision(tx, box) || liquidIn(tx, box) {
			continue
		}
		handle := spawn(b.kind, world.EntitySpawnOpts{Position: at, SpawnReason: "reinforcement", SpawnSeed: r.NextLong()})
		if handle == nil {
			return
		}
		called := tx.AddEntity(handle)
		if cm, ok := mobOf(called); ok {
			cm.SetTarget(target)
		}
		charge, _ := chance.modifier("minecraft:reinforcement_caller_charge")
		chance.setModifier("minecraft:reinforcement_caller_charge", charge-.05, "add_value")
		if h, ok := called.(interface{ state() *hostileState }); ok {
			h.state().attrs.get("minecraft:spawn_reinforcements", 0).setModifier("minecraft:reinforcement_callee_charge", -.05, "add_value")
		}
		return
	}
}
