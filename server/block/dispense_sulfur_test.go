package block_test

import (
	"testing"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
)

// sulfurCubeInFront returns what adds a sulfur cube of the age given to the cell
// in front of a dispenser, holding body if it is not nil.
func sulfurCubeInFront(age int, body world.Item) func(tx *world.Tx, front cube.Pos) {
	return func(tx *world.Tx, front cube.Pos) {
		conf := entity.SulfurCubeBehaviourConfig{Size: 2, Age: age}
		if body != nil {
			conf.BodyItem = item.NewStack(body, 1)
		}
		tx.AddEntity(world.EntitySpawnOpts{Position: front.Vec3Middle()}.New(entity.SulfurCubeType, conf))
	}
}

// bodiesOfCubes returns the block each sulfur cube of the world holds.
func (dt *dispenserTest) bodiesOfCubes() (bodies []item.Stack) {
	<-dt.w.Exec(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if c, ok := e.(*entity.SulfurCube); ok {
				bodies = append(bodies, c.BodyItem())
			}
		}
	})
	return bodies
}

// TestDispenserFeedsBlocksToSulfurCubes checks DispenserBlock.getDefaultDispenseMethod
// and SulfurCubeBlockDispenseItemBehavior: an item of #sulfur_cube_swallowable
// that has no behaviour of its own goes into the sulfur cube in front, one at a
// time, and is ejected when there is none. TNT tries the cube before it
// primes, and so does a pumpkin that is not carved.
func TestDispenserFeedsBlocksToSulfurCubes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		it   world.Item
	}{
		{"dirt", block.Dirt{}},
		{"stone", block.Stone{}},
		{"tnt", block.TNT{}},
		{"pumpkin", block.Pumpkin{}},
	} {
		t.Run(c.name+" swallowed", func(t *testing.T) {
			t.Parallel()
			dt := newDispenserTest(t, cube.FaceEast)
			set := sulfurCubeInFront(0, nil)
			dt.dispense(set, item.NewStack(c.it, 2))
			bodies := dt.bodiesOfCubes()
			if len(bodies) != 1 || bodies[0].Empty() || bodies[0].Count() != 1 {
				t.Fatalf("the cube holds %v, want one %T", bodies, c.it)
			}
			if got := dt.slot(0); got.Count() != 1 {
				t.Fatalf("slot holds %v, want one item left", got)
			}
			if items := dt.itemEntities(); len(items) != 0 {
				t.Fatalf("%d items dropped, want none", len(items))
			}
			if tnts := dt.entities("minecraft:tnt"); len(tnts) != 0 {
				t.Fatalf("%d primed TNT, want none", len(tnts))
			}
		})
	}

	t.Run("dirt ejected without a cube", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		dt.dispense(nil, item.NewStack(block.Dirt{}, 2))
		if got := dt.slot(0); got.Count() != 1 {
			t.Fatalf("slot holds %v, want one item left", got)
		}
		if items := dt.itemEntities(); len(items) != 1 {
			t.Fatalf("%d items dropped, want one", len(items))
		}
	})
	t.Run("ejected by a cube that is a baby", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		set := sulfurCubeInFront(-100, nil)
		dt.dispense(set, item.NewStack(block.Dirt{}, 2))
		if bodies := dt.bodiesOfCubes(); len(bodies) != 1 || !bodies[0].Empty() {
			t.Fatalf("the cube holds %v, want nothing", bodies)
		}
		if items := dt.itemEntities(); len(items) != 1 {
			t.Fatalf("%d items dropped, want one", len(items))
		}
	})
	t.Run("ejected by a cube that holds the same block", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		set := sulfurCubeInFront(0, block.Dirt{})
		dt.dispense(set, item.NewStack(block.Dirt{}, 2))
		if items := dt.itemEntities(); len(items) != 1 {
			t.Fatalf("%d items dropped, want one", len(items))
		}
	})
	t.Run("replaces another block", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		set := sulfurCubeInFront(0, block.Stone{})
		dt.dispense(set, item.NewStack(block.Dirt{}, 1))
		bodies := dt.bodiesOfCubes()
		if len(bodies) != 1 || bodies[0].Empty() {
			t.Fatalf("the cube holds %v, want the dirt", bodies)
		}
		if name, _ := bodies[0].Item().EncodeItem(); name != "minecraft:dirt" {
			t.Fatalf("the cube holds %s, want dirt", name)
		}
		if items := dt.itemEntities(); len(items) != 1 {
			t.Fatalf("%d items dropped, want the stone it spat out", len(items))
		}
		if got := dt.slot(0); !got.Empty() {
			t.Fatalf("slot holds %v, want nothing", got)
		}
	})
	t.Run("carved pumpkin is not swallowed", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		set := sulfurCubeInFront(0, nil)
		dt.dispense(set, item.NewStack(block.Pumpkin{Carved: true}, 2))
		if bodies := dt.bodiesOfCubes(); len(bodies) != 1 || !bodies[0].Empty() {
			t.Fatalf("the cube holds %v, want nothing", bodies)
		}
	})
}

// TestDispenserEmptiesSulfurCubeBucket checks the filled bucket behaviour that
// bootStrap registers for Items.SULFUR_CUBE_BUCKET: the cube comes out in front
// and the dispenser holds an empty bucket in its place.
func TestDispenserEmptiesSulfurCubeBucket(t *testing.T) {
	t.Parallel()
	dt := newDispenserTest(t, cube.FaceEast)
	dt.dispense(nil, item.NewStack(item.MobBucket{Entity: "minecraft:sulfur_cube"}, 1))
	if cubes := dt.entities("minecraft:sulfur_cube"); len(cubes) != 1 {
		t.Fatalf("%d sulfur cubes, want one", len(cubes))
	}
	if got := dt.slot(0); got.Empty() || got.Item() != (item.Bucket{}) {
		t.Fatalf("slot holds %v, want an empty bucket", got)
	}
}

// TestDispenserFlintAndSteelPrimesSulfurCube checks Java 26.3
// FlintAndSteelDispenseItemBehavior.tryIgniteExplosiveEntities: flint and steel
// primes an explosive sulfur cube in front without placing fire, and wears down
// by one. A non-explosive cube falls through to placing fire.
func TestDispenserFlintAndSteelPrimesSulfurCube(t *testing.T) {
	t.Parallel()
	t.Run("primes explosive cube without placing fire", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		dt.dispense(sulfurCubeInFront(0, block.TNT{}), item.NewStack(item.FlintAndSteel{}, 1))
		<-dt.w.Exec(func(tx *world.Tx) {
			var cubes []*entity.SulfurCube
			for e := range tx.Entities() {
				if c, ok := e.(*entity.SulfurCube); ok {
					cubes = append(cubes, c)
				}
			}
			if len(cubes) != 1 || !cubes[0].Primed() {
				t.Fatalf("cubes = %v, want one primed sulfur cube", cubes)
			}
			if _, ok := tx.Block(dt.front()).(block.Air); !ok {
				t.Errorf("block in front is %T, want air (no fire placed)", tx.Block(dt.front()))
			}
		})
		if got := dt.slot(0); got.Durability() != got.MaxDurability()-1 {
			t.Fatalf("flint and steel durability = %d of %d, want one less", got.Durability(), got.MaxDurability())
		}
	})
	t.Run("non-explosive cube falls through to fire", func(t *testing.T) {
		t.Parallel()
		dt := newDispenserTest(t, cube.FaceEast)
		dt.dispense(sulfurCubeInFront(0, block.Stone{}), item.NewStack(item.FlintAndSteel{}, 1))
		<-dt.w.Exec(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if c, ok := e.(*entity.SulfurCube); ok && c.Primed() {
					t.Fatal("non-explosive sulfur cube was primed")
				}
			}
			if _, ok := tx.Block(dt.front()).(block.Fire); !ok {
				t.Errorf("block in front is %T, want fire", tx.Block(dt.front()))
			}
		})
	})
}
