package session

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/dm-vev/adamant/net/nbt"
)

// mountEquipItem names an item of a slot of a mount's inventory screen; an
// Aux of 32767 matches every metadata value of the item.
type mountEquipItem struct {
	Aux  int16  `nbt:"Aux"`
	Name string `nbt:"Name"`
}

type mountEquipAccepted struct {
	SlotItem mountEquipItem `nbt:"slotItem"`
}

// mountEquipSlot is a slot of the screen: the items it takes, the item drawn
// in it while it is empty and its index in the window.
type mountEquipSlot struct {
	AcceptedItems []mountEquipAccepted `nbt:"acceptedItems"`
	Item          mountEquipItem       `nbt:"item"`
	SlotNumber    int32                `nbt:"slotNumber"`
}

type mountEquipSlots struct {
	Slots []mountEquipSlot `nbt:"slots"`
}

func mountEquipSlotOf(slot int32, icon string, accepted ...string) mountEquipSlot {
	s := mountEquipSlot{Item: mountEquipItem{Aux: 32767, Name: icon}, SlotNumber: slot}
	for _, name := range accepted {
		s.AcceptedItems = append(s.AcceptedItems, mountEquipAccepted{SlotItem: mountEquipItem{Aux: 32767, Name: name}})
	}
	return s
}

// mountEquipData holds the serialised inventory data of UpdateEquip for the
// three kinds of mount screens. BDS 1.26.52.3 writes there the slots of the
// mount's minecraft:equippable component, with the legacy item names of the
// behaviour pack: a saddle and horse armour for horses and zombie horses, a
// carpet in slot 1 for llamas, a saddle for the others. These are the bytes
// BDS sends for a mount that has not been equipped since it was loaded.
var mountEquipData = sync.OnceValue(func() map[string][]byte {
	saddle := mountEquipSlotOf(0, "minecraft:saddle", "minecraft:saddle")
	armour := mountEquipSlotOf(1, "minecraft:horsearmoriron", "minecraft:horsearmorleather", "minecraft:horsearmoriron",
		"minecraft:horsearmorgold", "minecraft:horsearmordiamond", "minecraft:copper_horse_armor", "minecraft:netherite_horse_armor")
	carpet := mountEquipSlotOf(1, "minecraft:carpet", "minecraft:carpet")
	data := map[string][]byte{}
	for kind, slots := range map[string][]mountEquipSlot{"horse": {saddle, armour}, "llama": {carpet}, "saddle": {saddle}} {
		b, err := nbt.MarshalEncoding(mountEquipSlots{Slots: slots}, nbt.NetworkLittleEndian)
		if err != nil {
			panic(err)
		}
		data[kind] = b
	}
	return data
})

// mountEquipDataOf returns the serialised inventory data of UpdateEquip for
// the mount passed.
func mountEquipDataOf(e world.Entity) []byte {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:horse", "minecraft:zombie_horse":
		return mountEquipData()["horse"]
	case "minecraft:llama", "minecraft:trader_llama":
		return mountEquipData()["llama"]
	}
	// Donkeys, mules, camels and camel husks; BDS gives skeleton horses no
	// equippable slots, but Adamant lets them be saddled.
	return mountEquipData()["saddle"]
}
