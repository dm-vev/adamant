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
func (s SpawnEgg) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, _ User, ctx *UseContext) bool {
	if b := tx.Block(pos); s.trySetSpawner(pos, b, tx, ctx) {
		return true
	}
	return s.spawnAt(pos.Side(face).Vec3Middle(), tx, ctx)
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
	name, _ := b.EncodeBlock()
	if name != "minecraft:mob_spawner" {
		return false
	}
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

var spawnEggItems = map[string]string{
	"minecraft:zombie": "minecraft:zombie_spawn_egg",
}
