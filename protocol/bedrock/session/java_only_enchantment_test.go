package session

import (
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/menu"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/net/nbt"
)

// sentEnchantmentIDs returns the IDs of the ench list of item NBT, and fails
// if one of them is not of a registered enchantment: a client must never be
// sent an ID it may not know, or know as another enchantment.
func sentEnchantmentIDs(t *testing.T, tag map[string]any) []int16 {
	t.Helper()
	var ids []int16
	list, _ := tag["ench"].([]map[string]any)
	for _, e := range list {
		id, _ := e["id"].(int16)
		if _, ok := item.EnchantmentByID(int(id)); !ok {
			t.Fatalf("ench holds ID %d, which no enchantment has", id)
		}
		ids = append(ids, id)
	}
	return ids
}

// A client is not sent Sweeping Edge, which Bedrock has no ID of, in ench.
// Its item NBT names it in a grey lore line before the lore of the stack,
// and an empty ench keeps the glint of a stack of Sweeping Edge alone. The
// item NBT keeps adamant:ench, and what a client sends back of it reads as
// the stack it was sent, which the checks of the held item need.
func TestNetworkItemNBTJavaOnlyEnchantments(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1)
	for _, c := range []struct {
		name  string
		stack item.Stack
		ench  []int16
		lore  []string
	}{
		{
			name:  "alone",
			stack: sword.WithEnchantments(item.NewEnchantment(enchantment.SweepingEdge, 2)),
			lore:  []string{"§r§7Sweeping Edge II"},
		},
		{
			name: "with Sharpness and lore",
			stack: sword.WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 4), item.NewEnchantment(enchantment.SweepingEdge, 3)).
				WithLore("§r§7Sweeping Edge III", "own").Damage(7),
			ench: []int16{9},
			lore: []string{"§r§7Sweeping Edge III", "§r§7Sweeping Edge III", "own"},
		},
		{
			name:  "on a book",
			stack: item.NewStack(item.Book{}, 1).WithEnchantments(item.NewEnchantment(enchantment.SweepingEdge, 1)),
			lore:  []string{"§r§7Sweeping Edge I"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			tag := networkItemNBT(c.stack)
			if _, ok := tag["ench"]; !ok {
				t.Fatal("no ench: the item has no glint")
			}
			if ids := sentEnchantmentIDs(t, tag); !reflect.DeepEqual(ids, c.ench) {
				t.Errorf("ench holds %v, want %v", ids, c.ench)
			}
			display, _ := tag["display"].(map[string]any)
			if lore := display["Lore"]; !reflect.DeepEqual(lore, c.lore) {
				t.Errorf("lore %q, want %q", lore, c.lore)
			}
			if _, ok := tag["adamant:ench"]; !ok {
				t.Error("no adamant:ench")
			}

			// What the client sends back, through the encoding of the wire.
			b, err := nbt.Marshal(tag)
			if err != nil {
				t.Fatal(err)
			}
			var back map[string]any
			if err := nbt.Unmarshal(b, &back); err != nil {
				t.Fatal(err)
			}
			if got := readNetworkItemNBT(back, item.NewStack(c.stack.Item(), c.stack.Count())); !got.Equal(c.stack) {
				t.Errorf("read back with %v and lore %q, want %v and %q", got.Enchantments(), got.Lore(), c.stack.Enchantments(), c.stack.Lore())
			}
		})
	}
}

// The item NBT of a stack without Java-only enchantments is what it was.
func TestNetworkItemNBTWithoutJavaOnlyEnchantments(t *testing.T) {
	s := item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1).WithEnchantments(item.NewEnchantment(enchantment.Sharpness, 1)).WithLore("own")
	tag := networkItemNBT(s)
	want := item.WriteTag(s)
	want["Damage"] = int32(0)
	if !reflect.DeepEqual(tag, want) {
		t.Fatalf("item NBT %v, want %v", tag, want)
	}
	// Lore a client sends of a stack of no Java-only enchantments stays,
	// even the line of one.
	back := readNetworkItemNBT(map[string]any{"display": map[string]any{"Lore": []any{"§r§7Sweeping Edge II"}}}, item.NewStack(item.Stick{}, 1))
	if lore := back.Lore(); !reflect.DeepEqual(lore, []string{"§r§7Sweeping Edge II"}) {
		t.Fatalf("lore read as %q", lore)
	}
}

// A trade offer of a book of Sweeping Edge names it in its lore.
func TestTradeItemNBTJavaOnlyEnchantments(t *testing.T) {
	book := item.NewStack(item.Book{}, 1).WithEnchantments(item.NewEnchantment(enchantment.SweepingEdge, 3))
	m := tradeItemNBT(book)
	tag, _ := m["tag"].(map[string]any)
	display, _ := tag["display"].(map[string]any)
	if lore := display["Lore"]; !reflect.DeepEqual(lore, []string{"§r§7Sweeping Edge III"}) {
		t.Fatalf("lore %q", lore)
	}
	sentEnchantmentIDs(t, tag)
	if m["Name"] != "minecraft:enchanted_book" {
		t.Fatalf("name %v", m["Name"])
	}
}

// The levels of the lore lines are those of Java's enchantment.level.N
// texts, I to X, and numbers past them.
func TestEnchantmentLevel(t *testing.T) {
	for level, want := range map[int]string{1: "I", 3: "III", 4: "IV", 9: "IX", 10: "X", 11: "11", 255: "255"} {
		if got := enchantmentLevel(level); got != want {
			t.Errorf("level %d: %q, want %q", level, got, want)
		}
	}
}

// The enchanting table sends a client the enchantments of an option it has
// IDs of. Sweeping Edge is left out: an option of it alone is sent with its
// cost and no enchantment.
func TestViewEnchantmentOptionsLeavesOutJavaOnly(t *testing.T) {
	s := testSession("enchanter")
	s.ViewEnchantmentOptions([3]menu.EnchantmentOption{
		{Cost: 3, Enchantments: []item.Enchantment{item.NewEnchantment(enchantment.Sharpness, 1), item.NewEnchantment(enchantment.SweepingEdge, 1)}},
		{Cost: 9, Enchantments: []item.Enchantment{item.NewEnchantment(enchantment.SweepingEdge, 2)}},
		{Cost: 20, Enchantments: []item.Enchantment{item.NewEnchantment(enchantment.SweepingEdge, 3), item.NewEnchantment(enchantment.Unbreaking, 2)}},
	})
	pk, ok := s.testPacket().(*mc.PlayerEnchantOptionsPacket)
	if !ok {
		t.Fatal("no PlayerEnchantOptions")
	}
	want := [][]mc.EnchantmentInstance{{{Type: 9, Level: 1}}, {}, {{Type: 17, Level: 2}}}
	if len(pk.Options) != 3 {
		t.Fatalf("%d options", len(pk.Options))
	}
	for i, o := range pk.Options {
		if o.Cost != []uint8{3, 9, 20}[i] || o.RecipeNetworkID != uint32(i) || o.Enchantments.Slot != int32(i) {
			t.Errorf("option %d: cost %d, recipe %d, slot %d", i, o.Cost, o.RecipeNetworkID, o.Enchantments.Slot)
		}
		if got := o.Enchantments.Enchantments[1]; !reflect.DeepEqual(got, want[i]) {
			t.Errorf("option %d shows %v, want %v", i, got, want[i])
		}
	}
}
