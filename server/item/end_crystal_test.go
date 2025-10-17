package item_test

import (
	"runtime"
	"testing"
	"time"
	_ "unsafe"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func init() {
	worldFinaliseBlockRegistry()
}

//go:linkname worldFinaliseBlockRegistry github.com/df-mc/dragonfly/server/world.finaliseBlockRegistry
func worldFinaliseBlockRegistry()

func TestEndCrystalUseOnBlockReturns(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry}.New()
	defer w.Close()

	loader := world.NewLoader(2, w, world.NopViewer{})
	defer func() {
		<-w.Exec(func(tx *world.Tx) {
			loader.Close(tx)
		})
	}()

	done := make(chan struct{})
	go func() {
		<-w.Exec(func(tx *world.Tx) {
			base := cube.Pos{0, 64, 0}
			tx.SetBlock(base, block.Obsidian{}, nil)

			loader.Move(tx, base.Vec3Centre())
			loader.Load(tx, 1)

			ctx := item.UseContext{}
			if !(item.EndCrystal{}).UseOnBlock(base, cube.FaceUp, mgl64.Vec3{}, tx, nil, &ctx) {
				t.Errorf("use on block failed")
			}
		})
		close(done)
	}()

	timer := time.AfterFunc(2*time.Second, func() {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		panic("end crystal placement transaction timed out:\n" + string(buf[:n]))
	})

	<-done
	timer.Stop()
}

func TestEndCrystalUseOnBlockAllowsSideFace(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry}.New()
	defer w.Close()

	loader := world.NewLoader(2, w, world.NopViewer{})
	defer func() {
		<-w.Exec(func(tx *world.Tx) {
			loader.Close(tx)
		})
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		<-w.Exec(func(tx *world.Tx) {
			base := cube.Pos{0, 64, 0}
			tx.SetBlock(base, block.Obsidian{}, nil)

			loader.Move(tx, base.Vec3Centre())
			loader.Load(tx, 1)

			ctx := item.UseContext{}
			if !(item.EndCrystal{}).UseOnBlock(base, cube.FaceNorth, mgl64.Vec3{}, tx, nil, &ctx) {
				t.Errorf("use on block with side face failed")
			}
		})
	}()

	timer := time.AfterFunc(2*time.Second, func() {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		panic("end crystal placement transaction timed out:\n" + string(buf[:n]))
	})

	<-done
	timer.Stop()
}

func TestEndCrystalPlacementPosition(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry}.New()
	defer w.Close()

	loader := world.NewLoader(2, w, world.NopViewer{})
	defer func() {
		<-w.Exec(func(tx *world.Tx) {
			loader.Close(tx)
		})
	}()

	type result struct {
		pos   mgl64.Vec3
		found bool
	}

	resCh := make(chan result, 1)

	go func() {
		<-w.Exec(func(tx *world.Tx) {
			base := cube.Pos{0, 64, 0}
			tx.SetBlock(base, block.Obsidian{}, nil)

			loader.Move(tx, base.Vec3Centre())
			loader.Load(tx, 1)

			ctx := item.UseContext{}
			if !(item.EndCrystal{}).UseOnBlock(base, cube.FaceUp, mgl64.Vec3{}, tx, nil, &ctx) {
				resCh <- result{}
				return
			}

			var (
				pos   mgl64.Vec3
				found bool
			)
			for e := range tx.Entities() {
				if crystal, ok := e.(*entity.EndCrystal); ok {
					pos = crystal.Position()
					found = true
					break
				}
			}
			resCh <- result{pos: pos, found: found}
		})
	}()

	timer := time.AfterFunc(2*time.Second, func() {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		panic("end crystal placement inspection timed out:\n" + string(buf[:n]))
	})

	res := <-resCh
	timer.Stop()

	if !res.found {
		t.Fatalf("end crystal entity not found after placement")
	}

	expectedY := float64(65)
	if diff := res.pos[1] - expectedY; diff < -1e-6 || diff > 1e-6 {
		t.Fatalf("unexpected crystal Y position: got %f, want %f", res.pos[1], expectedY)
	}
}
