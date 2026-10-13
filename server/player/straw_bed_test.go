package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPlayerStrawBedSleepWakeDoesNotSetRespawn(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	w.SetTime(18000)
	foot, head := cube.Pos{257, 70, -257}, cube.Pos{257, 70, -256}
	spawn := cube.Pos{300, 80, -300}
	origin := foot.Vec3Middle()
	handle := world.EntitySpawnOpts{Position: origin}.New(Type, Config{Position: origin, GameMode: world.GameModeSurvival, RespawnPosition: spawn, RespawnDimension: world.Overworld, HasRespawnPoint: true})
	job := w.Do(func(tx *world.Tx) {
		tx.SetBlock(foot, block.StrawBed{Facing: cube.South}, nil)
		tx.SetBlock(head, block.StrawBed{Facing: cube.South, Head: true}, nil)
		p := tx.AddEntity(handle).(*Player)
		if !tx.Block(foot).(block.StrawBed).Activate(foot, cube.FaceUp, tx, p, &item.UseContext{}) {
			t.Fatal("straw bed interaction not consumed")
		}
		if pos, sleeping := p.Sleeping(); !sleeping || pos != head {
			t.Fatalf("sleep=%v/%v want head=%v", pos, sleeping, head)
		}
		if want := head.Vec3Middle().Add(mgl64.Vec3{0, .375}); p.Position() != want {
			t.Fatalf("sleep position=%v Source=%v", p.Position(), want)
		}
		if p.respawnPosition != spawn || p.respawnDimension != world.Overworld {
			t.Fatal("disposable bed changed respawn point")
		}
		if !tx.Block(head).(block.StrawBed).Occupied {
			t.Fatal("sleep did not occupy head")
		}
		p.Wake()
		if _, sleeping := p.Sleeping(); sleeping {
			t.Fatal("player remained asleep")
		}
		for _, pos := range [...]cube.Pos{head, foot} {
			if _, air := tx.Block(pos).(block.Air); !air {
				t.Fatalf("Overworld straw bed half survived leaving at %v", pos)
			}
		}
		if p.respawnPosition != spawn {
			t.Fatal("wake changed respawn point")
		}
		t.Logf("actual player slept at Source elevation .375; occupied head; wake removed pair and preserved respawn=%v", spawn)
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerStrawBedMissingFootCannotStartSleeping(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	head := cube.Pos{257, 70, -256}
	origin := head.Vec3Middle()
	handle := world.EntitySpawnOpts{Position: origin}.New(Type, Config{Position: origin, GameMode: world.GameModeSurvival})
	job := w.Do(func(tx *world.Tx) {
		tx.SetBlock(head, block.StrawBed{Facing: cube.South, Head: true}, nil)
		p := tx.AddEntity(handle).(*Player)
		p.Sleep(head)
		if _, sleeping := p.Sleeping(); sleeping {
			t.Fatal("head without foot accepted Source sleep height")
		}
		if p.Position() != origin || tx.Block(head).(block.StrawBed).Occupied {
			t.Fatal("rejected sleep mutated player/bed")
		}
	})
	<-job.Done()
	if err := job.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestStrawBedOtherDimensionsDestroyOnUseWithoutExplosion(t *testing.T) {
	for index, dimension := range []world.Dimension{world.Nether, world.End} {
		t.Run([]string{"nether", "end"}[index], func(t *testing.T) {
			w := world.Config{Dim: dimension, Synchronous: true, RandomTickSpeed: -1}.New()
			t.Cleanup(func() { _ = w.Close() })
			foot, head, nearby := cube.Pos{257, 70, -257}, cube.Pos{257, 70, -256}, cube.Pos{258, 70, -256}
			job := w.Do(func(tx *world.Tx) {
				tx.SetBlock(foot, block.StrawBed{Facing: cube.South}, nil)
				tx.SetBlock(head, block.StrawBed{Facing: cube.South, Head: true}, nil)
				tx.SetBlock(nearby, block.Stone{}, nil)
				if !tx.Block(foot).(block.StrawBed).Activate(foot, cube.FaceUp, tx, nil, &item.UseContext{}) {
					t.Fatal("destroy-on-use not consumed")
				}
				if _, air := tx.Block(head).(block.Air); !air {
					t.Fatal("destroy-on-use left head")
				}
				if _, stone := tx.Block(nearby).(block.Stone); !stone {
					t.Fatal("straw mat incorrectly exploded")
				}
			})
			<-job.Done()
			if err := job.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
