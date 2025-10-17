package item

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item/category"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"math"
	"math/rand/v2"
	"time"
)

const (
	loyaltyEnchantName    = "Loyalty"
	impalingEnchantName   = "Impaling"
	riptideEnchantName    = "Riptide"
	channelingEnchantName = "Channeling"
	unbreakingEnchantName = "Unbreaking"
	waterLiquidType       = "water"
)

// Trident is a powerful weapon that can be thrown or used in melee combat.
type Trident struct{}

// MaxCount always returns 1.
func (Trident) MaxCount() int {
	return 1
}

// AttackDamage returns the base melee damage of a trident.
func (Trident) AttackDamage() float64 {
	return 8
}

// EnchantmentValue returns the enchantment value of a trident.
func (Trident) EnchantmentValue() int {
	return 1
}

// DurabilityInfo returns the durability configuration for the trident.
func (Trident) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 250,
		BrokenItem:    simpleItem(Stack{}),
	}
}

// HandEquipped ensures the trident renders like a weapon when held.
func (Trident) HandEquipped() bool {
	return true
}

// RepairableBy reports if the trident can be repaired using the provided stack.
func (Trident) RepairableBy(i Stack) bool {
	return matchesItemName(i, "minecraft:trident")
}

// Category returns the creative inventory category the trident resides in.
func (Trident) Category() category.Category {
	return category.Equipment().WithGroup("itemGroup.name.sword")
}

// EncodeItem ...
func (Trident) EncodeItem() (name string, meta int16) {
	return "minecraft:trident", 0
}

// Release throws the trident or triggers a riptide launch when released after charging.
func (Trident) Release(releaser Releaser, tx *world.Tx, ctx *UseContext, duration time.Duration) {
	ticks := int(duration / (time.Second / 20))
	if ticks < 10 {
		return
	}

	held, _ := releaser.HeldItems()
	stack := held.Grow(0)

	loyaltyLevel := stackEnchantmentLevel(stack, loyaltyEnchantName)
	impalingLevel := stackEnchantmentLevel(stack, impalingEnchantName)
	riptideLevel := stackEnchantmentLevel(stack, riptideEnchantName)
	hasChanneling := stackEnchantmentLevel(stack, channelingEnchantName) > 0

	if riptideLevel > 0 {
		if !canUseRiptide(tx, releaser) {
			return
		}
		launchWithRiptide(releaser, tx, ctx, riptideLevel)
		return
	}

	force := math.Min(float64(ticks)/20, 1)
	if force < 0.1 {
		return
	}

	direction := releaser.Rotation().Vec3().Normalize()
	speed := 2.5 * force
	opts := world.EntitySpawnOpts{
		Position: eyePosition(releaser),
		Velocity: direction.Mul(speed),
		Rotation: releaser.Rotation().Neg(),
	}

	thrown := stack.Grow(-stack.Count() + 1)
	damage := 1
	if level := stackEnchantmentLevel(stack, unbreakingEnchantName); level > 0 {
		damage = reduceDamageWithUnbreaking(stack.Item(), level, damage)
	}
	if damage > 0 {
		thrown = thrown.Damage(damage)
	}
	if thrown.Empty() {
		ctx.SubtractFromCount(1)
		tx.PlaySound(releaser.Position(), sound.ItemBreak{})
		return
	}

	creative := releaser.GameMode().CreativeInventory()
	if creative {
		thrown = Stack{}
	}

	create := tx.World().EntityRegistry().Config().Trident
	if create == nil {
		return
	}
	tx.AddEntity(create(opts, releaser, thrown, loyaltyLevel, impalingLevel, hasChanneling))

	ctx.SubtractFromCount(1)
	tx.PlaySound(releaser.Position(), sound.TridentThrow{})
}

// Requirements returns the items required to release the trident.
func (Trident) Requirements() []Stack {
	return nil
}

func canUseRiptide(tx *world.Tx, releaser Releaser) bool {
	pos := cube.PosFromVec3(releaser.Position())
	if liquid, ok := tx.Liquid(pos); ok && isWaterLiquid(liquid) {
		return true
	}
	head := cube.PosFromVec3(eyePosition(releaser))
	if liquid, ok := tx.Liquid(head); ok && isWaterLiquid(liquid) {
		return true
	}
	return tx.RainingAt(pos)
}

func isWaterLiquid(liquid world.Liquid) bool {
	return liquid != nil && liquid.LiquidType() == waterLiquidType
}

func launchWithRiptide(releaser Releaser, tx *world.Tx, ctx *UseContext, level int) {
	ctx.DamageItem(1)
	direction := releaser.Rotation().Vec3().Normalize()
	speed := riptideLaunchVelocity(level)
	velocity := direction.Mul(speed)

	if mover, ok := releaser.(interface {
		Velocity() mgl64.Vec3
		SetVelocity(mgl64.Vec3)
	}); ok {
		velocity[1] = math.Max(velocity[1], 0.5)
		mover.SetVelocity(velocity)
	}

	tx.PlaySound(releaser.Position(), sound.TridentRiptide{Level: level})
}

func riptideLaunchVelocity(level int) float64 {
	switch level {
	case 3:
		return 5
	case 2:
		return 4
	default:
		return 3
	}
}

func stackEnchantmentLevel(stack Stack, name string) int {
	for _, enchant := range stack.Enchantments() {
		if enchant.Type().Name() == name {
			return enchant.Level()
		}
	}
	return 0
}

func reduceDamageWithUnbreaking(it world.Item, level, amount int) int {
	after := amount
	if level <= 0 {
		return after
	}
	_, isArmour := it.(Armour)
	for i := 0; i < amount; i++ {
		if (!isArmour || rand.Float64() >= 0.6) && rand.IntN(level+1) > 0 {
			after--
		}
	}
	return after
}
