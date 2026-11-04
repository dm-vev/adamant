package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

// itemNBTCorpusFile holds, for every registered item, the hash of the item
// NBT of its corpus stacks (itemNBTCorpus) on Bedrock's wire and on disk, and
// of the stacks read back from both. It was recorded with item.WriteNBT(s,
// false), WriteNBT(s) and ReadNBT before the wire's item NBT moved to
// the adapter.
const itemNBTCorpusFile = "testdata/item_nbt/corpus.snapshot"

// itemNBTCorpus returns the stacks of an item the corpus encodes: the item
// alone, damaged, named with lore, enchanted, locked both ways, unbreakable,
// with an anvil cost, with dragonfly data, and with all of these at once.
func itemNBTCorpus(it world.Item) []item.Stack {
	base := item.NewStack(it, 1)
	var enchants []item.Enchantment
	for _, id := range []int{9, 17} { // Sharpness, Unbreaking.
		if t, ok := item.EnchantmentByID(id); ok {
			enchants = append(enchants, item.NewEnchantment(t, 3))
		}
	}
	stacks := []item.Stack{
		base,
		base.WithCustomName("Corpus §aname").WithLore("first", "second"),
		base.WithForcedEnchantments(enchants...),
		base.WithLockMode(item.LockInInventory),
		base.WithLockMode(item.LockInSlot),
		base.AsUnbreakable(),
		base.WithAnvilCost(5),
		base.WithValue("corpus", "value").WithValue("count", int64(3)),
	}
	all := base.Grow(2).WithCustomName("All").WithLore("lore").WithForcedEnchantments(enchants...).
		WithLockMode(item.LockInSlot).WithAnvilCost(7).WithValue("corpus", "all")
	if max := base.MaxDurability(); max > 1 {
		stacks = append(stacks, base.Damage(1), base.Damage(max-1))
		all = all.Damage(max / 2)
	}
	stacks = append(stacks, all, all.AsUnbreakable())
	return stacks
}

// dumpNBT writes a value decoded from or encoded to NBT with the Go type of
// every value, the keys of maps sorted, so that two dumps are equal exactly
// when the values deep-equal.
func dumpNBT(b *strings.Builder, v any) {
	switch v := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteString("map{")
		for _, k := range keys {
			fmt.Fprintf(b, "%q:", k)
			dumpNBT(b, v[k])
			b.WriteByte(',')
		}
		b.WriteByte('}')
	case []byte:
		fmt.Fprintf(b, "[]uint8(%x)", v)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			fmt.Fprintf(b, "%T[", v)
			for i := 0; i < rv.Len(); i++ {
				dumpNBT(b, rv.Index(i).Interface())
				b.WriteByte(',')
			}
			b.WriteByte(']')
			return
		}
		fmt.Fprintf(b, "%T(%#v)", v, v)
	}
}

// itemNBTCorpusDump dumps the corpus of an item: the network and disk NBT of
// each stack, and the disk NBT and count of the stack read back from each.
func itemNBTCorpusDump(it world.Item) string {
	br := world.DefaultBlockRegistry
	var b strings.Builder
	for i, s := range itemNBTCorpus(it) {
		if s.Empty() {
			// Stacks of air are empty.
			fmt.Fprintf(&b, "%d empty\n", i)
			continue
		}
		network, disk := corpusNetworkNBT(s), corpusDiskNBT(s)
		fromNetwork := corpusReadNetworkNBT(network, item.NewStack(s.Item(), s.Count()), br)
		fromDisk := corpusReadDiskNBT(disk, br)
		fmt.Fprintf(&b, "%d network ", i)
		dumpNBT(&b, network)
		b.WriteString("\n  disk ")
		dumpNBT(&b, disk)
		fmt.Fprintf(&b, "\n  read from network %d ", fromNetwork.Count())
		dumpNBT(&b, corpusDiskNBT(fromNetwork))
		fmt.Fprintf(&b, "\n  read from disk %d ", fromDisk.Count())
		dumpNBT(&b, corpusDiskNBT(fromDisk))
		b.WriteByte('\n')
	}
	return b.String()
}

// itemNBTCorpusHashes returns the hash of the corpus dump of every registered
// item by its name and metadata, and the dumps themselves.
func itemNBTCorpusHashes() (map[string]string, map[string]string) {
	hashes, dumps := map[string]string{}, map[string]string{}
	for _, it := range world.Items() {
		name, meta := it.EncodeItem()
		key := fmt.Sprintf("%s %d", name, meta)
		dump := itemNBTCorpusDump(it)
		sum := sha256.Sum256([]byte(dump))
		hashes[key], dumps[key] = hex.EncodeToString(sum[:16]), dump
	}
	return hashes, dumps
}

// TestItemNBTCorpus holds the item NBT of Bedrock's wire, which the adapter
// writes and reads, and the world format's item NBT, which the game writes
// and reads, to the corpus recorded before the wire's NBT left the game:
// every registered item crossed with damage, a name and lore, enchantments,
// locks, unbreakable, an anvil cost and dragonfly data, encoded and read back.
// ADAMANT_RECORD_ITEM_NBT=1 records the corpus anew.
func TestItemNBTCorpus(t *testing.T) {
	hashes, dumps := itemNBTCorpusHashes()
	keys := make([]string, 0, len(hashes))
	for k := range hashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if os.Getenv("ADAMANT_RECORD_ITEM_NBT") == "1" {
		var b strings.Builder
		fmt.Fprintf(&b, "# item NBT corpus: %d items\n", len(keys))
		for _, k := range keys {
			fmt.Fprintf(&b, "%s %s\n", k, hashes[k])
		}
		if err := os.WriteFile(itemNBTCorpusFile, []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	f, err := os.Open(itemNBTCorpusFile)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		i := strings.LastIndexByte(line, ' ')
		want[line[:i]] = line[i+1:]
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(want) != len(hashes) {
		t.Errorf("%d items registered, the corpus has %d", len(hashes), len(want))
	}
	failed := 0
	for _, k := range keys {
		if w, ok := want[k]; !ok {
			t.Errorf("%s is not in the corpus", k)
		} else if w != hashes[k] {
			if failed++; failed <= 3 {
				t.Errorf("%s: corpus hash %s, recorded %s; now:\n%s", k, hashes[k], w, dumps[k])
			} else {
				t.Errorf("%s: corpus hash %s, recorded %s", k, hashes[k], w)
			}
		}
	}
}

// ownDamageItem is a durable item whose NBT may hold a damage of its own, as a
// plugin's item may.
type ownDamageItem struct {
	damage        any
	maxDurability int
}

func (i ownDamageItem) EncodeItem() (string, int16)  { return "test:own_damage", 0 }
func (i ownDamageItem) DecodeNBT(map[string]any) any { return i }
func (i ownDamageItem) EncodeNBT() map[string]any {
	if i.damage == nil {
		return nil
	}
	return map[string]any{"Damage": i.damage}
}
func (i ownDamageItem) DurabilityInfo() item.DurabilityInfo {
	return item.DurabilityInfo{MaxDurability: i.maxDurability, BrokenItem: func() item.Stack { return item.Stack{} }}
}

// plainDamageItem is an item that is not durable, whose NBT holds a damage.
type plainDamageItem struct{}

func (plainDamageItem) EncodeItem() (string, int16)    { return "test:plain_damage", 0 }
func (i plainDamageItem) DecodeNBT(map[string]any) any { return i }
func (plainDamageItem) EncodeNBT() map[string]any      { return map[string]any{"Damage": int16(0)} }

// TestNetworkItemNBTDamage checks the damage networkItemNBT writes where the
// corpus of registered items has none to show: the items of plugins whose NBT
// holds a damage, and damage beyond an int16. Each wants what
// item.WriteNBT(s, false) wrote: an int32 of the damage of a durable item,
// unless the NBT of its item holds a damage other than zero.
func TestNetworkItemNBTDamage(t *testing.T) {
	for _, c := range []struct {
		name  string
		stack item.Stack
		want  any
	}{
		{"durable, no own damage", item.NewStack(ownDamageItem{maxDurability: 100}, 1).Damage(3), int32(3)},
		{"durable, own zero int32", item.NewStack(ownDamageItem{damage: int32(0), maxDurability: 100}, 1).Damage(3), int32(3)},
		{"durable, own zero int16, undamaged", item.NewStack(ownDamageItem{damage: int16(0), maxDurability: 100}, 1), int32(0)},
		{"durable, own int16 equal to the damage", item.NewStack(ownDamageItem{damage: int16(3), maxDurability: 100}, 1).Damage(3), int16(3)},
		{"durable, own int16 other than the damage", item.NewStack(ownDamageItem{damage: int16(7), maxDurability: 100}, 1).Damage(3), int16(7)},
		{"durable, own string", item.NewStack(ownDamageItem{damage: "x", maxDurability: 100}, 1).Damage(3), "x"},
		{"durable beyond an int16", item.NewStack(ownDamageItem{maxDurability: 100000}, 1).Damage(50000), int32(50000)},
		{"not durable, own zero int16", item.NewStack(plainDamageItem{}, 1), int16(0)},
	} {
		if got := networkItemNBT(c.stack)["Damage"]; got != c.want {
			t.Errorf("%s: Damage %T(%v), want %T(%v)", c.name, got, got, c.want, c.want)
		}
	}
}

// TestReadNetworkItemNBTDamage checks the damage readNetworkItemNBT reads
// into a new stack where the corpus has none to show, as ReadNBT(data, &s)
// read it: an int32 damage of any size, and a damage of another type as no
// damage. It also checks that the NBT passed is left as it is.
func TestReadNetworkItemNBTDamage(t *testing.T) {
	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1)
	huge := item.NewStack(ownDamageItem{maxDurability: 100000}, 1)
	for _, c := range []struct {
		name       string
		stack      item.Stack
		tag        map[string]any
		empty      bool
		durability int
	}{
		{"no NBT", sword, nil, false, sword.MaxDurability()},
		{"int32", sword, map[string]any{"Damage": int32(5)}, false, sword.MaxDurability() - 5},
		{"int16", sword, map[string]any{"Damage": int16(5)}, false, sword.MaxDurability()},
		{"int32 above an int16", sword, map[string]any{"Damage": int32(40000)}, true, 0},
		{"int32 below an int16", sword, map[string]any{"Damage": int32(-40000)}, false, sword.MaxDurability()},
		{"unbreakable after the damage", sword, map[string]any{"Damage": int32(5), "Unbreakable": byte(1)}, false, sword.MaxDurability() - 5},
		{"above an int16, more durable", huge, map[string]any{"Damage": int32(50000), "RepairCost": int32(2)}, false, 50000},
		{"above its durability, more durable", huge, map[string]any{"Damage": int32(200000)}, true, 0},
		{"below an int16, more durable", huge, map[string]any{"Damage": int32(-50000)}, false, 100000},
	} {
		before := fmt.Sprint(c.tag)
		got := readNetworkItemNBT(c.tag, c.stack)
		if after := fmt.Sprint(c.tag); after != before {
			t.Errorf("%s: NBT changed from %s to %s", c.name, before, after)
		}
		if got.Empty() != c.empty || (!c.empty && got.Durability() != c.durability) {
			t.Errorf("%s: empty %v, durability %d; want empty %v, durability %d", c.name, got.Empty(), got.Durability(), c.empty, c.durability)
		}
	}
}
