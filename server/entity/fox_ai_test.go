package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
)

func addFox(tx *world.Tx, pos mgl64.Vec3, conf entity.FoxConfig) *world.EntityHandle {
	return tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(entity.FoxType, conf)).H()
}

func foxOf(tx *world.Tx, h *world.EntityHandle) *entity.Fox {
	e, _ := h.Entity(tx)
	return e.(*entity.Fox)
}

// TestFoxesHuntChickens checks the land target goal, StalkPreyGoal and
// FoxPounceGoal of red foxes: a fox stalks a chicken and bites it.
func TestFoxesHuntChickens(t *testing.T) {
	w := wolfTestWorld(t)
	var chicken *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		pen(tx, 10)
		addFox(tx, mgl64.Vec3{-6.5, 64, .5}, entity.FoxConfig{})
		chicken = spawnAnimal(t, tx, "minecraft:chicken", 6.5, nil)
	})
	hurt := false
	for range 800 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { hurt = livingHealth(tx, chicken) < 4 })
		if hurt {
			break
		}
	}
	if !hurt {
		t.Fatal("fox did not attack a chicken in 40 seconds")
	}
}

// TestFoxesFleeFromPlayers checks the AvoidEntityGoal of foxes for players.
func TestFoxesFleeFromPlayers(t *testing.T) {
	w := wolfTestWorld(t)
	var fox *world.EntityHandle
	playerPos := mgl64.Vec3{4.5, 64, .5}
	<-w.Exec(func(tx *world.Tx) {
		fox = addFox(tx, mgl64.Vec3{.5, 64, .5}, entity.FoxConfig{})
		addSurvivalPlayer(tx, playerPos)
	})
	farthest := 0.0
	for range 100 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { farthest = max(farthest, horizontalDistance(entityPos(tx, fox), playerPos)) })
	}
	if farthest < 8 {
		t.Fatalf("fox got at most %.2f blocks from a player four blocks away, want it to flee", farthest)
	}
}

// TestFoxesPickSweetBerries checks FoxEatBerriesGoal: a fox walks to a ripe
// sweet berry bush, takes a berry into its mouth and leaves the bush
// picked.
func TestFoxesPickSweetBerries(t *testing.T) {
	w := traderTestWorld(t)
	var fox *world.EntityHandle
	bush := cube.Pos{5, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(bush, block.SweetBerries{Growth: 3}, nil)
		fox = addFox(tx, mgl64.Vec3{.5, 64, .5}, entity.FoxConfig{})
	})
	picked := false
	for range 2400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			b, _ := tx.Block(bush).(block.SweetBerries)
			picked = b.Growth == 1
		})
		if picked {
			break
		}
	}
	if !picked {
		t.Fatal("fox did not pick the ripe sweet berry bush in two minutes")
	}
	<-w.Exec(func(tx *world.Tx) {
		held, _ := foxOf(tx, fox).HeldItems()
		if _, ok := held.Item().(block.SweetBerries); !ok {
			t.Fatalf("fox holds %v after picking berries, want sweet berries", held)
		}
	})
}

// shelterGenerator generates a stone floor at y 63 and, in the chunk at the
// origin, a fenced cell at x and z 6 under a roof. The world lights a chunk
// as it generates it, so the roof shades the cell while sky light shines in
// through the fence.
type shelterGenerator struct{}

func (shelterGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk, _ world.GenerationContext) world.GenerationResult {
	stone, fence := world.BlockRuntimeID(block.Stone{}), world.BlockRuntimeID(block.WoodFence{})
	for x := uint8(0); x < 16; x++ {
		for z := uint8(0); z < 16; z++ {
			c.SetBlock(x, 63, z, 0, stone)
			if pos == (world.ChunkPos{}) && x >= 5 && x <= 7 && z >= 5 && z <= 7 && (x != 6 || z != 6) {
				c.SetBlock(x, 64, z, 0, fence)
			}
		}
	}
	if pos == (world.ChunkPos{}) {
		c.SetBlock(6, 66, 6, 0, stone)
	}
	return world.GenerationResult{}
}

func (shelterGenerator) DefaultSpawn(world.Dimension) cube.Pos { return cube.Pos{6, 64, 6} }

// TestFoxesSleepUnderCoverByDay checks SleepGoal: by day a fox under cover
// in bright light with nothing around lies down to sleep.
func TestFoxesSleepUnderCoverByDay(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Generator: shelterGenerator{}}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(6000)
	w.StopTime()
	var fox *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		if sky := tx.SkyLight(cube.Pos{6, 64, 6}); sky >= 15 || sky < 12 {
			t.Fatalf("sky light under the roof is %d, want shade bright enough for a fox", sky)
		}
		fox = addFox(tx, mgl64.Vec3{6.5, 64, 6.5}, entity.FoxConfig{})
	})
	slept := false
	for range 1200 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { slept = slept || foxOf(tx, fox).Sleeping() })
		if slept {
			break
		}
	}
	if !slept {
		t.Fatal("fox under cover did not fall asleep by day in a minute")
	}
}

// TestFoxesDefendTrustedPlayers checks DefendTrustedTargetGoal: a fox bites
// the zombie that hurts a player it trusts.
func TestFoxesDefendTrustedPlayers(t *testing.T) {
	w := aiTestWorld(t)
	p := addAITestPlayer(w, mgl64.Vec3{3.5, 65, .5}, world.GameModeSurvival)
	var zombie *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		pl, _ := p.Entity(tx)
		addFox(tx, mgl64.Vec3{.5, 65, 3.5}, entity.FoxConfig{Trusted: []string{pl.(*player.Player).UUID().String()}})
		zombie = tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 65, .5}})).H()
	})
	bitten := false
	for range 600 {
		stepWorld(t, w, 1)
		<-w.Exec(func(tx *world.Tx) { bitten = livingHealth(tx, zombie) < 20 })
		if bitten {
			break
		}
	}
	if !bitten {
		t.Fatal("fox did not attack the zombie hurting a player it trusts")
	}
}

// TestFoxesPickUpItems checks FoxSearchForItemsGoal and Fox.pickUpItem: a
// fox fetches an item nearby and holds one of it in its mouth.
func TestFoxesPickUpItems(t *testing.T) {
	w := wolfTestWorld(t)
	var fox *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		// The pen keeps the fox from strolling more than eight blocks from
		// the item.
		pen(tx, 5)
		fox = addFox(tx, mgl64.Vec3{.5, 64, .5}, entity.FoxConfig{})
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, .5}}, item.NewStack(item.Emerald{}, 3)))
	})
	// A fox perching to look around does not search for items.
	held := item.Stack{}
	for range 2400 {
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) { held, _ = foxOf(tx, fox).HeldItems() })
		if !held.Empty() {
			break
		}
	}
	if _, ok := held.Item().(item.Emerald); !ok || held.Count() != 1 {
		t.Fatalf("fox holds %v, want one emerald fetched from the ground", held)
	}
}
