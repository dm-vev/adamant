package item

import (
	"log/slog"
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnEgg is an item that spawns an entity when used.
type SpawnEgg struct {
	// Entity is the string identifier of the entity to spawn.
	Entity string
}

// UseOnBlock ports SpawnEggItem.useOn: a spawner takes the egg's entity,
// otherwise the mob spawns in the clicked block if it has no collision, or
// beside the clicked face, settled onto what is below it.
func (s SpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if !s.canSpawn(tx) {
		return false
	}
	if b := tx.Block(pos); isMobSpawner(b) {
		// SpawnEggItem.useOn: with spawner_blocks_work off, spawners can't be
		// changed and the user is told so.
		if !tx.World().GameRuleBool("spawner_blocks_work") {
			if m, ok := user.(interface{ Message(...any) }); ok {
				m.Message("Spawner blocks are not enabled on this server")
			}
			return false
		}
		return s.trySetSpawner(pos, b, tx, user, ctx)
	}
	spawnPos := pos
	if len(tx.Block(pos).Model().BBox(pos, tx)) != 0 {
		spawnPos = pos.Side(face)
	}
	if s.spawn(tx, user, spawnPos, "spawn_item_use", true, spawnPos != pos && face == cube.FaceUp) == nil {
		return false
	}
	spawnEggPlaced(tx, user, spawnPos)
	ctx.SubtractFromCount(1)
	return true
}

// Use ports SpawnEggItem.use: aimed at a liquid source block, the mob spawns
// inside it.
func (s SpawnEgg) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	_, pos, _, ok := povHit(tx, user, povSourceFluid)
	if !ok || !s.canSpawn(tx) {
		return false
	}
	if _, liquid := tx.Block(pos).(world.Liquid); !liquid {
		return false
	}
	if s.spawn(tx, user, pos, "spawn_item_use", false, false) == nil {
		return false
	}
	spawnEggPlaced(tx, user, pos)
	ctx.SubtractFromCount(1)
	return true
}

// Dispense ports SpawnEggItemBehavior: a dispenser spawns the mob in the
// block it faces, settled down unless it faces up. pos is the dispenser.
func (s SpawnEgg) Dispense(tx *world.Tx, pos cube.Pos, facing cube.Face) bool {
	if !s.canSpawn(tx) || s.spawn(tx, nil, pos.Side(facing), "dispenser", facing != cube.FaceUp, false) == nil {
		return false
	}
	tx.EmitGameEvent(world.GameEventEntityPlace, pos.Vec3Centre(), world.GameEventContext{})
	return true
}

// spawnEggPlaced emits the entity_place game event of SpawnEggItem.spawnMob.
func spawnEggPlaced(tx *world.Tx, user User, pos cube.Pos) {
	var source world.Entity
	if user != nil {
		source = user
	}
	tx.EmitGameEvent(world.GameEventEntityPlace, pos.Vec3Centre(), world.GameEventContext{Source: source})
}

// canSpawn ports EntityType.canSpawn: monsters can't be spawned in peaceful.
func (s SpawnEgg) canSpawn(tx *world.Tx) bool {
	return !spawnEggNotInPeaceful[s.Entity] || tx.World().Difficulty() != world.DifficultyPeaceful
}

func isMobSpawner(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:mob_spawner" || name == "minecraft:trial_spawner"
}

// UseOnEntity ports Mob.checkAndHandleImportantInteractions: an egg of the
// clicked mob's own type spawns a baby of it at the mob's position.
func (s SpawnEgg) UseOnEntity(e world.Entity, tx *world.Tx, user User, ctx *UseContext) bool {
	t, ok := tx.World().EntityRegistry().Lookup(s.Entity)
	if !ok || t.EncodeEntity() != e.H().Type().EncodeEntity() || !s.canSpawn(tx) {
		return false
	}
	var spawner world.Entity
	if user != nil {
		spawner = user
	}
	offspring := tx.World().EntityRegistry().Config().SpawnEggOffspring
	if offspring == nil || offspring(tx, e, spawner) == nil {
		return false
	}
	ctx.SubtractFromCount(1)
	return true
}

// EncodeItem returns the ID and meta of the spawn egg item.
func (s SpawnEgg) EncodeItem() (name string, meta int16) {
	if itemName, ok := spawnEggItems[s.Entity]; ok {
		return itemName, 0
	}
	return "minecraft:spawn_egg", 0
}

// spawn ports EntityType.spawn with tryMoveDown and movedUp: the mob is
// centred on spawnPos with a random yaw and, when moving down, dropped onto
// the collisions of the block (and the one below if it moved up).
func (s SpawnEgg) spawn(tx *world.Tx, user User, spawnPos cube.Pos, reason string, tryMoveDown, movedUp bool) world.Entity {
	t, ok := tx.World().EntityRegistry().Lookup(s.Entity)
	if !ok {
		slog.Default().Info("spawn_egg: unknown entity", "entity", s.Entity)
		return nil
	}
	pos := mgl64.Vec3{float64(spawnPos[0]) + 0.5, float64(spawnPos[1]), float64(spawnPos[2]) + 0.5}
	if tryMoveDown {
		pos[1]++
	}
	opts := world.EntitySpawnOpts{Position: pos, Rotation: cube.Rotation{float64(wrapDegrees(rand.Float32() * 360)), 0}, SpawnReason: reason}
	if user != nil {
		opts = stackSpawnOpts(user, opts)
	}
	var h *world.EntityHandle
	if create := tx.World().EntityRegistry().Config().SpawnMob; create != nil {
		h = create(s.Entity, opts)
	}
	if h == nil {
		h = opts.New(t, emptyEntityConfig{})
	}
	e := tx.AddEntity(h)
	if tryMoveDown {
		box := e.H().Type().BBox(e).Translate(pos)
		area := cube.Box(float64(spawnPos[0]), float64(spawnPos[1]), float64(spawnPos[2]), float64(spawnPos[0]+1), float64(spawnPos[1]+1), float64(spawnPos[2]+1))
		if movedUp {
			area = area.ExtendTowards(cube.FaceDown, 1)
		}
		pos[1] = float64(spawnPos[1]) + 1 + spawnDropOffset(tx, area, box, movedUp)
		if t, ok := e.(interface{ Teleport(mgl64.Vec3) }); ok {
			t.Teleport(pos)
		}
	}
	if _, mob := e.(interface{ MaxHealth() float64 }); mob {
		// Mob.playAmbientSound right after the spawn.
		tx.PlaySound(e.Position(), sound.NativeEvent{SoundType: "ambient", EntityType: world.BedrockEntityIdentifier(s.Entity), ExtraData: -1})
	}
	return e
}

// spawnDropOffset ports Shapes.collide along Y for EntityType.getYOffset:
// the box falls by one block, or two when moved up, onto the collision
// boxes of the blocks within area.
func spawnDropOffset(tx *world.Tx, area, box cube.BBox, movedUp bool) float64 {
	delta := -1.0
	if movedUp {
		delta = -2
	}
	min, max := area.Min(), area.Max()
	for y := int(math.Floor(min[1])); y < int(math.Ceil(max[1])); y++ {
		p := cube.Pos{int(min[0]), y, int(min[2])}
		for _, bb := range tx.Block(p).Model().BBox(p, tx) {
			if bb = bb.Translate(p.Vec3()); bb.IntersectsWith(area) {
				delta = box.YOffset(bb, delta)
			}
		}
	}
	return delta
}

type spawnerBlock interface {
	WithEntity(entity string) world.Block
}

func (s SpawnEgg) trySetSpawner(pos cube.Pos, b world.Block, tx *world.Tx, user User, ctx *UseContext) bool {
	spawner, ok := b.(spawnerBlock)
	if !ok {
		return false
	}
	tx.SetBlock(pos, spawner.WithEntity(s.Entity), nil)
	var source world.Entity
	if user != nil {
		source = user
	}
	tx.EmitGameEvent(world.GameEventBlockChange, pos.Vec3Centre(), world.GameEventContext{Source: source})
	ctx.SubtractFromCount(1)
	return true
}

type emptyEntityConfig struct{}

func (emptyEntityConfig) Apply(_ *world.EntityData) {}

var spawnEggEntities = [...]string{
	"minecraft:chicken", "minecraft:cow", "minecraft:pig", "minecraft:sheep", "minecraft:camel", "minecraft:donkey",
	"minecraft:horse", "minecraft:mule", "minecraft:cat", "minecraft:parrot", "minecraft:wolf", "minecraft:armadillo",
	"minecraft:bat", "minecraft:bee", "minecraft:fox", "minecraft:goat", "minecraft:llama", "minecraft:ocelot",
	"minecraft:panda", "minecraft:polar_bear", "minecraft:rabbit", "minecraft:axolotl", "minecraft:cod", "minecraft:dolphin",
	"minecraft:frog", "minecraft:glow_squid", "minecraft:nautilus", "minecraft:pufferfish", "minecraft:salmon", "minecraft:squid",
	"minecraft:tadpole", "minecraft:tropical_fish", "minecraft:turtle", "minecraft:allay", "minecraft:mooshroom", "minecraft:sniffer",
	"minecraft:sulfur_cube", "minecraft:copper_golem", "minecraft:iron_golem", "minecraft:snow_golem", "minecraft:trader_llama",
	"minecraft:villager", "minecraft:wandering_trader", "minecraft:bogged", "minecraft:camel_husk", "minecraft:drowned",
	"minecraft:husk", "minecraft:parched", "minecraft:skeleton", "minecraft:skeleton_horse", "minecraft:stray", "minecraft:wither",
	"minecraft:wither_skeleton", "minecraft:zombie", "minecraft:zombie_horse", "minecraft:zombie_nautilus", "minecraft:zombie_villager",
	"minecraft:cave_spider", "minecraft:spider", "minecraft:breeze", "minecraft:creaking", "minecraft:creeper",
	"minecraft:elder_guardian", "minecraft:guardian", "minecraft:phantom", "minecraft:silverfish", "minecraft:slime", "minecraft:warden",
	"minecraft:witch", "minecraft:evoker", "minecraft:pillager", "minecraft:ravager", "minecraft:vindicator", "minecraft:vex",
	"minecraft:blaze", "minecraft:ghast", "minecraft:happy_ghast", "minecraft:hoglin", "minecraft:magma_cube", "minecraft:piglin",
	"minecraft:piglin_brute", "minecraft:strider", "minecraft:zoglin", "minecraft:zombified_piglin", "minecraft:ender_dragon",
	"minecraft:enderman", "minecraft:endermite", "minecraft:shulker",
}

var spawnEggItems = func() map[string]string {
	items := make(map[string]string, len(spawnEggEntities))
	for _, entity := range spawnEggEntities {
		items[entity] = entity + "_spawn_egg"
		if alias, ok := spawnEggItemAliases[entity]; ok {
			items[entity] = alias
		}
	}
	return items
}()

// spawnEggItemAliases maps Java spawn egg item IDs to Bedrock's item registry names.
var spawnEggItemAliases = map[string]string{
	"minecraft:zombified_piglin": "minecraft:zombie_pigman_spawn_egg",
}

// spawnEggNotInPeaceful holds the entity types built with notInPeaceful in
// Java's EntityTypes.
var spawnEggNotInPeaceful = map[string]bool{
	"minecraft:blaze": true, "minecraft:bogged": true, "minecraft:breeze": true, "minecraft:cave_spider": true,
	"minecraft:creaking": true, "minecraft:creeper": true, "minecraft:drowned": true, "minecraft:elder_guardian": true,
	"minecraft:enderman": true, "minecraft:endermite": true, "minecraft:evoker": true, "minecraft:ghast": true,
	"minecraft:giant": true, "minecraft:guardian": true, "minecraft:hoglin": true, "minecraft:husk": true,
	"minecraft:illusioner": true, "minecraft:magma_cube": true, "minecraft:parched": true, "minecraft:phantom": true,
	"minecraft:piglin_brute": true, "minecraft:pillager": true, "minecraft:ravager": true, "minecraft:silverfish": true,
	"minecraft:skeleton": true, "minecraft:slime": true, "minecraft:spider": true, "minecraft:stray": true,
	"minecraft:vex": true, "minecraft:vindicator": true, "minecraft:warden": true, "minecraft:witch": true,
	"minecraft:wither": true, "minecraft:wither_skeleton": true, "minecraft:zoglin": true, "minecraft:zombie": true,
	"minecraft:zombie_villager": true, "minecraft:zombified_piglin": true,
}
