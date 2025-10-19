package block

import (
	"runtime"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

func init() {
	worldFinaliseBlockRegistry()
}

func TestTryCreateEndPortalCompletesRing(t *testing.T) {
	w := world.Config{}.New()
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
			origin := cube.Pos{0, 64, 0}
			loader.Move(tx, origin.Vec3Centre())
			loader.Load(tx, 1)

			axisX := directionVec(cube.East)
			axisZ := directionVec(cube.South)

			for _, inverted := range []bool{false, true} {
				for _, offset := range endPortalFrameOffsets {
					pos := origin.Add(applyEndPortalOffset(offset, axisX, axisZ))
					facing := facingForOffset(offset, cube.East, cube.South)
					if inverted {
						facing = facing.Opposite()
					}
					tx.SetBlock(pos, EndPortalFrame{Facing: facing, Eye: true}, nil)
				}

				placed := origin.Add(applyEndPortalOffset(endPortalFrameOffsets[0], axisX, axisZ))
				tryCreateEndPortal(tx, placed)

				for _, offset := range endPortalInteriorOffsets {
					pos := origin.Add(applyEndPortalOffset(offset, axisX, axisZ))
					if _, ok := tx.Block(pos).(EndPortal); !ok {
						t.Fatalf("interior block at %v not end portal: %T", pos, tx.Block(pos))
					}
					tx.SetBlock(pos, Air{}, nil)
				}

				for _, offset := range endPortalFrameOffsets {
					pos := origin.Add(applyEndPortalOffset(offset, axisX, axisZ))
					facing := facingForOffset(offset, cube.East, cube.South)
					if inverted {
						facing = facing.Opposite()
					}
					tx.SetBlock(pos, EndPortalFrame{Facing: facing, Eye: true}, nil)
				}
			}
		})
	}()

	timer := time.AfterFunc(5*time.Second, func() {
		buf := make([]byte, 1<<16)
		n := runtime.Stack(buf, true)
		panic("end portal creation transaction timed out:\n" + string(buf[:n]))
	})

	<-done
	timer.Stop()
}
