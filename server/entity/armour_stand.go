package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
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
	e := &Ent{tx: tx, handle: handle, data: data}
	if b, ok := data.Data.(*ArmourStandBehaviour); ok && !b.collisionInitialised {
		b.collisionInitialised = true
		b.passive.mc.CheckCollision(tx, e, data.Pos, mgl64.Vec3{})
		b.passive.mc.nativeBoxSet = false
		b.passive.mc.onGround = true
		data.OnGround = true
	}
	return e
}

func (armourStandType) EncodeEntity() string { return "minecraft:armor_stand" }

// TickAfterBlocks follows native ECS mob travel, which runs after dimension
// block ticks.
func (armourStandType) TickAfterBlocks() bool { return true }

func (armourStandType) CollisionWidth() float64 { return .5 }
func (armourStandType) BBox(e world.Entity) cube.BBox {
	if ent, ok := e.(*Ent); ok {
		if b, ok := ent.data.Data.(*ArmourStandBehaviour); ok {
			c := b.passive.mc
			if c.nativeBoxSet && ent.Position() == c.nativeBoxPosition {
				lo, hi := c.nativeBox.Min(), c.nativeBox.Max()
				return cube.Box(float64(lo[0]), float64(lo[1]), float64(lo[2]), float64(hi[0]), float64(hi[1]), float64(hi[2])).Translate(ent.Position().Mul(-1))
			}
		}
	}
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
		it := item.ReadNBT(itemMap, nil, world.BlockRegistryFromNBT(m))

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
