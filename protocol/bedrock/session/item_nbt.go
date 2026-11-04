package session

import (
	"maps"
	"math"
	"slices"
	"strconv"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// networkItemNBT returns the item NBT ItemStack sends for a stack: the tag
// compound of the world format (item.WriteTag), with the damage of a durable
// item as an int32 where the world format writes an int16, and the Java-only
// enchantments shown as showJavaOnlyEnchantments shows them.
func networkItemNBT(s item.Stack) map[string]any {
	tag := item.WriteTag(s)
	showJavaOnlyEnchantments(tag, s)
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
// format has an int16, less the lore lines of showJavaOnlyEnchantments. A
// damage of any other type is no damage.
func readNetworkItemNBT(tag map[string]any, s item.Stack) item.Stack {
	return withoutJavaOnlyLore(readNetworkTag(tag, s))
}

// readNetworkTag applies item NBT a client sent to s as readNetworkItemNBT
// does, lore lines and all.
func readNetworkTag(tag map[string]any, s item.Stack) item.Stack {
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

// tradeItemNBT returns the NBT of a stack of a trade offer: that of the world
// format (item.WriteNBT), with the Java-only enchantments shown as
// showJavaOnlyEnchantments shows them.
func tradeItemNBT(s item.Stack) map[string]any {
	m := item.WriteNBT(s)
	if tag, ok := m["tag"].(map[string]any); ok {
		showJavaOnlyEnchantments(tag, s)
	}
	return m
}

// showJavaOnlyEnchantments shows a client the Java-only enchantments of s
// (item.RegisterJavaOnlyEnchantment), of which it has no ID: tag, the item
// NBT of s, holds them under adamant:ench, which a client keeps but does not
// read. Each has a lore line of javaOnlyLore before the lore of s, and ench,
// if s has no other enchantment, is an empty list, for the glint a client
// gives an item that has ench (no client was at hand to see it). adamant:ench
// stays, so readNetworkItemNBT reads what a client sends back of the stack
// as s.
func showJavaOnlyEnchantments(tag map[string]any, s item.Stack) {
	lines := javaOnlyLore(s)
	if len(lines) == 0 {
		return
	}
	if _, ok := tag["ench"]; !ok {
		tag["ench"] = []map[string]any{}
	}
	display, _ := tag["display"].(map[string]any)
	display = maps.Clone(display)
	if display == nil {
		display = map[string]any{}
	}
	display["Lore"] = append(lines, s.Lore()...)
	tag["display"] = display
}

// withoutJavaOnlyLore returns s, read from item NBT a client sent, without
// the lore lines showJavaOnlyEnchantments put before its lore: those
// javaOnlyLore gives for the Java-only enchantments s was read with from
// adamant:ench.
func withoutJavaOnlyLore(s item.Stack) item.Stack {
	lines := javaOnlyLore(s)
	if lore := s.Lore(); len(lines) != 0 && len(lore) >= len(lines) && slices.Equal(lore[:len(lines)], lines) {
		return s.WithLore(lore[len(lines):]...)
	}
	return s
}

// javaOnlyLore returns a lore line for each Java-only enchantment of s, in
// the order of s.Enchantments: its name, with the level unless the
// enchantment has the one level, in grey, or red for a curse, as Java's
// Enchantment.getFullname writes them.
func javaOnlyLore(s item.Stack) []string {
	var lines []string
	for _, e := range s.Enchantments() {
		if !item.IsJavaOnlyEnchantment(e.Type()) {
			continue
		}
		// The reset drops the style a client gives lore of its own.
		line := "§r§7"
		if c, ok := e.Type().(interface{ Curse() bool }); ok && c.Curse() {
			line = "§r§c"
		}
		line += e.Type().Name()
		if e.Level() != 1 || e.Type().MaxLevel() != 1 {
			line += " " + enchantmentLevel(e.Level())
		}
		lines = append(lines, line)
	}
	return lines
}

// enchantmentLevel returns a level as the enchantment.level.N texts of Java
// name it, I to X, and as a number past X, which Java has no text for.
func enchantmentLevel(level int) string {
	if level >= 1 && level <= 10 {
		return [...]string{"I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X"}[level-1]
	}
	return strconv.Itoa(level)
}
