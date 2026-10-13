package item

import (
	"log/slog"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// SpawnEgg is an item that spawns an entity when used.
type SpawnEgg struct {
	// Entity is the string identifier of the entity to spawn.
	Entity string
}

// UseOnBlock spawns an entity at the clicked position.
func (s SpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user User, ctx *UseContext) bool {
	if b := tx.Block(pos); isMobSpawner(b) {
		// SpawnEggItem.useOn: with spawner_blocks_work off, spawners can't be
		// changed and the user is told so.
		if !tx.World().GameRuleBool("spawner_blocks_work") {
			if m, ok := user.(interface{ Message(...any) }); ok {
				m.Message("Spawner blocks are not enabled on this server")
			}
			return false
		}
		return s.trySetSpawner(pos, b, tx, ctx)
	}
	return s.spawnAt(pos.Side(face).Vec3Middle(), tx, ctx)
}

func isMobSpawner(b world.Block) bool {
	name, _ := b.EncodeBlock()
	return name == "minecraft:mob_spawner"
}

// UseOnEntity spawns an entity at the target entity's position.
func (s SpawnEgg) UseOnEntity(e world.Entity, tx *world.Tx, _ User, ctx *UseContext) bool {
	return s.spawnAt(e.Position(), tx, ctx)
}

// EncodeItem returns the ID and meta of the spawn egg item.
func (s SpawnEgg) EncodeItem() (name string, meta int16) {
	if itemName, ok := spawnEggItems[s.Entity]; ok {
		return itemName, 0
	}
	return "minecraft:spawn_egg", 0
}

func (s SpawnEgg) spawnAt(pos mgl64.Vec3, tx *world.Tx, ctx *UseContext) bool {
	t, ok := tx.World().EntityRegistry().Lookup(s.Entity)
	if !ok {
		slog.Default().Info("spawn_egg: unknown entity", "entity", s.Entity)
		return false
	}
	opts := world.EntitySpawnOpts{Position: pos}
	tx.AddEntity(opts.New(t, emptyEntityConfig{}))
	ctx.SubtractFromCount(1)
	return true
}

type spawnerBlock interface {
	WithEntity(entity string) world.Block
}

func (s SpawnEgg) trySetSpawner(pos cube.Pos, b world.Block, tx *world.Tx, ctx *UseContext) bool {
	spawner, ok := b.(spawnerBlock)
	if !ok {
		return false
	}
	tx.SetBlock(pos, spawner.WithEntity(s.Entity), nil)
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
