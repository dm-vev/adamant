package entity_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// villagerWorld returns a world stopped at the time of day passed.
func villagerWorld(t *testing.T, time int) *world.World {
	t.Helper()
	w := wolfTestWorld(t)
	w.SetTime(time)
	w.StopTime()
	return w
}

// placeBed places a bed with its head at pos and its foot to the south.
func placeBed(tx *world.Tx, pos cube.Pos) {
	tx.SetBlock(pos.Side(cube.FaceSouth), block.Bed{Facing: cube.North}, nil)
	tx.SetBlock(pos, block.Bed{Facing: cube.North, Head: true}, nil)
}

// TestVillagersTakeJobs checks AcquirePoi, GoToPotentialJobSite and
// AssignProfessionFromJobSite: an unemployed villager takes a lectern near
// and becomes a librarian.
func TestVillagersTakeJobs(t *testing.T) {
	w := villagerWorld(t, 1000)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{6, 64, 0}, block.Lectern{}, nil)
		h = addVillager(t, tx, mgl64.Vec3{.5, 64, .5}).H()
	})
	for range 1200 {
		w.AdvanceTick()
		hired := false
		<-w.Exec(func(tx *world.Tx) { hired = villager(tx, h).Profession() == "minecraft:librarian" })
		if hired {
			return
		}
	}
	t.Fatal("unemployed villager did not become a librarian at a lectern six blocks away in a minute")
}

// TestVillagersSleepAtNight checks the rest schedule, AcquirePoi for homes
// and SleepInBed: at night a villager sleeps in a bed near, which is then
// occupied.
func TestVillagersSleepAtNight(t *testing.T) {
	w := villagerWorld(t, 13000)
	var h *world.EntityHandle
	bed := cube.Pos{5, 64, 0}
	<-w.Exec(func(tx *world.Tx) {
		placeBed(tx, bed)
		h = addVillager(t, tx, mgl64.Vec3{.5, 64, .5}).H()
	})
	for range 1200 {
		w.AdvanceTick()
		asleep := false
		<-w.Exec(func(tx *world.Tx) {
			pos, sleeping := villager(tx, h).Sleeping()
			if sleeping {
				asleep = true
				if pos != bed {
					t.Fatalf("villager sleeps at %v, want the bed at %v", pos, bed)
				}
				if b, ok := tx.Block(bed).(block.Bed); !ok || !b.Occupied {
					t.Fatal("bed a villager sleeps in is not occupied")
				}
			}
		})
		if asleep {
			// Morning wakes it up.
			w.SetTime(1000)
			for range 100 {
				w.AdvanceTick()
			}
			<-w.Exec(func(tx *world.Tx) {
				if _, sleeping := villager(tx, h).Sleeping(); sleeping {
					t.Fatal("villager still sleeps in the morning")
				}
				if b, _ := tx.Block(bed).(block.Bed); b.Occupied {
					t.Fatal("bed still occupied after the villager woke")
				}
			})
			return
		}
	}
	t.Fatal("villager did not sleep in a bed five blocks away at night in a minute")
}

// TestFarmersBakeBread checks WorkAtComposter.makeBread: a farmer at its
// composter while at work bakes bread out of its wheat.
func TestFarmersBakeBread(t *testing.T) {
	w := villagerWorld(t, 3000)
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{2, 64, 0}, block.Composter{}, nil)
		v := addVillager(t, tx, mgl64.Vec3{.5, 64, .5})
		v.SetProfession("minecraft:farmer")
		_, _ = v.Inventory().AddItem(item.NewStack(item.Wheat{}, 6))
		h = v.H()
	})
	for range 2400 {
		w.AdvanceTick()
		baked := false
		<-w.Exec(func(tx *world.Tx) {
			for _, s := range villager(tx, h).Inventory().Slots() {
				if _, ok := s.Item().(item.Bread); ok && !s.Empty() {
					baked = true
				}
			}
		})
		if baked {
			return
		}
	}
	t.Fatal("farmer with wheat at its composter did not bake bread in two minutes")
}

// TestVillagersBreed checks VillagerMakeLove: two villagers with food and a
// bed free beside their own have a baby that gets the bed.
func TestVillagersBreed(t *testing.T) {
	w := villagerWorld(t, 500)
	<-w.Exec(func(tx *world.Tx) {
		// A pen keeps them close enough to court.
		for x := -8; x <= 8; x++ {
			for z := -4; z <= 9; z++ {
				if x == -8 || x == 8 || z == -4 || z == 9 {
					tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
					tx.SetBlock(cube.Pos{x, 65, z}, block.Stone{}, nil)
				}
			}
		}
		for _, x := range []int{-6, 0, 6} {
			placeBed(tx, cube.Pos{x, 64, 6})
		}
		for _, x := range []float64{.5, 2.5} {
			v := addVillager(t, tx, mgl64.Vec3{x, 64, .5})
			_, _ = v.Inventory().AddItem(item.NewStack(item.Bread{}, 3))
		}
	})
	for range 3000 {
		w.AdvanceTick()
		baby := false
		<-w.Exec(func(tx *world.Tx) {
			for e := range tx.Entities() {
				if v, ok := e.(*entity.Villager); ok && v.Baby() {
					baby = true
				}
			}
		})
		if baby {
			return
		}
	}
	t.Fatal("two fed villagers with a bed to spare had no baby in two and a half minutes")
}
