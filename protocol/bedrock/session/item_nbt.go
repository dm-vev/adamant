package session

import (
	"maps"
	"math"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// networkItemNBT returns the item NBT ItemStack sends for a stack: the tag
// compound of the world format (item.WriteTag), with the damage of a durable
// item as an int32 where the world format writes an int16.
func networkItemNBT(s item.Stack) map[string]any {
	tag := item.WriteTag(s)
	if _, ok := s.Item().(item.Durable); !ok {
		return tag
	}
	damage := s.MaxDurability() - s.Durability()
	if tag["Damage"] != any(int16(damage)) {
		// The NBT of the item holds a damage of its own, which WriteTag
		// kept.
		return tag
	}
	if damage != 0 {
		if nbter, ok := s.Item().(world.NBTer); ok {
			if own, ok := nbter.EncodeNBT()["Damage"]; ok && own == tag["Damage"] {
				// The NBT of the item holds the same damage, as an int16,
				// which WriteTag kept.
				return tag
			}
		}
	}
	tag["Damage"] = int32(damage)
	return tag
}

// readNetworkItemNBT applies item NBT a client sent in an ItemStack to s, a
// new stack of the item it names, and returns the result: the tag compound
// of the world format (item.ReadTag), with the damage an int32 where the world
// format has an int16. A damage of any other type is no damage.
func readNetworkItemNBT(tag map[string]any, s item.Stack) item.Stack {
	damage, _ := tag["Damage"].(int32)
	tag = maps.Clone(tag)
	if tag == nil {
		tag = map[string]any{}
	}
	switch {
	case damage >= math.MinInt16 && damage <= math.MaxInt16:
		tag["Damage"] = int16(damage)
	case damage < 0 || s.MaxDurability() <= math.MaxInt16:
		// Damaging a new stack by any negative damage leaves it whole, and
		// by any damage of its durability or more breaks it, so the bound
		// of an int16 damages it alike.
		tag["Damage"] = int16(max(min(damage, math.MaxInt16), math.MinInt16))
	default:
		// A damage the world format cannot hold, of an item more durable
		// than an int16 counts. ReadTag reads the anvil cost before the
		// damage, which does not change it, so the damage may come first.
		delete(tag, "Damage")
		return item.ReadTag(tag, s.Damage(int(damage)))
	}
	return item.ReadTag(tag, s)
}
