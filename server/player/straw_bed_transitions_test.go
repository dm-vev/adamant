package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestStrawBedSleepRestrictions(t *testing.T) {
	for _, tc := range []struct {
		name                              string
		time                              int
		creative, monster, slime, outside bool
		obstruction                       world.Block
		wantSleep                         bool
	}{
		{name: "clear_before_dusk", time: 12522},
		{name: "clear_at_dusk", time: 12523, wantSleep: true},
		{name: "clear_before_dawn", time: 23477, wantSleep: true},
		{name: "clear_at_dawn", time: 23478},
		{name: "stone_suffocates", time: 18000, obstruction: block.Stone{}},
		{name: "glass_collision_is_not_suffocation", time: 18000, obstruction: block.Glass{}, wantSleep: true},
		{name: "partial_farmland_still_suffocates", time: 18000, obstruction: block.Farmland{}},
		{name: "nearby_zombie_prevents_rest", time: 18000, monster: true},
		{name: "creative_ignores_nearby_zombie", time: 18000, monster: true, creative: true, wantSleep: true},
		{name: "slime_is_not_source_monster", time: 18000, slime: true, wantSleep: true},
		{name: "zombie_outside_query", time: 18000, monster: true, outside: true, wantSleep: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			w.SetTime(tc.time)
			foot, head := cube.Pos{0, 65, 0}, cube.Pos{0, 65, -1}
			mode := world.GameMode(world.GameModeSurvival)
			if tc.creative {
				mode = world.GameModeCreative
			}
			handle := world.EntitySpawnOpts{Position: foot.Vec3Middle()}.New(Type, Config{Position: foot.Vec3Middle(), GameMode: mode})
			job := w.Do(func(tx *world.Tx) {
				tx.SetBlock(foot, block.StrawBed{Facing: cube.North}, nil)
				tx.SetBlock(head, block.StrawBed{Facing: cube.North, Head: true}, nil)
				if tc.obstruction != nil {
					tx.SetBlock(head.Side(cube.FaceUp), tc.obstruction, nil)
				}
				p := tx.AddEntity(handle).(*Player)
				mobPos := head.Vec3Middle().Add(mgl64.Vec3{7, 0, 0})
				if tc.outside {
					mobPos[0] = head.Vec3Middle()[0] + 9
				}
				if tc.monster {
					tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mobPos}))
				}
				if tc.slime {
					tx.AddEntity(entity.NewSlime(world.EntitySpawnOpts{Position: mobPos}))
				}
				tx.Block(foot).(block.StrawBed).Activate(foot, cube.FaceUp, tx, p, &item.UseContext{})
				if _, sleeping := p.Sleeping(); sleeping != tc.wantSleep {
					t.Fatalf("sleeping=%v want=%v", sleeping, tc.wantSleep)
				}
				if tx.Block(head).(block.StrawBed).Occupied != tc.wantSleep {
					t.Fatal("bed occupancy differs from accepted sleep")
				}
				if !tc.wantSleep && p.Position() != foot.Vec3Middle() {
					t.Fatal("rejected sleep moved player")
				}
			})
			<-job.Done()
			if err := job.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStrawBedSleepDismountsAndClearsMotion(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(18000)
	foot, head := cube.Pos{0, 65, 0}, cube.Pos{0, 65, -1}
	job := w.Do(func(tx *world.Tx) {
		tx.SetBlock(foot, block.StrawBed{Facing: cube.North}, nil)
		tx.SetBlock(head, block.StrawBed{Facing: cube.North, Head: true}, nil)
		cushion := tx.AddEntity(entity.NewCushion(world.EntitySpawnOpts{Position: mgl64.Vec3{2.5, 65, .5}}, 0)).(*entity.Cushion)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: foot.Vec3Middle(), Velocity: mgl64.Vec3{.2, .5, .7}}.New(Type, Config{Position: foot.Vec3Middle(), GameMode: world.GameModeSurvival})).(*Player)
		if !cushion.Mount(tx, p) {
			t.Fatal("player could not mount cushion")
		}
		tx.Block(foot).(block.StrawBed).Activate(foot, cube.FaceUp, tx, p, &item.UseContext{})
		if _, sleeping := p.Sleeping(); !sleeping {
			t.Fatal("rider did not enter sleep")
		}
		if p.Riding() != nil || cushion.Passenger() != nil {
			t.Fatal("sleep retained vehicle/passenger link")
		}
		if p.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("sleep retained motion=%v", p.Velocity())
		}
		if p.Position() != head.Vec3Middle().Add(mgl64.Vec3{0, .375}) {
			t.Fatalf("rider sleep position=%v", p.Position())
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestStrawBedTickWakesAtSourceDawn(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(23477)
	foot, head := cube.Pos{0, 65, 0}, cube.Pos{0, 65, -1}
	job := w.Do(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(foot, block.StrawBed{Facing: cube.North}, nil)
		tx.SetBlock(head, block.StrawBed{Facing: cube.North, Head: true}, nil)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: foot.Vec3Middle()}.New(Type, Config{Position: foot.Vec3Middle(), GameMode: world.GameModeSurvival})).(*Player)
		tx.Block(foot).(block.StrawBed).Activate(foot, cube.FaceUp, tx, p, &item.UseContext{})
		if _, sleeping := p.Sleeping(); !sleeping {
			t.Fatal("Source pre-dawn sleep rejected")
		}
		w.SetTime(23478)
		p.Tick(tx, 1)
		if _, sleeping := p.Sleeping(); sleeping {
			t.Fatal("player remained asleep in Source daylight")
		}
		for _, pos := range [...]cube.Pos{head, foot} {
			if _, air := tx.Block(pos).(block.Air); !air {
				t.Fatalf("wake left bed half at %v", pos)
			}
		}
		if want := (mgl64.Vec3{1.5, 65, -.5}); p.Position() != want {
			t.Fatalf("wake position=%v want=%v", p.Position(), want)
		}
		// Look from the accepted stand-up position to this head is west: Source
		// Mth.atan2(0, -1) is PI; the final float yaw matches east_fractional.
		if got := math.Float32bits(float32(p.Rotation().Yaw())); got != 0x42b3ffff {
			t.Fatalf("wake yaw bits=%08x Source=42b3ffff", got)
		}
		if p.Rotation().Pitch() != 0 {
			t.Fatal("wake retained pitch")
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestStrawBedWakeFloatingUsesSourceFallback(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(18000)
	foot, head := cube.Pos{0, 65, 0}, cube.Pos{0, 65, -1}
	job := w.Do(func(tx *world.Tx) {
		tx.SetBlock(foot, block.StrawBed{Facing: cube.North}, nil)
		tx.SetBlock(head, block.StrawBed{Facing: cube.North, Head: true}, nil)
		p := tx.AddEntity(world.EntitySpawnOpts{Position: foot.Vec3Middle()}.New(Type, Config{Position: foot.Vec3Middle(), GameMode: world.GameModeSurvival})).(*Player)
		p.Sleep(head)
		p.Wake()
		if want := (mgl64.Vec3{.5, 66.1, -.5}); p.Position() != want {
			t.Fatalf("floating wake position=%v Source fallback=%v", p.Position(), want)
		}
		if p.Rotation().Yaw() != -90 {
			t.Fatalf("fallback yaw=%v want=-90", p.Rotation().Yaw())
		}
		for _, pos := range [...]cube.Pos{head, foot} {
			if _, air := tx.Block(pos).(block.Air); !air {
				t.Fatalf("wake left bed half at %v", pos)
			}
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}
