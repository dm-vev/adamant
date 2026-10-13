package entity_test

import (
	"math/rand/v2"
	"sync"
	"testing"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// breedingTestWorld returns a world with a grass floor around the origin.
func breedingTestWorld(t *testing.T) *world.World {
	t.Helper()
	w := newDeathTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		for x := -8; x <= 8; x++ {
			for z := -8; z <= 8; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Grass{}, nil)
			}
		}
	})
	return w
}

// spawnAnimal spawns the Java mob passed on the floor with the NBT given.
func spawnAnimal(t *testing.T, tx *world.Tx, id string, x float64, nbt map[string]any) *world.EntityHandle {
	t.Helper()
	data := map[string]any{"id": id}
	for k, v := range nbt {
		data[k] = v
	}
	h, err := entity.NewFromJavaNBT(data, world.EntitySpawnOpts{Position: mgl64.Vec3{x, 64, 0.5}, SpawnFinalized: true})
	if err != nil {
		t.Fatal(err)
	}
	return tx.AddEntity(h).H()
}

// feed makes a player use the item passed on the entity and reports whether
// the entity took it.
func feed(t *testing.T, tx *world.Tx, p *player.Player, h *world.EntityHandle, it world.Item) bool {
	t.Helper()
	e, _ := h.Entity(tx)
	p.SetHeldItems(item.NewStack(it, 64), item.Stack{})
	ctx := &item.UseContext{}
	return e.(entity.Interactable).Interact(tx, p, ctx) && ctx.CountSub == 1
}

func inLove(tx *world.Tx, h *world.EntityHandle) bool {
	e, _ := h.Entity(tx)
	return e.(interface{ InLove() bool }).InLove()
}

func baby(tx *world.Tx, h *world.EntityHandle) bool {
	e, _ := h.Entity(tx)
	return e.(interface{ Baby() bool }).Baby()
}

func TestFedAdultsBreedABabyAndRest(t *testing.T) {
	w := breedingTestWorld(t)
	var a, b *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addDeathTestPlayer(tx)
		a, b = spawnAnimal(t, tx, "minecraft:cow", 0.5, nil), spawnAnimal(t, tx, "minecraft:cow", 2.5, nil)
		if feed(t, tx, p, a, block.Carrot{}) {
			t.Fatal("cow took a carrot")
		}
		if !feed(t, tx, p, a, item.Wheat{}) || !feed(t, tx, p, b, item.Wheat{}) || !inLove(tx, a) || !inLove(tx, b) {
			t.Fatal("fed cows did not fall in love")
		}
		if feed(t, tx, p, a, item.Wheat{}) {
			t.Fatal("cow in love ate again")
		}
	})
	for range 200 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		cows, babies := 0, 0
		for e := range tx.Entities() {
			if e.H().Type() == entity.CowType {
				cows++
				if baby(tx, e.H()) {
					babies++
				}
			}
		}
		if cows != 3 || babies != 1 {
			t.Fatalf("%d cows of which %d babies, want 3 and 1", cows, babies)
		}
		if countEntities(tx, "minecraft:xp_orb") == 0 {
			t.Fatal("breeding dropped no experience")
		}
		p := addDeathTestPlayer(tx)
		if inLove(tx, a) || inLove(tx, b) || feed(t, tx, p, a, item.Wheat{}) {
			t.Fatal("parents could fall in love again right after breeding")
		}
	})
}

func TestHurtAnimalsFallOutOfLove(t *testing.T) {
	w := breedingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := addDeathTestPlayer(tx)
		h := spawnAnimal(t, tx, "minecraft:cow", 0.5, nil)
		feed(t, tx, p, h, item.Wheat{})
		e, _ := h.Entity(tx)
		e.(entity.Living).Hurt(1, entity.VoidDamageSource{})
		if inLove(tx, h) {
			t.Fatal("hurt cow stayed in love")
		}
	})
}

func TestFeedingBabiesSpeedsUpGrowth(t *testing.T) {
	w := breedingTestWorld(t)
	var fed, unfed *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addDeathTestPlayer(tx)
		// 2000 ticks to go: feeding takes off a tenth, 200 ticks.
		fed = spawnAnimal(t, tx, "minecraft:cow", 0.5, map[string]any{"Age": int32(-2000)})
		unfed = spawnAnimal(t, tx, "minecraft:cow", 4.5, map[string]any{"Age": int32(-2000)})
		if !feed(t, tx, p, fed, item.Wheat{}) {
			t.Fatal("calf did not eat")
		}
	})
	for range 1850 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if baby(tx, fed) || !baby(tx, unfed) {
			t.Fatalf("after 1850 ticks: fed calf baby %v, unfed calf baby %v", baby(tx, fed), baby(tx, unfed))
		}
	})
}

// breedPair feeds two adults of the type passed and waits for them to
// breed.
func breedPair(t *testing.T, id string, food world.Item, nbt map[string]any) *world.World {
	t.Helper()
	w := breedingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := addDeathTestPlayer(tx)
		a, b := spawnAnimal(t, tx, id, 0.5, nbt), spawnAnimal(t, tx, id, 2.5, nbt)
		if !feed(t, tx, p, a, food) || !feed(t, tx, p, b, food) {
			t.Fatalf("%s did not eat %T", id, food)
		}
	})
	for range 200 {
		w.AdvanceTick()
	}
	return w
}

func TestFarmAnimalsBreedOnTheirFood(t *testing.T) {
	cases := []struct {
		id   string
		food world.Item
	}{
		{"minecraft:pig", block.Carrot{}},
		{"minecraft:sheep", item.Wheat{}},
		{"minecraft:chicken", block.WheatSeeds{}},
		{"minecraft:goat", item.Wheat{}},
		{"minecraft:rabbit", block.Carrot{}},
		{"minecraft:mooshroom", item.Wheat{}},
		{"minecraft:armadillo", item.SpiderEye{}},
		// The feeding player stands far enough that the ocelot does not
		// take the fish as an offer of trust.
		{"minecraft:ocelot", item.Cod{}},
		{"minecraft:bee", block.Flower{Type: block.Dandelion()}},
	}
	for _, c := range cases {
		w := breedPair(t, c.id, c.food, nil)
		<-w.Exec(func(tx *world.Tx) {
			n, babies := 0, 0
			for e := range tx.Entities() {
				if e.H().Type().EncodeEntity() == c.id {
					n++
					if baby(tx, e.H()) {
						babies++
					}
				}
			}
			if n != 3 || babies != 1 {
				t.Errorf("%s: %d animals of which %d babies, want 3 and 1", c.id, n, babies)
			}
		})
	}
}

func TestSniffersLayAnEggInsteadOfABaby(t *testing.T) {
	w := breedPair(t, "minecraft:sniffer", block.TorchflowerCrop{}, nil)
	<-w.Exec(func(tx *world.Tx) {
		eggs := 0
		_, items := deathDrops(tx)
		for _, it := range items {
			if _, egg := it.Item().(block.SnifferEgg); egg {
				eggs++
			}
		}
		if n := countEntities(tx, "minecraft:sniffer"); n != 2 || eggs != 1 {
			t.Fatalf("%d sniffers and %d eggs, want 2 and 1", n, eggs)
		}
	})
}

// registerVanillaRecipes is the vanilla recipe registration the server runs
// on start.
//
//go:linkname registerVanillaRecipes github.com/df-mc/dragonfly/server/item/recipe.registerVanilla
func registerVanillaRecipes()

var registerRecipesOnce sync.Once

func TestLambFleeceMixesParentDyes(t *testing.T) {
	registerRecipesOnce.Do(func() {
		world.DefaultBlockRegistry.Finalize()
		registerVanillaRecipes()
	})
	w := breedingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := addDeathTestPlayer(tx)
		// Java wool colour IDs: red is 14, yellow 4.
		a := spawnAnimal(t, tx, "minecraft:sheep", 0.5, map[string]any{"Color": byte(14)})
		b := spawnAnimal(t, tx, "minecraft:sheep", 2.5, map[string]any{"Color": byte(4)})
		feed(t, tx, p, a, item.Wheat{})
		feed(t, tx, p, b, item.Wheat{})
	})
	for range 200 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if s, ok := e.(*entity.Sheep); ok && s.Baby() {
				if got := s.LootComponents()["minecraft:sheep/color"]; got != "orange" {
					t.Fatalf("lamb of red and yellow sheep is %v, want orange", got)
				}
				return
			}
		}
		t.Fatal("no lamb was born")
	})
}

func TestOnlyTamedPetsBreedTamedYoung(t *testing.T) {
	owner := []int32{0x11223344, 0x55667788, -0x66778899, 0x0abbccdd}
	cases := []struct {
		id    string
		food  world.Item
		tamed bool
	}{
		{"minecraft:cat", item.Cod{}, true},
		{"minecraft:wolf", item.Beef{}, true},
		{"minecraft:wolf", item.Beef{}, false},
	}
	for _, c := range cases {
		nbt := map[string]any{}
		if c.tamed {
			nbt["Tame"], nbt["Owner"] = byte(1), owner
		}
		w := breedPair(t, c.id, c.food, nbt)
		<-w.Exec(func(tx *world.Tx) {
			var babies []world.Entity
			for e := range tx.Entities() {
				if e.H().Type().EncodeEntity() == c.id && baby(tx, e.H()) {
					babies = append(babies, e)
				}
			}
			if !c.tamed {
				if len(babies) != 0 {
					t.Errorf("wild %s bred", c.id)
				}
				return
			}
			if len(babies) != 1 {
				t.Fatalf("tamed %s: %d babies, want 1", c.id, len(babies))
			}
			b := babies[0].(interface{ Tamed() bool })
			if !b.Tamed() {
				t.Errorf("baby %s of tamed parents is wild", c.id)
			}
		})
	}
}

// frogPond builds a stone floor at y 63 with a strip of water at x 3 to 5.
func frogPond(tx *world.Tx) {
	for x := -4; x <= 8; x++ {
		for z := -4; z <= 4; z++ {
			tx.SetBlock(cube.Pos{x, 62, z}, block.Stone{}, nil)
			if x >= 3 && x <= 5 {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Water{Depth: 8, Still: true}, nil)
			} else {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
	}
}

func TestFrogspawnHatchesIntoTadpoles(t *testing.T) {
	w := breedingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		frogPond(tx)
		pos := cube.Pos{4, 64, 0}
		tx.SetBlock(pos, block.FrogSpawn{}, nil)
		block.FrogSpawn{}.ScheduledTick(pos, tx, rand.New(rand.NewPCG(1, 2)))
		if _, ok := tx.Block(pos).(block.FrogSpawn); ok {
			t.Fatal("hatched frogspawn stayed")
		}
		if n := countEntities(tx, "minecraft:tadpole"); n < 2 || n > 5 {
			t.Fatalf("frogspawn hatched %d tadpoles, want 2 to 5", n)
		}
	})
}

func TestFrogspawnNeedsWaterBelow(t *testing.T) {
	w := breedingTestWorld(t)
	pos := cube.Pos{4, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		frogPond(tx)
		tx.SetBlock(pos, block.FrogSpawn{}, nil)
		tx.SetBlock(pos.Side(cube.FaceDown), block.Stone{}, nil)
	})
	w.AdvanceTick()
	<-w.Exec(func(tx *world.Tx) {
		if _, ok := tx.Block(pos).(block.FrogSpawn); ok {
			t.Fatal("frogspawn stayed on stone")
		}
	})
}

func TestBredFrogLaysFrogspawnOnNearbyWater(t *testing.T) {
	w := breedingTestWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		frogPond(tx)
		p := addDeathTestPlayer(tx)
		a, b := spawnAnimal(t, tx, "minecraft:frog", 0.5, nil), spawnAnimal(t, tx, "minecraft:frog", 1.5, nil)
		if !feed(t, tx, p, a, item.Slimeball{}) || !feed(t, tx, p, b, item.Slimeball{}) {
			t.Fatal("frogs did not eat slime balls")
		}
	})
	for range 600 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		spawn := 0
		for x := 3; x <= 5; x++ {
			for z := -4; z <= 4; z++ {
				if _, ok := tx.Block(cube.Pos{x, 64, z}).(block.FrogSpawn); ok {
					spawn++
				}
			}
		}
		if spawn != 1 || countEntities(tx, "minecraft:frog") != 2 {
			t.Fatalf("%d frogspawn on the water and %d frogs, want 1 and 2", spawn, countEntities(tx, "minecraft:frog"))
		}
	})
}

func TestGoldenDandelionPausesBabyGrowth(t *testing.T) {
	w := breedingTestWorld(t)
	var locked, free *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		p := addDeathTestPlayer(tx)
		// Both calves are 10 ticks from growing up.
		locked = spawnAnimal(t, tx, "minecraft:cow", 0.5, map[string]any{"Age": int32(-10)})
		free = spawnAnimal(t, tx, "minecraft:cow", 4.5, map[string]any{"Age": int32(-10)})
		if !feed(t, tx, p, locked, block.GoldenDandelion{}) {
			t.Fatal("calf did not take the golden dandelion")
		}
		if feed(t, tx, p, locked, item.Wheat{}) {
			t.Fatal("age locked calf ate wheat to grow")
		}
	})
	for range 100 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		if !baby(tx, locked) || baby(tx, free) {
			t.Fatalf("after 100 ticks: locked calf baby %v, free calf baby %v", baby(tx, locked), baby(tx, free))
		}
		p := addDeathTestPlayer(tx)
		if !feed(t, tx, p, locked, block.GoldenDandelion{}) {
			t.Fatal("calf did not take a second golden dandelion")
		}
		if !feed(t, tx, p, locked, item.Wheat{}) {
			t.Fatal("unlocked calf did not eat wheat to grow")
		}
	})
}
