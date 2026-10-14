package loot

import (
	"fmt"
	"strings"
	"time"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// LootComponentHolder is implemented by entities exposing Java data
// components (e.g. "minecraft:sheep/color") to minecraft:components entity
// predicates.
type LootComponentHolder interface {
	LootComponents() map[string]any
}

// bedrockToJavaEntity maps Bedrock actor identifiers whose Java entity type
// differs.
var bedrockToJavaEntity = map[string]string{
	"minecraft:zombie_pigman":          "minecraft:zombified_piglin",
	"minecraft:ender_crystal":          "minecraft:end_crystal",
	"minecraft:fireworks_rocket":       "minecraft:firework_rocket",
	"minecraft:thrown_trident":         "minecraft:trident",
	"minecraft:evocation_illager":      "minecraft:evoker",
	"minecraft:vindication_illager":    "minecraft:vindicator",
	"minecraft:wither_skull_dangerous": "minecraft:wither_skull",
	"minecraft:wind_charge_projectile": "minecraft:wind_charge",
	"minecraft:breeze_wind_charge":     "minecraft:breeze_wind_charge",
	"minecraft:snow_golem":             "minecraft:snow_golem",
	"minecraft:villager_v2":            "minecraft:villager",
	"minecraft:zombie_villager_v2":     "minecraft:zombie_villager",
	"minecraft:leash_knot":             "minecraft:leash_knot",
	"minecraft:xp_orb":                 "minecraft:experience_orb",
	"minecraft:xp_bottle":              "minecraft:experience_bottle",
	"minecraft:splash_potion":          "minecraft:splash_potion",
	"minecraft:ender_pearl":            "minecraft:ender_pearl",
	"minecraft:eye_of_ender_signal":    "minecraft:eye_of_ender",
	"minecraft:fishing_hook":           "minecraft:fishing_bobber",
}

// JavaEntityID returns the Java entity type identifier of an entity. Entities
// whose Java type depends on their state, like boats of each wood, report it
// through a JavaEntityID method.
func JavaEntityID(e world.Entity) string {
	if typed, ok := e.(interface{ JavaEntityID() string }); ok {
		return typed.JavaEntityID()
	}
	id := e.H().Type().EncodeEntity()
	if java, ok := bedrockToJavaEntity[id]; ok {
		return java
	}
	return id
}

// entityFor resolves the entity a loot predicate targets.
func entityFor(target string, c *Context) world.Entity {
	switch target {
	case "this":
		return c.ThisEntity
	case "attacker":
		return c.SourceEntity
	case "direct_attacker":
		if c.DirectEntity != nil {
			return c.DirectEntity
		}
		return c.SourceEntity
	case "attacking_player":
		return c.Player
	}
	return nil
}

// matchEntity ports EntityPredicate.matches for the predicate keys used by
// the builtin loot tables. Keys the entity cannot answer fall back to the
// properties supplied in the loot context.
func matchEntity(e world.Entity, predicate map[string]any, c *Context) (bool, error) {
	for key, want := range predicate {
		ok, err := matchEntityKey(e, key, want, c)
		if err != nil || !ok {
			return false, err
		}
	}
	return true, nil
}

func matchEntityKey(e world.Entity, key string, want any, c *Context) (bool, error) {
	switch key {
	case "minecraft:entity_type", "type":
		name, ok := want.(string)
		if !ok {
			return false, fmt.Errorf("invalid entity type predicate %T", want)
		}
		if strings.HasPrefix(name, "#") {
			return false, fmt.Errorf("entity type tag predicate %q requires entity tags", name)
		}
		return JavaEntityID(e) == normalize(name), nil
	case "minecraft:flags", "flags":
		flags, ok := want.(map[string]any)
		if !ok {
			return false, fmt.Errorf("invalid entity flags predicate %T", want)
		}
		for flag, expected := range flags {
			b, ok := expected.(bool)
			if !ok {
				return false, fmt.Errorf("invalid entity flag %q value %T", flag, expected)
			}
			actual, err := entityFlag(e, flag)
			if err != nil {
				return false, err
			}
			if actual != b {
				return false, nil
			}
		}
		return true, nil
	case "minecraft:vehicle", "vehicle":
		nested, ok := want.(map[string]any)
		if !ok {
			return false, fmt.Errorf("invalid vehicle predicate %T", want)
		}
		r, ok := e.(interface{ Riding() *world.EntityHandle })
		if !ok || r.Riding() == nil || c.Tx == nil {
			return false, nil
		}
		vehicle, ok := r.Riding().Entity(c.Tx)
		if !ok {
			return false, nil
		}
		return matchEntity(vehicle, nested, c)
	case "minecraft:equipment", "equipment":
		slots, ok := want.(map[string]any)
		if !ok {
			return false, fmt.Errorf("invalid equipment predicate %T", want)
		}
		for slot, raw := range slots {
			stack, err := equipmentStack(e, slot)
			if err != nil {
				return false, err
			}
			if ok, err := matchItemStack(stack, raw); err != nil || !ok {
				return false, err
			}
		}
		return true, nil
	case "minecraft:components":
		components, ok := want.(map[string]any)
		if !ok {
			return false, fmt.Errorf("invalid entity components predicate %T", want)
		}
		holder, ok := e.(LootComponentHolder)
		if !ok {
			return false, nil
		}
		actual := holder.LootComponents()
		for k, v := range components {
			if a, ok := actual[k]; !ok || !equalJSON(v, a) {
				return false, nil
			}
		}
		return true, nil
	case "minecraft:type_specific/sheep":
		fields, _ := want.(map[string]any)
		sheared, ok := fields["sheared"].(bool)
		s, isSheep := e.(interface{ Sheared() bool })
		return ok && isSheep && s.Sheared() == sheared, nil
	case "minecraft:type_specific/cube_mob":
		fields, _ := want.(map[string]any)
		cube, ok := e.(interface{ CubeSize() int })
		if id := e.H().Type().EncodeEntity(); !ok || id != "minecraft:slime" && id != "minecraft:magma_cube" {
			return false, nil
		}
		return matchesIntRange(fields["size"], cube.CubeSize())
	case "minecraft:type_specific/raider":
		fields, _ := want.(map[string]any)
		r, ok := e.(interface{ IsCaptain() bool })
		if !ok {
			return false, nil
		}
		if captain, ok := fields["is_captain"].(bool); ok && r.IsCaptain() != captain {
			return false, nil
		}
		return true, nil
	case "minecraft:type_specific/fishing_hook":
		fields, _ := want.(map[string]any)
		open, ok := fields["in_open_water"].(bool)
		if !ok {
			return false, fmt.Errorf("invalid fishing-hook predicate %v", want)
		}
		return e.H().Type().EncodeEntity() == "minecraft:fishing_hook" && c.OpenWater == open, nil
	}
	if c.EntityProperties == nil {
		return false, fmt.Errorf("entity predicate %q is not supported", key)
	}
	actual, ok := c.EntityProperties[key]
	return ok && equalJSON(want, actual), nil
}

// entityFlag ports EntityFlagsPredicate.
func entityFlag(e world.Entity, flag string) (bool, error) {
	switch flag {
	case "is_on_fire":
		f, ok := e.(interface{ OnFireDuration() time.Duration })
		return ok && f.OnFireDuration() > 0, nil
	case "is_baby":
		b, ok := e.(interface{ Baby() bool })
		return ok && b.Baby(), nil
	case "is_sneaking":
		s, ok := e.(interface{ Sneaking() bool })
		return ok && s.Sneaking(), nil
	case "is_sprinting":
		s, ok := e.(interface{ Sprinting() bool })
		return ok && s.Sprinting(), nil
	case "is_swimming":
		s, ok := e.(interface{ Swimming() bool })
		return ok && s.Swimming(), nil
	case "is_on_ground":
		s, ok := e.(interface{ OnGround() bool })
		return ok && s.OnGround(), nil
	case "is_flying", "is_fall_flying":
		s, ok := e.(interface{ Gliding() bool })
		return ok && s.Gliding(), nil
	}
	return false, fmt.Errorf("unsupported entity flag %q", flag)
}

// equipmentStack returns the item in the named equipment slot.
func equipmentStack(e world.Entity, slot string) (item.Stack, error) {
	switch slot {
	case "mainhand", "offhand":
		h, ok := e.(interface {
			HeldItems() (item.Stack, item.Stack)
		})
		if !ok {
			return item.Stack{}, nil
		}
		main, off := h.HeldItems()
		if slot == "mainhand" {
			return main, nil
		}
		return off, nil
	case "head", "chest", "legs", "feet":
		a, ok := e.(interface {
			Armour() interface {
				Helmet() item.Stack
				Chestplate() item.Stack
				Leggings() item.Stack
				Boots() item.Stack
			}
		})
		if !ok {
			return item.Stack{}, nil
		}
		switch slot {
		case "head":
			return a.Armour().Helmet(), nil
		case "chest":
			return a.Armour().Chestplate(), nil
		case "legs":
			return a.Armour().Leggings(), nil
		}
		return a.Armour().Boots(), nil
	}
	return item.Stack{}, fmt.Errorf("unsupported equipment slot %q", slot)
}

// matchItemStack ports ItemPredicate: items, count, components and the
// minecraft:enchantments sub-predicate.
func matchItemStack(stack item.Stack, raw any) (bool, error) {
	predicate, ok := raw.(map[string]any)
	if !ok {
		return false, fmt.Errorf("invalid item predicate %T", raw)
	}
	if stack.Empty() {
		return false, nil
	}
	id, _ := stack.Item().EncodeItem()
	if items, ok := predicate["items"]; ok {
		names := []any{items}
		if list, ok := items.([]any); ok {
			names = list
		}
		matches := false
		for _, v := range names {
			name, ok := v.(string)
			if !ok {
				return false, fmt.Errorf("invalid item predicate item %T", v)
			}
			if strings.HasPrefix(name, "#") {
				matches = matches || itemInTag(id, name[1:])
			} else {
				matches = matches || normalize(id) == normalize(name)
			}
		}
		if !matches {
			return false, nil
		}
	}
	if countRange, ok := predicate["count"]; ok {
		if ok, err := matchesIntRange(countRange, stack.Count()); err != nil || !ok {
			return false, err
		}
	}
	if components, ok := predicate["components"]; ok {
		if ok, err := matchItemComponents(components, stack); err != nil || !ok {
			return false, err
		}
	}
	if sub, ok := predicate["predicates"].(map[string]any); ok {
		for key, value := range sub {
			if key != "minecraft:enchantments" {
				return false, fmt.Errorf("unsupported item sub-predicate %q", key)
			}
			list, ok := value.([]any)
			if !ok {
				return false, fmt.Errorf("invalid enchantments predicate %T", value)
			}
			for _, entry := range list {
				m, _ := entry.(map[string]any)
				if !stackHasEnchantment(stack, m) {
					return false, nil
				}
			}
		}
	}
	return true, nil
}

// stackHasEnchantment ports EnchantmentPredicate: some enchantment in the
// id, tag or list matches with a level in range.
func stackHasEnchantment(stack item.Stack, m map[string]any) bool {
	var ids []string
	switch v := m["enchantments"].(type) {
	case string:
		if strings.HasPrefix(v, "#") {
			tag := v[1:]
			if !strings.Contains(tag, ":") {
				tag = "minecraft:" + tag
			}
			for id := range enchantmentTags[tag] {
				ids = append(ids, id)
			}
		} else {
			ids = []string{v}
		}
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok {
				ids = append(ids, s)
			}
		}
	case nil:
		for _, e := range stack.Enchantments() {
			ids = append(ids, enchantmentID(e.Type()))
		}
	}
	for _, id := range ids {
		level := lootEnchantmentLevel(stack, id)
		if level == 0 {
			continue
		}
		if r, ok := m["levels"]; ok {
			if ok, _ := matchesIntRange(r, level); !ok {
				continue
			}
		}
		return true
	}
	return false
}

// testDamageSourcePredicate ports DamageSourcePredicate: tags, the direct
// entity (the projectile or attacker) and the source entity (its owner).
func testDamageSourcePredicate(x condition, c *Context) (bool, error) {
	predicate, ok := x.Predicate.(map[string]any)
	if !ok {
		return false, fmt.Errorf("invalid damage source predicate %T", x.Predicate)
	}
	if c.Damage == nil {
		return false, nil
	}
	for key, want := range predicate {
		switch key {
		case "tags":
			list, ok := want.([]any)
			if !ok {
				return false, fmt.Errorf("invalid damage tags %T", want)
			}
			for _, raw := range list {
				m, _ := raw.(map[string]any)
				id, _ := m["id"].(string)
				expected, _ := m["expected"].(bool)
				if c.DamageTags[strings.TrimPrefix(id, "#")] != expected {
					return false, nil
				}
			}
		case "direct_entity", "source_entity":
			nested, ok := want.(map[string]any)
			if !ok {
				return false, fmt.Errorf("invalid damage %s predicate %T", key, want)
			}
			e := c.SourceEntity
			if key == "direct_entity" {
				e = c.DirectEntity
			}
			if e == nil {
				return false, nil
			}
			if ok, err := matchEntity(e, nested, c); err != nil || !ok {
				return false, err
			}
		case "is_direct":
			expected, _ := want.(bool)
			if (c.DirectEntity != nil && c.DirectEntity == c.SourceEntity) != expected {
				return false, nil
			}
		default:
			return false, fmt.Errorf("unsupported damage source predicate %q", key)
		}
	}
	return true, nil
}
