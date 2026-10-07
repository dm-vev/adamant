package entity

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
)

// applyTrialEquipment applies the EquipmentTable carried separately from a
// trial SpawnData entity compound before the mob is inserted into its world.
func applyTrialEquipment(opts world.EntitySpawnOpts, data *world.EntityData, id string) error {
	spawnData := opts.SpawnData
	if spawnData == nil { return nil }
	equipment, _ := spawnData["equipment"].(map[string]any)
	if equipment == nil { return nil }
	table, _ := equipment["loot_table"].(string)
	if table == "" { return nil }
	seed := opts.SpawnSeed
	if seed == 0 { seed = 1 }
	random := java26.NewLegacyRandom(seed)
	stacks, err := loot.Generate(table, seed, loot.Context{Random: &random, Origin: opts.Position, Entity: id})
	if err != nil { return fmt.Errorf("generate trial equipment from %q: %w", table, err) }
	native := CloneNativeMobNBT(data.NativeNBT)
	if native == nil { native = make(map[string]any) }
	gear := ReadNativeMobEquipment(native)
	if gear.Armor == nil { gear.Armor = make([]NativeMobItem, 4) }
	used := map[int]bool{}
	for _, stack := range stacks {
		if stack.Empty() { continue }
		slot := trialEquipmentSlot(stack.Item())
		if used[slot] { continue }
		entry, err := nativeMobEquipmentItem(stack)
		if err != nil { return err }
		used[slot] = true
		switch {
		case slot >= 0 && slot < 4:
			gear.Armor[slot] = entry
		case slot == 4:
			gear.Mainhand = []NativeMobItem{entry}
		case slot == 5:
			gear.Offhand = []NativeMobItem{entry}
		}
	}
	for key, value := range EncodeNativeMobEquipment(gear) { native[key] = value }
	data.NativeNBT = native
	return nil
}

// Java EquipmentUser resolves explicit equippable armor slots first, then puts
// other items in main hand. The native Bedrock actor arrays order armor from
// head through feet.
func trialEquipmentSlot(it world.Item) int {
	switch it.(type) {
	case item.HelmetType:
		return 0
	case item.ChestplateType:
		return 1
	case item.LeggingsType:
		return 2
	case item.BootsType:
		return 3
	case item.Shield:
		return 5
	}
	name, _ := it.EncodeItem()
	name = strings.TrimPrefix(name, "minecraft:")
	switch {
	case strings.HasSuffix(name, "_helmet"), strings.HasSuffix(name, "_head"), strings.HasSuffix(name, "_skull"):
		return 0
	case strings.HasSuffix(name, "_chestplate"):
		return 1
	case strings.HasSuffix(name, "_leggings"):
		return 2
	case strings.HasSuffix(name, "_boots"):
		return 3
	case name == "shield":
		return 5
	default:
		return 4
	}
}

func nativeMobEquipmentItem(stack item.Stack) (NativeMobItem, error) {
	raw := item.WriteNBT(stack, true)
	name, ok := raw["Name"].(string)
	if !ok || name == "" { return NativeMobItem{}, fmt.Errorf("trial equipment item has no native Bedrock name") }
	return NativeMobItem{Name: name, Count: nativeNBTByte(raw["Count"]), Damage: int32(nbtconv.Int32(raw, "Damage")), WasPickedUp: 0, Raw: raw}, nil
}
