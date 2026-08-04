package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
)

// NewArmourStand creates a new armour stand entity in the world with the given spawn options.
func NewArmourStand(opts world.EntitySpawnOpts) *world.EntityHandle {
	conf := armourStandConf
	conf.Armour = inventory.NewArmour(nil)
	return opts.New(ArmourStandType, conf)
}

var armourStandConf = ArmourStandBehaviourConfig{}

// ArmourStandType is a world.EntityType implementation for armour stands.
var ArmourStandType armourStandType

type armourStandType struct{}

func (armourStandType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (armourStandType) EncodeEntity() string { return "minecraft:armor_stand" }

func (armourStandType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.25, 0, -0.25, 0.25, 1.975, 0.25)
}

func (armourStandType) DecodeNBT(m map[string]any, data *world.EntityData) {
	poseIndex := int(nbtconv.Int32(m, "PoseIndex"))
	if poseIndex < 0 {
		poseIndex = 0
	}

	conf := ArmourStandBehaviourConfig{
		Armour:    inventory.NewArmour(nil),
		PoseIndex: poseIndex % 13,
		MainHand:  item.MapNBT(m, "MainHand"),
		OffHand:   item.MapNBT(m, "Offhand"),
	}

	armours := nbtconv.Slice(m, "Armor")
	for i := 0; i < 4 && i < len(armours); i++ {
		itemMap, ok := armours[i].(map[string]any)
		if !ok {
			continue
		}
		it := item.ReadNBT(itemMap, nil)

		switch i {
		case 0:
			conf.Armour.SetHelmet(it)
		case 1:
			conf.Armour.SetChestplate(it)
		case 2:
			conf.Armour.SetLeggings(it)
		case 3:
			conf.Armour.SetBoots(it)
		}
	}
	data.Data = conf.New()
}

func (armourStandType) EncodeNBT(data *world.EntityData) map[string]any {
	a, ok := data.Data.(*ArmourStandBehaviour)
	if !ok {
		return map[string]any{}
	}
	return map[string]any{
		"MainHand": item.WriteNBT(a.conf.MainHand, true),
		"Offhand":  item.WriteNBT(a.conf.OffHand, true),
		"Armor": []map[string]any{
			item.WriteNBT(a.Armour().Helmet(), true),
			item.WriteNBT(a.Armour().Chestplate(), true),
			item.WriteNBT(a.Armour().Leggings(), true),
			item.WriteNBT(a.Armour().Boots(), true),
		},
		"PoseIndex": int32(a.conf.PoseIndex),
	}
}
