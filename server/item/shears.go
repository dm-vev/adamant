package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// Shears is a tool used to shear sheep, mine a few types of blocks, and carve pumpkins.
type Shears struct{}

// UseOnBlock ...
func (s Shears) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if tip, ok := tx.Block(pos).(interface {
		ShearTip(cube.Pos, *world.Tx) bool
	}); ok && tip.ShearTip(pos, tx) {
		// ShearsItem.useOn: the tip of a growing plant stops growing.
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: "block.growing_plant.crop", Volume: 1, Pitch: 1})
		if user != nil {
			tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: user, AffectedState: tx.Block(pos)})
		}
		ctx.DamageItem(1)
		return true
	}
	direction := face
	if face == cube.FaceUp || face == cube.FaceDown {
		direction = user.Rotation().Direction().Opposite().Face()
	}
	blockName, _ := tx.Block(pos).EncodeBlock()
	if c, ok := tx.Block(pos).(carvable); ok {
		if res, ok := c.Carve(direction); ok {
			if blockName == "minecraft:pumpkin" {
				if seeds, ok := world.ItemByName("minecraft:pumpkin_seeds", 0); ok {
					stack := NewStack(seeds, 4)
					offset, velocity := mgl64.Vec3{}, mgl64.Vec3{0, .05}
					switch direction {
					case cube.FaceNorth:
						offset[2], velocity[2] = -.65, -.05
					case cube.FaceSouth:
						offset[2], velocity[2] = .65, .05
					case cube.FaceWest:
						offset[0], velocity[0] = -.65, -.05
					case cube.FaceEast:
						offset[0], velocity[0] = .65, .05
					}
					dropPosition := pos.Vec3Middle().Add(offset)
					dropPosition[1] += .1
					if dropper, ok := user.(interface {
						DropItemAt(mgl64.Vec3, Stack, mgl64.Vec3)
					}); ok {
						dropper.DropItemAt(dropPosition, stack, velocity)
					} else {
						ctx.NewItem = stack
						ctx.NewItemSurvivalOnly = true
					}
				}
				tx.PlaySound(pos.Vec3Middle(), sound.Custom{Name: "block.pumpkin.carve", Volume: 1, Pitch: 1})
			}
			if dropProvider, ok := tx.Block(pos).(interface{ ShearDrops() Stack }); ok {
				if drops := dropProvider.ShearDrops(); !drops.Empty() {
					dropPos := pos.Vec3Middle()
					dropPos[1] += 0.5
					if dropper, ok := user.(interface {
						DropItemAt(mgl64.Vec3, Stack, mgl64.Vec3)
					}); ok {
						dropper.DropItemAt(dropPos, drops, mgl64.Vec3{0, 0.1})
					} else {
						ctx.NewItem = drops
						ctx.NewItemSurvivalOnly = true
					}
					if blockName == "minecraft:beehive" || blockName == "minecraft:bee_nest" {
						tx.PlaySound(pos.Vec3Middle(), sound.Custom{Name: "block.beehive.shear", Volume: 1, Pitch: 1})
					}
				}
			}
			tx.SetBlock(pos, res, nil)
			if h, ok := c.(honeyHarvester); ok {
				h.HoneyHarvested(pos, tx, user)
			}
			ctx.DamageItem(1)
			return true
		}
	}
	return false
}

// UseOnEntity shears a harness from a compatible rideable mob.
func (s Shears) UseOnEntity(target world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	if mob, ok := target.(interface {
		ShearEquipment(*world.Tx, world.Entity) bool
	}); ok && !userSneaking(user) && mob.ShearEquipment(tx, user) {
		// Mob.attemptToShearEquipment.
		ctx.DamageItem(1)
		return true
	}
	if mob, ok := target.(interface {
		ShearHarness(*world.Tx, world.Entity) bool
	}); ok && mob.ShearHarness(tx, user) {
		ctx.DamageItem(1)
		return true
	}
	if mob, ok := target.(interface {
		CanShearBodyArmor(world.Entity) bool
		RemoveBodyArmor(*world.Tx) Stack
	}); ok && mob.CanShearBodyArmor(user) {
		if armour := mob.RemoveBodyArmor(tx); !armour.Empty() {
			ctx.NewItem = armour
			ctx.NewItemSurvivalOnly = true
			ctx.DamageItem(1)
			shearSound := world.Sound(sound.Custom{Name: "item.armor.unequip_nautilus", Volume: 1, Pitch: 1})
			if soundProvider, ok := target.(interface{ ArmorShearingSound() world.Sound }); ok {
				shearSound = soundProvider.ArmorShearingSound()
			}
			tx.PlaySound(target.Position(), shearSound)
			return true
		}
	}
	if mob, ok := target.(interface{ Shear(*world.Tx) Stack }); ok {
		if drops := mob.Shear(tx); !drops.Empty() {
			if dropper, ok := user.(interface {
				DropItemAt(mgl64.Vec3, Stack, mgl64.Vec3)
			}); ok {
				dropPos := target.Position()
				dropPos[1] += 0.5
				dropper.DropItemAt(dropPos, drops, mgl64.Vec3{0, 0.1})
			} else {
				ctx.NewItem = drops
				ctx.NewItemSurvivalOnly = true
			}
			ctx.DamageItem(1)
			tx.PlaySound(target.Position(), sound.Custom{Name: "entity.sheep.shear", Volume: 1, Pitch: 1})
			return true
		}
	}
	return false
}

// carvable represents a block that may be carved by using shears on it.
type carvable interface {
	// Carve returns the resulting block of carving this block. If carving it has no result, Carve returns false.
	Carve(f cube.Face) (world.Block, bool)
}

// ToolType ...
func (s Shears) ToolType() ToolType {
	return TypeShears
}

// HarvestLevel ...
func (s Shears) HarvestLevel() int {
	return 1
}

// BaseMiningEfficiency ...
func (s Shears) BaseMiningEfficiency(world.Block) float64 {
	return 1.5
}

// DurabilityInfo ...
func (s Shears) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability:    238,
		BrokenItem:       simpleItem(Stack{}),
		AttackDurability: 0,
		BreakDurability:  1,
	}
}

// MaxCount ...
func (s Shears) MaxCount() int {
	return 1
}

// EncodeItem ...
func (s Shears) EncodeItem() (name string, meta int16) {
	return "minecraft:shears", 0
}

// userSneaking reports whether the user is sneaking, which keeps shears
// from shearing the equipment of a mob.
func userSneaking(user User) bool {
	s, ok := user.(interface{ Sneaking() bool })
	return ok && s.Sneaking()
}
