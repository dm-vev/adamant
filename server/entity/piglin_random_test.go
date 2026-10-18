package entity

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

const piglinRandomLifecycleChild = "DRAGONFLY_PIGLIN_RANDOM_LIFECYCLE_CHILD"

func TestPiglinRandomStreamsSurviveSpawnTickAndReopen(t *testing.T) {
	if os.Getenv(piglinRandomLifecycleChild) == "1" {
		testPiglinRandomStreamsSurviveSpawnTickAndReopen(t)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPiglinRandomStreamsSurviveSpawnTickAndReopen$")
	cmd.Env = append(os.Environ(), piglinRandomLifecycleChild+"=1")
	output, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("piglin spawn and wander tick did not complete before the deadline; output: %s", output)
	}
	if err != nil {
		t.Fatalf("piglin random lifecycle child failed: %v\n%s", err, output)
	}
}

func testPiglinRandomStreamsSurviveSpawnTickAndReopen(t *testing.T) {
	w := world.Config{Entities: DefaultRegistry, Synchronous: true, RandomTickSpeed: -1}.New()
	w.Freeze()
	t.Cleanup(func() {
		w.Unfreeze()
		_ = w.Close()
	})

	piglinID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	bruteID := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	piglinPos, brutePos := mgl64.Vec3{.5, 64, .5}, mgl64.Vec3{40.5, 64, .5}
	var piglinHandle, bruteHandle *world.EntityHandle
	if err := w.Do(func(tx *world.Tx) {
		for _, x0 := range []int{-8, 32} {
			for x := x0; x <= x0+16; x++ {
				for z := -8; z <= 8; z++ {
					tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
				}
			}
		}
		piglinHandle = tx.AddEntity(NewPiglin(world.EntitySpawnOpts{ID: piglinID, Position: piglinPos})).H()
		bruteHandle = tx.AddEntity(NewPiglinBrute(world.EntitySpawnOpts{ID: bruteID, Position: brutePos})).H()
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}

	if err := w.StepTicks(1); err != nil {
		t.Fatal(err)
	}
	w.AdvanceTick()

	var failure string
	if err := w.Do(func(tx *world.Tx) {
		for _, actor := range []struct {
			name   string
			handle *world.EntityHandle
			id     uuid.UUID
		}{
			{name: "piglin", handle: piglinHandle, id: piglinID},
			{name: "piglin brute", handle: bruteHandle, id: bruteID},
		} {
			want := java26.NewLegacyRandom(int64(fnv64(actor.id[:])))
			want.NextInt(11)
			want.NextInt(11)
			wantState := want.State()
			for open := range 2 {
				entity, ok := actor.handle.Entity(tx)
				if !ok {
					failure = actor.name + " could not be reopened in its owning world"
					return
				}
				var state *piglinState
				switch e := entity.(type) {
				case *Piglin:
					state = e.p()
				case *PiglinBrute:
					state = e.p()
				default:
					failure = fmt.Sprintf("%s reopened as %T", actor.name, entity)
					return
				}
				if got := state.random.State(); got != wantState {
					failure = fmt.Sprintf("%s random state after its first wander tick and reopen %d = %+v, want Java UUID stream after two wander draws %+v", actor.name, open+1, got, wantState)
					return
				}
			}
		}
	}).Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	if failure != "" {
		t.Fatal(failure)
	}
}
