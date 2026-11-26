package player

import (
	"log/slog"
	"testing"
	"time"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/client/clienttest"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/item/inventory"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type sharedStateTestClient struct {
	*clienttest.Recorder
	sharedCalls int
	plainCalls  int
	reused      int
}

func (c *sharedStateTestClient) ViewEntityState(e world.Entity) {
	c.plainCalls++
	c.Recorder.ViewEntityState(e)
}

func (c *sharedStateTestClient) ViewEntityStateShared(e world.Entity, b *world.Broadcast) {
	c.sharedCalls++
	if b.Load() != nil {
		c.reused++
	} else {
		b.Store("encoded-state")
	}
	c.Recorder.ViewEntityState(e)
}

func TestUpdateStateSharesBroadcastAcrossViewersAndSelf(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	defer func() { _ = w.Close() }()

	v1 := &sharedStateTestClient{Recorder: clienttest.New()}
	v2 := &sharedStateTestClient{Recorder: clienttest.New()}
	self := &sharedStateTestClient{Recorder: clienttest.New()}
	l1 := world.NewLoader(1, w, v1)
	l2 := world.NewLoader(1, w, v2)
	defer func() {
		<-w.Exec(func(tx *world.Tx) {
			l1.Close(tx)
			l2.Close(tx)
		})
	}()

	start := mgl64.Vec3{0.5, 64, 0.5}
	var closedTx *world.Tx
	var h *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		l1.Move(tx, start)
		l1.Load(tx, 9)
		l2.Move(tx, start)
		l2.Load(tx, 9)
		h = world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "shared", Position: start, GameMode: world.GameModeSurvival, Client: self,
		})
		tx.AddEntity(h)
	})

	// Self is NOT a world viewer via Loader: all three must share one Broadcast and be called once.
	<-w.Exec(func(tx *world.Tx) {
		p := mustPlayer(t, h, tx)
		v1.sharedCalls, v2.sharedCalls, self.sharedCalls = 0, 0, 0
		v1.reused, v2.reused, self.reused = 0, 0, 0

		p.SetNameTag("tag1")
		if v1.sharedCalls != 1 || v2.sharedCalls != 1 || self.sharedCalls != 1 {
			t.Fatalf("sharedCalls = (%d,%d,%d), want (1,1,1)", v1.sharedCalls, v2.sharedCalls, self.sharedCalls)
		}
		if v1.reused+v2.reused+self.reused != 2 {
			t.Fatalf("reused = %d, want 2 (first encodes, remaining 2 reuse)", v1.reused+v2.reused+self.reused)
		}
	})

	// Attach a Loader for self so self IS in tx.Viewers: self must still be called only ONCE.
	lSelf := world.NewLoader(1, w, self)
	defer func() {
		<-w.Exec(func(tx *world.Tx) { lSelf.Close(tx) })
	}()
	<-w.Exec(func(tx *world.Tx) {
		lSelf.Move(tx, start)
		lSelf.Load(tx, 9)
	})

	var stalePlayer *Player
	<-w.Exec(func(tx *world.Tx) {
		closedTx = tx
		p := mustPlayer(t, h, tx)
		stalePlayer = p
		v1.sharedCalls, v2.sharedCalls, self.sharedCalls = 0, 0, 0
		v1.reused, v2.reused, self.reused = 0, 0, 0

		p.SetNameTag("tag2")
		if v1.sharedCalls != 1 || v2.sharedCalls != 1 || self.sharedCalls != 1 {
			t.Fatalf("with self loader, sharedCalls = (%d,%d,%d), want (1,1,1)", v1.sharedCalls, v2.sharedCalls, self.sharedCalls)
		}
		if v1.reused+v2.reused+self.reused != 2 {
			t.Fatalf("with self loader, reused = %d, want 2", v1.reused+v2.reused+self.reused)
		}

		// Simulated player (p.c == nil) broadcasts via world.BroadcastEntityState.
		simH := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "sim", Position: start, GameMode: world.GameModeSurvival,
		})
		sim := tx.AddEntity(simH).(*Player)
		v1.sharedCalls, v2.sharedCalls, self.sharedCalls = 0, 0, 0
		v1.reused, v2.reused, self.reused = 0, 0, 0
		sim.SetNameTag("simtag")
		if v1.sharedCalls != 1 || v2.sharedCalls != 1 || self.sharedCalls != 1 {
			t.Fatalf("simulated sharedCalls = (%d,%d,%d), want (1,1,1)", v1.sharedCalls, v2.sharedCalls, self.sharedCalls)
		}
		if v1.reused+v2.reused+self.reused != 2 {
			t.Fatalf("simulated reused = %d, want 2", v1.reused+v2.reused+self.reused)
		}
		_ = sim.Close()
	})

	// After tx closes, updateState must not panic and must fall back to p.c.ViewEntityState.
	if !closedTx.Closed() {
		t.Fatal("expected tx to be closed")
	}
	self.plainCalls = 0
	stalePlayer.updateState()
	if self.plainCalls != 1 {
		t.Fatalf("closed-tx plainCalls = %d, want 1", self.plainCalls)
	}
	<-w.Exec(func(tx *world.Tx) {
		_ = mustPlayer(t, h, tx).Close()
	})
}

func mustPlayer(t *testing.T, h *world.EntityHandle, tx *world.Tx) *Player {
	t.Helper()
	e, ok := h.Entity(tx)
	if !ok {
		t.Fatal("player handle missing from world")
	}
	return e.(*Player)
}

func TestCheckBlockCollisionsEarlyExitAndShapes(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	defer func() { _ = w.Close() }()

	<-w.Exec(func(tx *world.Tx) {
		for x := -2; x <= 2; x++ {
			for z := -2; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		// Powder snow at player feet to ensure stationary CheckEntityInsiders still runs.
		tx.SetBlock(cube.Pos{0, 64, 0}, block.PowderSnow{}, nil)
		// Solid wall at x=1.
		tx.SetBlock(cube.Pos{1, 64, 0}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{1, 65, 0}, block.Stone{}, nil)

		start := mgl64.Vec3{0.5, 64, 0.5}
		h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "col", Position: start, GameMode: world.GameModeSurvival,
		})
		p := tx.AddEntity(h).(*Player)
		defer func() { _ = p.Close() }()

		p.collidedHorizontally = true
		p.collidedVertically = true
		p.checkBlockCollisions(mgl64.Vec3{})
		if p.collidedHorizontally || p.collidedVertically {
			t.Fatalf("stationary collision flags = (%v,%v), want (false,false)", p.collidedHorizontally, p.collidedVertically)
		}
		if !p.data.InPowderSnow {
			t.Fatal("expected stationary checkBlockCollisions to run CheckEntityInsiders for PowderSnow")
		}

		// Moving into the full-cube stone wall at x=1 must collide horizontally.
		p.checkBlockCollisions(mgl64.Vec3{0.5, 0, 0})
		if !p.collidedHorizontally {
			t.Fatal("expected horizontal collision against full-cube stone wall")
		}
	})
}

func TestInventoryTrackingTurtleHelmetAndAttackStrength(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	defer func() { _ = w.Close() }()

	<-w.Exec(func(tx *world.Tx) {
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		inv := inventory.New(36, nil)
		_ = inv.SetItem(0, item.NewStack(item.Sword{Tier: item.ToolTierIron}, 1))
		start := mgl64.Vec3{0.5, 64, 0.5}
		h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "track", Position: start, GameMode: world.GameModeSurvival, Client: clienttest.New(), Inventory: inv,
		})
		p := tx.AddEntity(h).(*Player)
		defer func() { _ = p.Close() }()

		p.Tick(tx, 1)
		p.Tick(tx, 2)
		if p.attackStrengthTicks != 1 {
			t.Fatalf("attackStrengthTicks = %d, want 1 after steady sword hold", p.attackStrengthTicks)
		}
		// Switching held item must invalidate the cached main-hand state and reset attackStrengthTicks.
		_ = p.Inventory().SetItem(0, item.NewStack(item.Diamond{}, 1))
		p.Tick(tx, 3)
		if p.attackStrengthTicks != 0 {
			t.Fatalf("attackStrengthTicks = %d, want 0 after main-hand item changed", p.attackStrengthTicks)
		}

		// Equipping a turtle helmet must update hasTurtleHelmet and grant Water Breathing out of water.
		if p.hasTurtleHelmet.Load() {
			t.Fatal("expected hasTurtleHelmet=false before equipping")
		}
		p.Armour().SetHelmet(item.NewStack(item.TurtleShell{}, 1))
		if !p.hasTurtleHelmet.Load() {
			t.Fatal("expected hasTurtleHelmet=true after equipping TurtleShell")
		}
		p.Tick(tx, 4)
		if eff, ok := p.Effect(effect.WaterBreathing); !ok || eff.Duration() < 9*time.Second {
			t.Fatalf("expected WaterBreathing effect from TurtleShell, got (%v, %v)", eff, ok)
		}
		p.Armour().SetHelmet(item.Stack{})
		if p.hasTurtleHelmet.Load() {
			t.Fatal("expected hasTurtleHelmet=false after unequipping")
		}
	})
}

func TestSleepAndWakeWithClosedTxFallback(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, Log: slog.New(slog.DiscardHandler), Entities: entity.DefaultRegistry}.New()
	defer func() { _ = w.Close() }()

	head := cube.Pos{2, 64, 0}
	foot := head.Side(cube.FaceSouth)
	var h *world.EntityHandle
	var stalePlayer *Player
	<-w.Exec(func(tx *world.Tx) {
		tx.World().SetTime(18000)
		for x := -1; x <= 3; x++ {
			for z := -1; z <= 2; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(foot, block.Bed{Facing: cube.North, Head: false}, nil)
		tx.SetBlock(head, block.Bed{Facing: cube.North, Head: true}, nil)
		start := mgl64.Vec3{0.5, 64, 0.5}
		h = world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
			Name: "sleeper", Position: start, GameMode: world.GameModeSurvival, Client: clienttest.New(),
		})
		stalePlayer = tx.AddEntity(h).(*Player)
	})

	// stalePlayer.tx is now closed: Sleep and Wake must not panic and must schedule onto the world cleanly.
	stalePlayer.Sleep(head)
	<-w.Exec(func(tx *world.Tx) {
		cur := mustPlayer(t, h, tx)
		if pos, sleeping := cur.Sleeping(); !sleeping || pos != head {
			t.Fatalf("Sleeping() = (%v, %v), want (%v, true)", pos, sleeping, head)
		}
	})
	stalePlayer.Wake()
	<-w.Exec(func(tx *world.Tx) {
		cur := mustPlayer(t, h, tx)
		if _, sleeping := cur.Sleeping(); sleeping {
			t.Fatal("expected player to be awake after Wake on closed tx")
		}
		_ = cur.Close()
	})
}
