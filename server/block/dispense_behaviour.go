package block

import (
	"math"
	"reflect"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// dispenseSource is what a dispense behaviour acts from, as Java's
// BlockSource: the dispenser, its position and the stack dispensed.
type dispenseSource struct {
	d     Dispenser
	pos   cube.Pos
	tx    *world.Tx
	stack item.Stack
}

// dispenseBehaviour is a DispenseItemBehavior of Java: it dispenses the stack
// of src and returns the stack its slot holds afterwards. With ok false, the
// slot is left as it was and comparators are not told.
type dispenseBehaviour func(src dispenseSource) (left item.Stack, ok bool)

// dispenseSpawner creates the entity a behaviour spawns for the item it.
type dispenseSpawner func(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle

// dispenseBehaviours holds the behaviour of each item type that has one, as
// Java's DispenserBlock.DISPENSER_REGISTRY, filled by
// DispenseItemBehavior.bootStrap. Items of other types are dropped
// (dispenseDefault).
var dispenseBehaviours = map[reflect.Type]dispenseBehaviour{
	// ProjectileDispenseBehavior, registered by registerProjectileBehavior.
	reflect.TypeFor[item.Egg]():                dispenseProjectile(spawnDispensedEgg),
	reflect.TypeFor[item.Snowball]():           dispenseProjectile(spawnDispensedSnowball),
	reflect.TypeFor[item.FireCharge]():         dispenseProjectile(spawnDispensedFireCharge),
	reflect.TypeFor[item.WindCharge]():         dispenseProjectile(spawnDispensedWindCharge),
	reflect.TypeFor[item.Arrow]():              dispenseShot(defaultDispenseConfig, spawnDispensedArrow),
	reflect.TypeFor[item.BottleOfEnchanting](): dispenseShot(thrownDispenseConfig, spawnDispensedBottleOfEnchanting),
	reflect.TypeFor[item.SplashPotion]():       dispenseShot(thrownDispenseConfig, spawnDispensedSplashPotion),
	reflect.TypeFor[item.LingeringPotion]():    dispenseShot(thrownDispenseConfig, spawnDispensedLingeringPotion),
	reflect.TypeFor[item.Firework]():           dispenseProjectile(spawnDispensedFirework),

	// BoatDispenseItemBehavior.
	reflect.TypeFor[item.Boat]():      dispenseBoat(spawnDispensedBoat),
	reflect.TypeFor[item.ChestBoat](): dispenseBoat(spawnDispensedChestBoat),

	// MinecartDispenseItemBehavior.
	reflect.TypeFor[item.Minecart]():             dispenseMinecart(spawnDispensedMinecart),
	reflect.TypeFor[item.MinecartChest]():        dispenseMinecart(spawnDispensedMinecartChest),
	reflect.TypeFor[item.MinecartHopper]():       dispenseMinecart(spawnDispensedMinecartHopper),
	reflect.TypeFor[item.MinecartTNT]():          dispenseMinecart(spawnDispensedMinecartTNT),
	reflect.TypeFor[item.MinecartCommandBlock](): dispenseMinecart(spawnDispensedMinecartCommandBlock),

	// The anonymous behaviours of bootStrap for Items.ARMOR_STAND and
	// Blocks.TNT.
	reflect.TypeFor[item.ArmourStand](): dispenseArmourStand,
	reflect.TypeFor[TNT]():              dispenseTNT,

	// SpawnEggItemBehavior, which getDefaultDispenseMethod picks for spawn
	// eggs.
	reflect.TypeFor[item.SpawnEgg](): dispenseSpawnEgg,

	// EquipmentDispenseItemBehavior, which getDefaultDispenseMethod picks
	// for equippable items: here the harness alone.
	reflect.TypeFor[item.Harness](): dispenseHarness,

	// The bucket behaviours, FlintAndSteelDispenseItemBehavior and the
	// anonymous behaviours of Items.BONE_MEAL and Items.GLASS_BOTTLE, which
	// use the item on the block in front.
	reflect.TypeFor[item.Bucket]():        dispenseUseOnBlock,
	reflect.TypeFor[item.MobBucket]():     dispenseUseOnBlock,
	reflect.TypeFor[item.FishBucket]():    dispenseUseOnBlock,
	reflect.TypeFor[item.BoneMeal]():      dispenseUseOnBlock,
	reflect.TypeFor[item.FlintAndSteel](): dispenseUseOnBlock,
	reflect.TypeFor[item.GlassBottle]():   dispenseUseOnBlock,
}

// dispenseBehaviourOf returns the behaviour of the item it, as Java's
// DispenserBlock.getDispenseMethod.
func dispenseBehaviourOf(it world.Item) dispenseBehaviour {
	if b, ok := dispenseBehaviours[reflect.TypeOf(it)]; ok {
		return b
	}
	return dispenseDefault
}

// front returns the position the dispenser faces.
func (src dispenseSource) front() cube.Pos { return src.pos.Side(src.d.Facing) }

// direction returns the unit vector the dispenser faces.
func (src dispenseSource) direction() mgl64.Vec3 {
	return src.front().Vec3Centre().Sub(src.pos.Vec3Centre())
}

// launch returns where and how fast a projectile leaves the dispenser.
func (src dispenseSource) launch() world.EntitySpawnOpts {
	direction := src.direction()
	return world.EntitySpawnOpts{Position: src.pos.Vec3Centre().Add(direction.Mul(0.7)), Velocity: direction.Mul(1.1)}
}

// consume returns the stack with the dispensed item gone, as
// ItemStack.shrink(1).
func (src dispenseSource) consume() (item.Stack, bool) { return src.stack.Grow(-1), true }

// dispenseDefault drops one item of the stack in front of the dispenser, as
// DefaultDispenseItemBehavior.
func dispenseDefault(src dispenseSource) (item.Stack, bool) {
	src.d.eject(src.tx, src.pos, src.stack.Grow(1-src.stack.Count()))
	return src.consume()
}

// dispenseProjectile shoots the item as the entity spawn creates, as
// ProjectileDispenseBehavior.
func dispenseProjectile(spawn dispenseSpawner) dispenseBehaviour {
	return func(src dispenseSource) (item.Stack, bool) {
		src.tx.AddEntity(spawn(src.tx.World().EntityRegistry().Config(), src.launch(), src.stack.Item()))
		return src.consume()
	}
}

// dispenseConfig is ProjectileItem.DispenseConfig: where a projectile leaves
// the dispenser and how it is shot.
type dispenseConfig struct {
	// offset is how far from the centre of the dispenser, towards its front,
	// the projectile spawns, and lift how far above that.
	offset, lift float64
	// uncertainty and power are the arguments of Projectile.shoot.
	uncertainty, power float64
}

var (
	// defaultDispenseConfig is ProjectileItem.DispenseConfig.DEFAULT: 0.7
	// blocks out and 0.1 up, uncertainty 6 and power 1.1.
	defaultDispenseConfig = dispenseConfig{offset: 0.7, lift: 0.1, uncertainty: 6, power: 1.1}
	// thrownDispenseConfig is the DispenseConfig of ExperienceBottleItem and
	// ThrowablePotionItem: the default with half its uncertainty and 1.25
	// times its power.
	thrownDispenseConfig = dispenseConfig{offset: 0.7, lift: 0.1, uncertainty: 3, power: 1.375}
)

// launch returns where a projectile spawns and the velocity Projectile.shoot
// gives it for the dispenser's direction: the direction made a unit vector,
// moved by a triangular random amount of 0.0172275 times the uncertainty on
// each axis, drawn from the world's random stream, and scaled by the power.
func (c dispenseConfig) launch(src dispenseSource) world.EntitySpawnOpts {
	direction := src.direction()
	spread := 0.0172275 * c.uncertainty
	triangle := func() float64 { return spread * float64(src.tx.RandomFloat32()-src.tx.RandomFloat32()) }
	vel := direction.Add(mgl64.Vec3{triangle(), triangle(), triangle()}).Mul(c.power)
	pos := src.pos.Vec3Centre().Add(direction.Mul(c.offset)).Add(mgl64.Vec3{0, c.lift})
	return world.EntitySpawnOpts{Position: pos, Velocity: vel}
}

// dispenseShot shoots the item as ProjectileDispenseBehavior does with the
// DispenseConfig given.
func dispenseShot(conf dispenseConfig, spawn dispenseSpawner) dispenseBehaviour {
	return func(src dispenseSource) (item.Stack, bool) {
		src.tx.AddEntity(spawn(src.tx.World().EntityRegistry().Config(), conf.launch(src), src.stack.Item()))
		return src.consume()
	}
}

func spawnDispensedEgg(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.Egg(opts, nil)
}

func spawnDispensedSnowball(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.Snowball(opts, nil)
}

func spawnDispensedFireCharge(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.SmallFireball(opts, nil)
}

func spawnDispensedWindCharge(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.WindCharge(opts, nil)
}

func spawnDispensedArrow(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	return conf.Arrow(opts, world.ArrowSpawnConfig{Damage: 2, Tip: it.(item.Arrow).Tip, ObtainArrowOnPickup: true})
}

func spawnDispensedBottleOfEnchanting(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.BottleOfEnchanting(opts, nil)
}

func spawnDispensedSplashPotion(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	return conf.SplashPotion(opts, it.(item.SplashPotion).Type, nil)
}

func spawnDispensedLingeringPotion(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	return conf.LingeringPotion(opts, it.(item.LingeringPotion).Type, nil)
}

func spawnDispensedFirework(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	return conf.Firework(opts, it.(item.Firework), nil, 1, 0, false)
}

// dispenseBoat places the boat spawn creates on the water in front, or on
// the air above it, as BoatDispenseItemBehavior, and drops it otherwise.
func dispenseBoat(spawn dispenseSpawner) dispenseBehaviour {
	return func(src dispenseSource) (item.Stack, bool) {
		tx, front := src.tx, src.front()
		frontLiquid, frontWater := tx.Liquid(front)
		belowLiquid, waterBelow := tx.Liquid(front.Side(cube.FaceDown))
		frontWater = frontWater && frontLiquid.LiquidType() == "water"
		waterBelow = waterBelow && belowLiquid.LiquidType() == "water"
		_, frontAir := tx.Block(front).(Air)
		if !frontWater && (!frontAir || !waterBelow) {
			return dispenseDefault(src)
		}
		direction := src.direction()
		opts := src.launch()
		opts.Position = front.Vec3Middle().Add(mgl64.Vec3{0, 0.375, 0})
		opts.Rotation = cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0}
		opts.Velocity = direction.Mul(0.1)
		tx.AddEntity(spawn(tx.World().EntityRegistry().Config(), opts, src.stack.Item()))
		return src.consume()
	}
}

func spawnDispensedBoat(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	return conf.Boat(opts, it.(item.Boat).Variant.Int())
}

func spawnDispensedChestBoat(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	return conf.ChestBoat(opts, it.(item.ChestBoat).Variant.Int())
}

// dispenseMinecart places the minecart spawn creates on the rail in front,
// or on the rail below the air in front, as MinecartDispenseItemBehavior,
// and drops it otherwise.
func dispenseMinecart(spawn dispenseSpawner) dispenseBehaviour {
	return func(src dispenseSource) (item.Stack, bool) {
		tx, pos, front := src.tx, src.pos, src.front()
		railPos := front
		if _, air := tx.Block(front).(Air); air {
			railPos = front.Side(cube.FaceDown)
		}
		if !IsRail(tx.Block(railPos)) {
			return dispenseDefault(src)
		}
		// Native MinecartItem::dispense requests the rail-cell centre,
		// offset 1/8 along the horizontal facing, for every rail shape.
		direction := src.direction()
		var centre mgl64.Vec3
		for axis := range 3 {
			centre[axis] = float64(float32(railPos[axis]) + .5)
			if axis != 1 {
				centre[axis] = float64(float32(centre[axis]) + float32(direction[axis])*.125)
			}
		}
		cart := spawn(tx.World().EntityRegistry().Config(), world.EntitySpawnOpts{Position: centre}, src.stack.Item())
		if origin, ok := cart.Type().(interface{ NetworkOffset() float64 }); ok {
			centre[1] = float64(float32(centre[1]) - float32(origin.NetworkOffset()))
			cart.SetPosition(centre)
		}
		spawned := tx.AddEntity(cart)
		// Native chunk actor passes include a newly dispensed cart in the
		// current or a later chunk, but defer a cart entering an earlier
		// chunk until the next game tick. Coordinates are signed here.
		if owner, ok := tx.EntityChunk(spawned); ok && owner[0] >= int32(pos[0]>>4) && owner[1] >= int32(pos[2]>>4) {
			tx.QueueEntityTick(spawned)
		}
		return src.consume()
	}
}

func spawnDispensedMinecart(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.Minecart(opts)
}

func spawnDispensedMinecartChest(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.MinecartChest(opts)
}

func spawnDispensedMinecartHopper(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.MinecartHopper(opts)
}

func spawnDispensedMinecartTNT(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.MinecartTNT(opts)
}

func spawnDispensedMinecartCommandBlock(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, _ world.Item) *world.EntityHandle {
	return conf.MinecartCommandBlock(opts)
}

// dispenseArmourStand stands an armour stand in front, facing away from the
// dispenser, as the behaviour bootStrap registers for Items.ARMOR_STAND, and
// drops it if the world has no armour stands.
func dispenseArmourStand(src dispenseSource) (item.Stack, bool) {
	create := src.tx.World().EntityRegistry().Config().ArmourStand
	if create == nil {
		return dispenseDefault(src)
	}
	direction := src.direction()
	opts := src.launch()
	opts.Position = src.front().Vec3Middle()
	opts.Rotation = cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0}
	src.tx.AddEntity(create(opts))
	return src.consume()
}

// dispenseTNT primes TNT in front, as the behaviour bootStrap registers for
// Blocks.TNT, and drops it if the world has no primed TNT.
func dispenseTNT(src dispenseSource) (item.Stack, bool) {
	create := src.tx.World().EntityRegistry().Config().TNT
	if create == nil {
		return dispenseDefault(src)
	}
	src.tx.AddEntity(create(world.EntitySpawnOpts{Position: src.front().Vec3Middle()}, 4*time.Second, nil))
	return src.consume()
}

// dispenseSpawnEgg spawns the mob of the egg in front, as
// SpawnEggItemBehavior, and leaves the egg if it can't.
func dispenseSpawnEgg(src dispenseSource) (item.Stack, bool) {
	if !src.stack.Item().(item.SpawnEgg).Dispense(src.tx, src.pos, src.d.Facing) {
		return src.stack, false
	}
	return src.consume()
}

// dispenseHarness puts the harness on the first mob in front that takes it,
// as EquipmentDispenseItemBehavior, and drops it if none does.
func dispenseHarness(src dispenseSource) (item.Stack, bool) {
	front := src.front()
	box := cube.Box(float64(front.X()), float64(front.Y()), float64(front.Z()), float64(front.X()+1), float64(front.Y()+1), float64(front.Z()+1))
	harness := src.stack.Item().(item.Harness)
	for e := range src.tx.EntitiesWithin(box) {
		ctx := &item.UseContext{}
		if harness.UseOnEntity(e, src.tx, nil, ctx) {
			return src.used(ctx), true
		}
	}
	return dispenseDefault(src)
}

// dispenseUseOnBlock uses the item on the block in front, as a player using
// it on the face of that block that the dispenser looks at, and drops it if
// that does nothing.
func dispenseUseOnBlock(src dispenseSource) (item.Stack, bool) {
	usable := src.stack.Item().(interface {
		UseOnBlock(cube.Pos, cube.Face, mgl64.Vec3, *world.Tx, item.User, *item.UseContext) bool
	})
	front := src.front()
	ctx := &item.UseContext{}
	if usable.UseOnBlock(front, src.d.Facing, front.Vec3Centre(), src.tx, nil, ctx) {
		return src.used(ctx), true
	}
	return dispenseDefault(src)
}

// used returns the stack left after the item was used with ctx: damaged,
// with the items used gone, and the item the use made in their place, or
// dropped if the stack is not empty.
func (src dispenseSource) used(ctx *item.UseContext) item.Stack {
	left := src.stack.Damage(ctx.Damage).Grow(-ctx.CountSub)
	if !ctx.NewItem.Empty() {
		if left.Empty() {
			left = ctx.NewItem
		} else {
			src.d.eject(src.tx, src.pos, ctx.NewItem)
		}
	}
	return left
}
