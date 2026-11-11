package block

import (
	"math"
	"reflect"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
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
// of src and returns what became of its slot and what the clients see.
type dispenseBehaviour func(src dispenseSource) dispensed

// dispenseSpawner creates the entity a behaviour spawns for the item it.
type dispenseSpawner func(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle

// dispenseBehaviours holds the behaviour of each item type that has one, as
// Java's DispenserBlock.DISPENSER_REGISTRY, filled by
// DispenseItemBehavior.bootStrap. Equippable items are worn by an entity in
// front, as getDefaultDispenseMethod picks EquipmentDispenseItemBehavior for
// them, then SulfurCubeBlockDispenseItemBehavior for the blocks a sulfur cube
// swallows, and items of other types are dropped (dispenseDefault).
var dispenseBehaviours = map[reflect.Type]dispenseBehaviour{
	// ProjectileDispenseBehavior, registered by registerProjectileBehavior.
	reflect.TypeFor[item.Egg]():                dispenseShot(defaultDispenseConfig, spawnDispensedEgg),
	reflect.TypeFor[item.Snowball]():           dispenseShot(defaultDispenseConfig, spawnDispensedSnowball),
	reflect.TypeFor[item.FireCharge]():         dispenseShot(fireChargeDispenseConfig, spawnDispensedFireCharge),
	reflect.TypeFor[item.WindCharge]():         dispenseShot(windChargeDispenseConfig, spawnDispensedWindCharge),
	reflect.TypeFor[item.Arrow]():              dispenseShot(arrowDispenseConfig, spawnDispensedArrow),
	reflect.TypeFor[item.BottleOfEnchanting](): dispenseShot(thrownDispenseConfig, spawnDispensedBottleOfEnchanting),
	reflect.TypeFor[item.SplashPotion]():       dispenseShot(thrownDispenseConfig, spawnDispensedSplashPotion),
	reflect.TypeFor[item.LingeringPotion]():    dispenseShot(thrownDispenseConfig, spawnDispensedLingeringPotion),
	reflect.TypeFor[item.Firework]():           dispenseShot(fireworkDispenseConfig, spawnDispensedFirework),

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

	// The behaviours bootStrap registers for the items below.
	reflect.TypeFor[Chest]():          dispenseChest,
	reflect.TypeFor[Pumpkin]():        dispensePumpkin,
	reflect.TypeFor[Skull]():          dispenseSkull,
	reflect.TypeFor[ShulkerBox]():     dispenseShulkerBox,
	reflect.TypeFor[Glowstone]():      dispenseGlowstone,
	reflect.TypeFor[item.Shears]():    dispenseShears,
	reflect.TypeFor[item.Brush]():     dispenseBrush,
	reflect.TypeFor[item.Honeycomb](): dispenseHoneycomb,
	reflect.TypeFor[item.Potion]():    dispensePotion,

	// The bucket behaviours, which use the item on the block in front.
	reflect.TypeFor[item.Bucket]():     dispenseBucket,
	reflect.TypeFor[item.MobBucket]():  dispenseBucket,
	reflect.TypeFor[item.FishBucket](): dispenseBucket,

	// FlintAndSteelDispenseItemBehavior and the anonymous behaviours of
	// Items.BONE_MEAL and Items.GLASS_BOTTLE, which are Optional ones.
	reflect.TypeFor[item.FlintAndSteel](): dispenseFlintAndSteel,
	reflect.TypeFor[item.BoneMeal]():      dispenseBoneMeal,
	reflect.TypeFor[item.GlassBottle]():   dispenseGlassBottle,
}

// dispenseBehaviourOf returns the behaviour of the item it, as Java's
// DispenserBlock.getDispenseMethod.
func dispenseBehaviourOf(it world.Item) dispenseBehaviour {
	if b, ok := dispenseBehaviours[reflect.TypeOf(it)]; ok {
		return b
	}
	if equippableByDispenser(it) {
		return dispenseEquip
	}
	if sulfurCubeSwallowable(it) {
		return dispenseSulfurCube
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

// dispenseDefault drops one item of the stack in front of the dispenser, as
// DefaultDispenseItemBehavior.
func dispenseDefault(src dispenseSource) dispensed {
	ejectContainerItem(src.pos, src.d.Facing, src.tx, src.stack.Grow(1-src.stack.Count()))
	return dispensed{left: src.stack.Grow(-1), effects: dispenseEffects{click: clickEjected}}
}

// dispenseConfig is ProjectileItem.DispenseConfig: where a projectile leaves
// the dispenser and how it is shot.
type dispenseConfig struct {
	// offset is how far from the centre of the dispenser, towards its front,
	// the projectile spawns, and lift how far above that.
	offset, lift float64
	// uncertainty and power are the arguments of Projectile.shoot.
	uncertainty, power float64
	// effects are what the clients see and hear besides the smoke: the
	// level event of the config (overrideDispenseEvent) and the click BDS
	// plays for the item.
	effects dispenseEffects
}

var (
	// defaultDispenseConfig is ProjectileItem.DispenseConfig.DEFAULT: 0.7
	// blocks out and 0.1 up, uncertainty 6 and power 1.1. BDS shoots eggs and
	// snowballs with the launch sound and no click.
	defaultDispenseConfig = dispenseConfig{offset: 0.7, lift: 0.1, uncertainty: 6, power: 1.1,
		effects: dispenseEffects{launch: sound.DispenserLaunch{}, click: clickSilent}}
	// arrowDispenseConfig is the default config of the arrow, which BDS also
	// clicks for.
	arrowDispenseConfig = dispenseConfig{offset: 0.7, lift: 0.1, uncertainty: 6, power: 1.1,
		effects: dispenseEffects{launch: sound.DispenserLaunch{}, click: clickDone}}
	// thrownDispenseConfig is the DispenseConfig of ExperienceBottleItem and
	// ThrowablePotionItem: the default with half its uncertainty and 1.25
	// times its power.
	thrownDispenseConfig = dispenseConfig{offset: 0.7, lift: 0.1, uncertainty: 3, power: 1.375,
		effects: dispenseEffects{launch: sound.DispenserLaunch{}, click: clickSilent}}
	// fireChargeDispenseConfig is the DispenseConfig of FireChargeItem (dispense
	// event 1018): 1.0 block out with no lift, uncertainty 6.6666665 and
	// power 1.0. FireChargeItem and WindChargeItem override
	// ProjectileItem.shoot with an empty body, but nothing calls it:
	// ProjectileDispenseBehavior calls the static
	// Projectile.spawnProjectileUsingShoot, which calls Projectile.shoot on the
	// projectile itself, so the config applies to them as to any projectile.
	fireChargeDispenseConfig = dispenseConfig{offset: 1, uncertainty: float64(float32(6.6666665)), power: 1,
		effects: dispenseEffects{launch: sound.DispenserFireball{}, click: clickSilent}}
	// windChargeDispenseConfig is the DispenseConfig of WindChargeItem (dispense
	// event 1051), the same as the fire charge but for the sound.
	windChargeDispenseConfig = dispenseConfig{offset: 1, uncertainty: float64(float32(6.6666665)), power: 1,
		effects: dispenseEffects{launch: sound.DispenserLaunch{}, click: clickDone}}
	// fireworkDispenseConfig is the DispenseConfig of FireworkRocketItem
	// (dispense event 1004): just outside the dispenser
	// (getEntityJustOutsideOfBlockPos, 0.5000099999997474 blocks from its
	// centre), uncertainty 1 and power 0.5. BDS sends no launch event for it:
	// the rocket makes its own launch sound.
	fireworkDispenseConfig = dispenseConfig{offset: 0.5000099999997474, uncertainty: 1, power: 0.5,
		effects: dispenseEffects{click: clickDone}}
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
	return func(src dispenseSource) dispensed {
		src.tx.AddEntity(spawn(src.tx.World().EntityRegistry().Config(), conf.launch(src), src.stack.Item()))
		return dispensed{left: src.stack.Grow(-1), effects: conf.effects}
	}
}

// spawnDispensedEgg throws the egg of the item, which keeps the climate of its
// chicken: Items.BLUE_EGG and Items.BROWN_EGG have a ProjectileDispenseBehavior
// of their own that makes a ThrownBlueEgg or ThrownBrownEgg.
func spawnDispensedEgg(conf *world.EntityRegistryConfig, opts world.EntitySpawnOpts, it world.Item) *world.EntityHandle {
	if variant := it.(item.Egg).Variant; variant != item.EggTemperate && conf.EggVariant != nil {
		return conf.EggVariant(opts, nil, int(variant))
	}
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

// boatWidth is the width of every boat of Java's EntityType (1.375), which
// BoatDispenseItemBehavior uses to put the boat just outside the dispenser.
const boatWidth = 1.375

// dispenseBoat places the boat spawn creates on the water in front, or on
// the air above it, as BoatDispenseItemBehavior, and drops it otherwise: the
// boat is put 0.5625 plus half its width out of the dispenser's centre,
// 1.125 blocks along it if it faces up or down, and a block higher over water
// than over air; it faces the way the dispenser does, and starts at rest.
func dispenseBoat(spawn dispenseSpawner) dispenseBehaviour {
	return func(src dispenseSource) dispensed {
		tx, front := src.tx, src.front()
		var yOffset float64
		if liquid, ok := tx.Liquid(front); ok && liquid.LiquidType() == "water" {
			yOffset = 1
		} else {
			below, belowWater := tx.Liquid(front.Side(cube.FaceDown))
			if _, air := tx.Block(front).(Air); !air || !belowWater || below.LiquidType() != "water" {
				return dispenseDefault(src)
			}
		}
		direction := src.direction()
		justOutside := 0.5625 + boatWidth/2
		pos := src.pos.Vec3Centre()
		opts := world.EntitySpawnOpts{
			Position: mgl64.Vec3{pos[0] + direction[0]*justOutside, pos[1] + direction[1]*1.125 + yOffset, pos[2] + direction[2]*justOutside},
			Rotation: cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0},
		}
		tx.AddEntity(spawn(tx.World().EntityRegistry().Config(), opts, src.stack.Item()))
		return src.done()
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
	return func(src dispenseSource) dispensed {
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
		return src.done()
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
func dispenseArmourStand(src dispenseSource) dispensed {
	create := src.tx.World().EntityRegistry().Config().ArmourStand
	if create == nil {
		return dispenseDefault(src)
	}
	direction := src.direction()
	opts := src.launch()
	opts.Position = src.front().Vec3Middle()
	opts.Rotation = cube.Rotation{math.Atan2(direction[2], direction[0])*180/math.Pi + 90, 0}
	src.tx.AddEntity(create(opts))
	return src.done()
}

// dispenseTNT primes TNT in front, as the behaviour bootStrap registers for
// Blocks.TNT, and drops it if the world has no primed TNT. With the gamerule
// tnt_explodes off, the behaviour keeps the TNT and the dispenser clicks.
func dispenseTNT(src dispenseSource) dispensed {
	if !src.tx.World().GameRuleBool("tnt_explodes") {
		return src.failed()
	}
	// SulfurCubeBlockDispenseItemBehavior.dispenseBlock comes first: a cube
	// in front swallows the TNT as a block of the explosive archetype.
	if dispenseToSulfurCube(src) {
		return src.done()
	}
	create := src.tx.World().EntityRegistry().Config().TNT
	if create == nil {
		return dispenseDefault(src)
	}
	src.tx.AddEntity(create(world.EntitySpawnOpts{Position: src.front().Vec3Middle()}, 4*time.Second, nil))
	// level.playSound(TNT_PRIMED); BDS plays the fuse at the launch point.
	src.tx.PlaySound(src.launchPoint(), sound.TNT{})
	return src.done()
}

// dispenseSpawnEgg spawns the mob of the egg in front, as
// SpawnEggItemBehavior, and leaves the egg if it can't.
func dispenseSpawnEgg(src dispenseSource) dispensed {
	if !src.stack.Item().(item.SpawnEgg).Dispense(src.tx, src.pos, src.d.Facing) {
		// The egg stays; the behaviour is not an optional one, so the
		// dispenser clicks as for any dispense.
		return dispensed{left: src.stack, unchanged: true, effects: dispenseEffects{click: clickDone}}
	}
	return src.done()
}

// dispenseHarness puts the harness on the first mob in front that takes it,
// as EquipmentDispenseItemBehavior, and drops it if none does.
func dispenseHarness(src dispenseSource) dispensed {
	harness := src.stack.Item().(item.Harness)
	for e := range entitiesInBlock(src.tx, src.front()) {
		ctx := &item.UseContext{}
		if harness.UseOnEntity(e, src.tx, nil, ctx) {
			return src.used(ctx)
		}
	}
	return dispenseDefault(src)
}

// dispenseBucket uses the bucket on the block in front, as the bucket
// behaviours of DispenseItemBehavior.bootStrap do: a filled one empties there,
// an empty one picks up what is there, and it is dropped if that does
// nothing.
func dispenseBucket(src dispenseSource) dispensed {
	usable := src.stack.Item().(interface {
		UseOnBlock(cube.Pos, cube.Face, mgl64.Vec3, *world.Tx, item.User, *item.UseContext) bool
	})
	front := src.front()
	ctx := &item.UseContext{}
	if usable.UseOnBlock(front, src.d.Facing, front.Vec3Centre(), src.tx, nil, ctx) {
		return src.used(ctx)
	}
	return dispenseDefault(src)
}

// used returns what became of the slot after the item was used with ctx:
// damaged, with the items used gone, and the item the use made in the place of
// the last item or, if the stack is not empty, to be put into the dispenser
// or dropped.
func (src dispenseSource) used(ctx *item.UseContext) dispensed {
	left := src.stack.Damage(ctx.Damage).Grow(-ctx.CountSub)
	res := dispensed{left: left, effects: dispenseEffects{click: clickDone}}
	if !ctx.NewItem.Empty() {
		if left.Empty() {
			res.left = ctx.NewItem
		} else {
			res.insert = ctx.NewItem
		}
	}
	return res
}
