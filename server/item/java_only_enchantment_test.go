package item_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/dm-vev/adamant/net/nbt"
)

// roundTrip encodes the item NBT of a stack to the bytes of a world save
// and reads them back.
func roundTrip(t *testing.T, s item.Stack) item.Stack {
	t.Helper()
	b, err := nbt.MarshalEncoding(item.WriteNBT(s), nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding(b, &m, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	return item.ReadNBT(m)
}

// Bedrock has no ID of Sweeping Edge. The item NBT of a stack holds it by
// its Java ID under adamant:ench, and ench holds the enchantments Bedrock
// has alone: BDS 1.26.52.3 and 1.26.61.1 load, save and send adamant:ench as
// it is, also when they enchant the item, which drops an ench entry of an ID
// they do not know. A stack of Sweeping Edge alone has no ench.
func TestJavaOnlyEnchantmentNBT(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	sweeping := func(level int) map[string]any {
		return map[string]any{"id": "minecraft:sweeping_edge", "lvl": int16(level)}
	}
	for _, c := range []struct {
		name               string
		stack              item.Stack
		ench, adamant      any
		hasEnch, hasJavaID bool
	}{
		{
			name:  "with Sharpness",
			stack: sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 4), item.NewEnchantment(enchantment.SweepingEdge, 2)),
			ench:  []map[string]any{{"id": int16(9), "lvl": int16(4)}}, hasEnch: true,
			adamant: []map[string]any{sweeping(2)}, hasJavaID: true,
		},
		{
			name:    "alone",
			stack:   sword.WithEnchantments(item.NewEnchantment(enchantment.SweepingEdge, 3)),
			adamant: []map[string]any{sweeping(3)}, hasJavaID: true,
		},
		{
			name:    "on a book",
			stack:   item.NewStack(item.Book{}, 1).WithEnchantments(item.NewEnchantment(enchantment.SweepingEdge, 1)),
			adamant: []map[string]any{sweeping(1)}, hasJavaID: true,
		},
		{
			name:  "none",
			stack: sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1)),
			ench:  []map[string]any{{"id": int16(9), "lvl": int16(1)}}, hasEnch: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			tag := item.WriteTag(c.stack)
			if ench, ok := tag["ench"]; ok != c.hasEnch || ok && !reflect.DeepEqual(ench, c.ench) {
				t.Errorf("ench = %v (%v), want %v (%v)", ench, ok, c.ench, c.hasEnch)
			}
			if adamant, ok := tag["adamant:ench"]; ok != c.hasJavaID || ok && !reflect.DeepEqual(adamant, c.adamant) {
				t.Errorf("adamant:ench = %v (%v), want %v (%v)", adamant, ok, c.adamant, c.hasJavaID)
			}
			if got := roundTrip(t, c.stack); !got.Equal(c.stack) {
				t.Errorf("read back as %v, want %v", got.Enchantments(), c.stack.Enchantments())
			}
		})
	}
}

// An ench entry of an ID Bedrock does not have, such as 42 or 1000, and an
// adamant:ench entry of a Java ID no enchantment has, are left out. A Java
// ID without its namespace names the enchantment, and a level below 1 is 1.
func TestJavaOnlyEnchantmentNBTRead(t *testing.T) {
	tag := map[string]any{
		"ench": []any{
			map[string]any{"id": int16(42), "lvl": int16(2)},
			map[string]any{"id": int16(1000), "lvl": int16(1)},
			map[string]any{"id": int16(9), "lvl": int16(2)},
		},
		"adamant:ench": []any{
			map[string]any{"id": "sweeping_edge", "lvl": int16(0)},
			map[string]any{"id": "minecraft:wind_slash", "lvl": int16(1)},
		},
	}
	got := item.ReadTag(tag, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
	want := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithEnchantments(
		item.NewEnchantment(enchantment.Sharpness, 2), item.NewEnchantment(enchantment.SweepingEdge, 1))
	if !got.Equal(want) {
		t.Fatalf("read %v, want %v", got.Enchantments(), want.Enchantments())
	}
	if e := got.Enchantments(); e[0].Type() != enchantment.Sharpness || e[1].Type() != enchantment.SweepingEdge {
		t.Fatalf("Enchantments() orders %v, want Sharpness before Sweeping Edge", e)
	}
}

// Java item NBT names Sweeping Edge minecraft:sweeping_edge, in the
// enchantments component and in the stored enchantments of a book.
func TestSweepingEdgeJavaNBT(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.SweepingEdge, 2))
	data, err := item.StackToStructureNBT(sword)
	if err != nil {
		t.Fatal(err)
	}
	components, _ := data["components"].(map[string]any)
	if want := map[string]any{"minecraft:sweeping_edge": int32(2)}; !reflect.DeepEqual(components["minecraft:enchantments"], want) {
		t.Fatalf("enchantments component %v, want %v", components["minecraft:enchantments"], want)
	}
	if back, err := item.StackFromStructureNBT(data); err != nil || !slices.Equal(back.Enchantments(), sword.Enchantments()) {
		t.Fatalf("read back as %v (%v), want %v", back.Enchantments(), err, sword.Enchantments())
	}

	book := map[string]any{"id": "minecraft:enchanted_book", "count": int32(1), "components": map[string]any{
		"minecraft:stored_enchantments": map[string]any{"minecraft:sweeping_edge": int32(3)},
	}}
	if s, err := item.StackFromStructureNBT(book); err != nil {
		t.Fatal(err)
	} else if e, ok := s.Enchantment(enchantment.SweepingEdge); !ok || e.Level() != 3 {
		t.Fatalf("book holds %v, want Sweeping Edge III", s.Enchantments())
	}
}
