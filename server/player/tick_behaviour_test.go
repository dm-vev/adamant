package player

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// tickScenario is one run of TestTickBehaviour: a player in a flat world, set up by setup, that is ticked
// ticks times, with before called ahead of each tick.
type tickScenario struct {
	name  string
	ticks int
	// setup changes the world and the player, standing on the ground, before the first tick.
	setup func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler)
	// before is called with the number of the tick, from 0, ahead of the tick.
	before func(tick int, tx *world.Tx, p *Player)
	// immune keeps the player immune to damage that was taken, as the wall clock decides it.
	immune bool
}

// TestTickBehaviour pins what Player.Tick does over a table of scenarios: for each tick the state of the
// player, the events its handler saw, and the calls the client of the player and a viewer saw, as recorded
// before the method was split along the phases of ServerPlayer.tick.
func TestTickBehaviour(t *testing.T) {
	ground := tickBenchmarkGround
	at := func(tx *world.Tx, p *Player, y float64) { p.data.Pos = mgl64.Vec3{0.5, y, 0.5} }
	scenarios := []tickScenario{
		{name: "idle", ticks: 8},
		{name: "walking", ticks: 12, before: func(tick int, tx *world.Tx, p *Player) {
			p.ClientMove(clientMove(p.Position().Add(mgl64.Vec3{0.2, 0, 0})))
		}},
		{name: "sprint falling", ticks: 12, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			at(tx, p, ground+6)
			p.onGround = false
		}},
		{name: "dead", ticks: 4, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.Hurt(1000, entity.VoidDamageSource{})
		}},
		{name: "sleeping by day", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().SetTime(6000)
			sleepInBed(t, tx, p, block.Bed{Facing: cube.North})
		}},
		{name: "sleeping by night", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().SetTime(18000)
			sleepInBed(t, tx, p, block.Bed{Facing: cube.North})
		}},
		{name: "sleeping and the bed is gone", ticks: 4, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().SetTime(18000)
			head := sleepInBed(t, tx, p, block.Bed{Facing: cube.North})
			tx.SetBlock(head, block.Stone{}, nil)
		}},
		{name: "sleeping in straw", ticks: 4, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().SetTime(18000)
			pos := cube.PosFromVec3(p.Position()).Add(cube.Pos{2, 0, 0})
			tx.SetBlock(pos, block.StrawBed{}, nil)
			p.Sleep(pos)
		}},
		{name: "turtle helmet out of water", ticks: 4, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.Armour().SetHelmet(item.NewStack(item.TurtleShell{}, 1))
			p.StartSwimming()
		}},
		{name: "elytra", ticks: 45, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			at(tx, p, ground+30)
			p.onGround = false
			p.Armour().SetChestplate(item.NewStack(item.Elytra{}, 1).Damage(item.Elytra{}.DurabilityInfo().MaxDurability - 3))
			p.StartGliding()
		}},
		{name: "powder snow", ticks: 30, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.SetBlock(cube.PosFromVec3(p.Position()), block.PowderSnow{}, nil)
		}, immune: false},
		{name: "void", ticks: 12, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			at(tx, p, float64(tx.Range()[0])-5)
			p.onGround = false
		}},
		{name: "void immune", ticks: 6, immune: true, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			at(tx, p, float64(tx.Range()[0])-5)
			p.onGround = false
		}},
		{name: "suffocation", ticks: 12, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			pos := cube.PosFromVec3(p.Position())
			tx.SetBlock(pos, block.Stone{}, nil)
			tx.SetBlock(pos.Side(cube.FaceUp), block.Stone{}, nil)
		}},
		{name: "on fire", ticks: 50, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetOnFire(2 * time.Second)
		}},
		{name: "on fire creative", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeCreative)
			p.SetOnFire(2 * time.Second)
		}},
		{name: "on fire in the rain", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			tx.World().StartRaining(time.Hour)
			p.SetOnFire(2 * time.Second)
		}},
		{name: "blocking delay", ticks: 14, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetBlockingDelay(500 * time.Millisecond)
		}},
		{name: "drawing a bow", ticks: 30, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
			if _, err := p.Inventory().AddItem(item.NewStack(item.Arrow{}, 64)); err != nil {
				t.Fatal(err)
			}
			p.UseItem()
		}},
		{name: "eating", ticks: 40, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.hunger.foodLevel = 10
			p.SetHeldItems(item.NewStack(item.Bread{}, 3), item.Stack{})
			p.UseItem()
		}},
		{name: "breaking stone", ticks: 30, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			pos := cube.PosFromVec3(p.Position()).Add(cube.Pos{1, 0, 0})
			tx.SetBlock(pos, block.Stone{}, nil)
			p.StartBreaking(pos, cube.FaceWest)
		}},
		{name: "changed world", ticks: 3, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.prevWorld = world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
			t.Cleanup(func() { _ = p.prevWorld.Close() })
		}},
		{name: "spectator", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetGameMode(world.GameModeSpectator)
		}},
		{name: "immobile", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			at(tx, p, ground+4)
			p.onGround = false
			p.SetImmobile()
		}},
		{name: "hungry", ticks: 10, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.hunger.foodLevel, p.hunger.saturationLevel, p.hunger.exhaustionLevel = 0, 0, 3.9
			p.hunger.foodTick = 78
			p.addHealth(-10)
		}},
		{name: "full and healing", ticks: 12, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.hunger.foodLevel, p.hunger.saturationLevel = 20, 5
			p.hunger.foodTick = 8
			p.addHealth(-10)
		}},
		{name: "under water", ticks: 30, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			pos := cube.PosFromVec3(p.Position())
			for dy := range 3 {
				tx.SetLiquid(pos.Add(cube.Pos{0, dy, 0}), block.Water{Still: true, Depth: 8})
			}
		}},
		{name: "regeneration and poison", ticks: 30, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.AddEffect(effect.New(effect.Regeneration, 2, 3*time.Second))
			p.AddEffect(effect.New(effect.Poison, 1, 3*time.Second))
		}},
		{name: "weapon cooldown", ticks: 6, setup: func(t *testing.T, tx *world.Tx, p *Player, h *traceHandler) {
			p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1), item.Stack{})
		}},
	}
	tr := &transcript{}
	for _, s := range scenarios {
		runTickScenario(t, tr, s)
	}
	checkTranscript(t, "tick", tr)
}

func runTickScenario(t *testing.T, tr *transcript, s tickScenario) {
	t.Helper()
	w := tickBenchmarkWorld(t)
	viewer := clienttest.New()
	loader := world.NewLoader(2, w, viewer)
	r := clienttest.New()
	start := mgl64.Vec3{0.5, tickBenchmarkGround, 0.5}
	h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
		Name: "ticker", Position: start, GameMode: world.GameModeSurvival, Client: r,
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, start)
		loader.Load(tx, 25)
		p := tx.AddEntity(h).(*Player)
		th := &traceHandler{tr: tr, cancel: map[string]bool{}}
		p.Handle(th)
		tr.linef("== %s", s.name)
		// Past the grace of the join.
		for tick := range int64(62) {
			p.ClientMove(clientMove(p.Position()))
			p.Tick(tx, tick)
		}
		if s.setup != nil {
			s.setup(t, tx, p, th)
		}
		viewer.Reset()
		r.Reset()
		tr.linef("start %s", digest(p))
		for i := range s.ticks {
			if s.before != nil {
				s.before(i, tx, p)
			}
			if s.immune {
				p.immuneUntil = time.Now().Add(time.Hour)
			} else {
				p.immuneUntil = time.Time{}
			}
			p.Tick(tx, int64(62+i))
			tr.linef("tick %d %s", i, digest(p))
			tr.calls("viewer", viewer)
			tr.calls("client", r)
		}
		_ = p.Close()
	})
	<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
}

// clientMove is the move of a client to pos.
func clientMove(pos mgl64.Vec3) client.Move { return client.Move{Position: pos} }

// sleepInBed places the bed passed three blocks east of the player, with its head to the north, and puts the
// player to sleep in it. It returns the position of the head.
func sleepInBed(t *testing.T, tx *world.Tx, p *Player, bed block.Bed) cube.Pos {
	t.Helper()
	foot := cube.PosFromVec3(p.Position()).Add(cube.Pos{3, 0, 0})
	head := foot.Side(cube.FaceNorth)
	bed.Facing, bed.Head = cube.North, false
	tx.SetBlock(foot, bed, nil)
	bed.Head = true
	tx.SetBlock(head, bed, nil)
	p.Sleep(head)
	if _, sleeping := p.Sleeping(); !sleeping {
		t.Fatal("the player did not sleep")
	}
	return head
}
