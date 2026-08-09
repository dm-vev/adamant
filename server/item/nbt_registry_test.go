package item

import (
	"math"
	"reflect"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

func TestBlockItemNBTRoundTripUsesRegistry(t *testing.T) {
	RegisterEnchantment(255, testEnchantment{})

	registry := world.NewBlockRegistry()
	for _, powered := range []bool{false, true} {
		b := registryBlockItem{Powered: powered}
		name, states := b.EncodeBlock()
		registry.RegisterBlockState(world.BlockState{Name: name, Properties: states})
		registry.RegisterBlock(b)
	}
	registry.Finalize()

	want := NewStack(registryBlockItem{Powered: true}, 3).WithForcedEnchantments(NewEnchantment(testEnchantment{}, 3))
	encoded := WriteNBT(want, true)
	got := ReadNBT(encoded, nil, registry)
	if got.Count() != want.Count() {
		t.Fatalf("count = %d, want %d", got.Count(), want.Count())
	}
	if gotItem, ok := got.Item().(registryBlockItem); !ok || !gotItem.Powered {
		t.Fatalf("item = %#v, want powered registryBlockItem", got.Item())
	}
	if reencoded := WriteNBT(got, true); !reflect.DeepEqual(reencoded, encoded) {
		t.Fatalf("round-trip NBT differs\ngot:  %#v\nwant: %#v", reencoded, encoded)
	}
}

func TestReadNBTClampsInvalidEnchantmentLevels(t *testing.T) {
	RegisterEnchantment(255, testEnchantment{})

	for _, test := range []struct {
		name  string
		data  map[string]any
		stack *Stack
		level int16
	}{
		{
			name:  "client zero",
			data:  map[string]any{"ench": []map[string]any{{"id": int16(255), "lvl": int16(0)}}},
			stack: ptr(NewStack(Apple{}, 1)),
			level: 0,
		},
		{
			name:  "client negative",
			data:  map[string]any{"ench": []map[string]any{{"id": int16(255), "lvl": int16(-2)}}},
			stack: ptr(NewStack(Apple{}, 1)),
			level: -2,
		},
		{
			name: "persisted zero",
			data: map[string]any{
				"Name": "minecraft:apple", "Damage": int16(0), "Count": byte(1),
				"tag": map[string]any{"ench": []map[string]any{{"id": int16(255), "lvl": int16(0)}}},
			},
			level: 0,
		},
		{
			name: "persisted negative",
			data: map[string]any{
				"Name": "minecraft:apple", "Damage": int16(0), "Count": byte(1),
				"tag": map[string]any{"ench": []map[string]any{{"id": int16(255), "lvl": int16(-2)}}},
			},
			level: -2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := ReadNBT(test.data, test.stack)
			enchantments := got.Enchantments()
			if len(enchantments) != 1 || enchantments[0].Level() != 1 {
				t.Fatalf("enchantments = %#v, want level 1 after clamping %d", enchantments, test.level)
			}
		})
	}
}

type registryBlockItem struct {
	Powered bool
}

func (b registryBlockItem) EncodeBlock() (string, map[string]any) {
	return "test:registry_block", map[string]any{"powered": b.Powered}
}

func (registryBlockItem) EncodeItem() (string, int16) { return "test:registry_block", 0 }
func (registryBlockItem) Hash() (uint64, uint64)      { return 0, math.MaxUint64 }
func (registryBlockItem) Model() world.BlockModel     { return nil }

type testEnchantment struct{}

func (testEnchantment) Name() string                                   { return "test" }
func (testEnchantment) MaxLevel() int                                  { return 3 }
func (testEnchantment) Cost(int) (int, int)                            { return 0, 0 }
func (testEnchantment) Rarity() EnchantmentRarity                      { return EnchantmentRarityCommon }
func (testEnchantment) CompatibleWithEnchantment(EnchantmentType) bool { return true }
func (testEnchantment) CompatibleWithItem(world.Item) bool             { return true }

func ptr(s Stack) *Stack { return &s }
