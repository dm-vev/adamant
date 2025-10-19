package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func TestRespawnAnchorExplodesOutsideNether(t *testing.T) {
	conf := world.Config{Dim: world.Overworld}
	w := conf.New()
	t.Cleanup(func() { _ = w.Close() })

	pos := cube.Pos{}
	anchor := RespawnAnchor{}

	var (
		activated bool
		consumed  int
		final     world.Block
	)

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, anchor, nil)
		ctx := &item.UseContext{}
		sleeper := &fakeRespawnSleeper{main: item.NewStack(Glowstone{}, 1)}

		activated = anchor.Activate(pos, cube.FaceUp, tx, sleeper, ctx)
		consumed = ctx.CountSub
		final = tx.Block(pos)
	})

	if !activated {
		t.Fatalf("expected activation to succeed")
	}
	if consumed != 1 {
		t.Fatalf("expected glowstone to be consumed, got %d", consumed)
	}
	if _, ok := final.(Air); !ok {
		t.Fatalf("expected anchor to explode outside nether, got %T", final)
	}
}

func TestRespawnAnchorChargesInNether(t *testing.T) {
	conf := world.Config{Dim: world.Nether}
	w := conf.New()
	t.Cleanup(func() { _ = w.Close() })

	pos := cube.Pos{}
	anchor := RespawnAnchor{}

	var (
		activated bool
		consumed  int
		charged   RespawnAnchor
		ok        bool
	)

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(pos, anchor, nil)
		ctx := &item.UseContext{}
		sleeper := &fakeRespawnSleeper{main: item.NewStack(Glowstone{}, 1)}

		activated = anchor.Activate(pos, cube.FaceUp, tx, sleeper, ctx)
		consumed = ctx.CountSub
		charged, ok = tx.Block(pos).(RespawnAnchor)
	})

	if !activated {
		t.Fatalf("expected activation to succeed")
	}
	if consumed != 1 {
		t.Fatalf("expected glowstone to be consumed, got %d", consumed)
	}
	if !ok {
		t.Fatalf("expected respawn anchor block, got different type")
	}
	if charged.Charge != 1 {
		t.Fatalf("expected anchor charge to increase to 1, got %d", charged.Charge)
	}
}

func TestRespawnAnchorSafeSpawnSkipsHazards(t *testing.T) {
	conf := world.Config{Dim: world.Nether}
	w := conf.New()
	t.Cleanup(func() { _ = w.Close() })

	var (
		spawn cube.Pos
		ok    bool
	)

	base := cube.Pos{}
	anchor := RespawnAnchor{Charge: 1}

	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(base, anchor, nil)
		tx.SetLiquid(base.Add(cube.Pos{0, 1, 0}), Lava{}.WithDepth(8, false))
		tx.SetBlock(base.Add(cube.Pos{-1, 0, 0}), Stone{}, nil)

		spawn, ok = anchor.SafeSpawn(base, tx)
	})

	if !ok {
		t.Fatalf("expected to find a safe spawn position")
	}
	if spawn != (cube.Pos{-1, 1, 0}) {
		t.Fatalf("expected safe spawn at {-1,1,0}, got %v", spawn)
	}
}

type fakeRespawnSleeper struct {
	main item.Stack
	msg  []chat.Translation
	id   uuid.UUID
}

func (f *fakeRespawnSleeper) Close() error                           { return nil }
func (f *fakeRespawnSleeper) H() *world.EntityHandle                 { return &world.EntityHandle{} }
func (f *fakeRespawnSleeper) Position() mgl64.Vec3                   { return mgl64.Vec3{} }
func (f *fakeRespawnSleeper) Rotation() cube.Rotation                { return cube.Rotation{} }
func (f *fakeRespawnSleeper) HeldItems() (item.Stack, item.Stack)    { return f.main, item.Stack{} }
func (f *fakeRespawnSleeper) SetHeldItems(main, _ item.Stack)        { f.main = main }
func (f *fakeRespawnSleeper) UsingItem() bool                        { return false }
func (f *fakeRespawnSleeper) ReleaseItem()                           {}
func (f *fakeRespawnSleeper) UseItem()                               {}
func (f *fakeRespawnSleeper) Messaget(tr chat.Translation, _ ...any) { f.msg = append(f.msg, tr) }
func (f *fakeRespawnSleeper) UUID() uuid.UUID {
	if f.id == uuid.Nil {
		f.id = uuid.New()
	}
	return f.id
}
