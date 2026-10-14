package builtin

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// TestRotateTurnsAnEntity checks RotateCommand: absolute and source-relative
// rotations, facing a position, and rejecting selectors of several entities.
func TestRotateTurnsAnEntity(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	rotate := cmd.New("rotate", "", nil, rotateCommand{}, rotateFacingCommand{}, rotateFacingEntityCommand{})
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 64, 0.5}}))
		check := func(args string, yaw, pitch float64) {
			t.Helper()
			if out := runTestCommand(rotate, args, tx); out.ErrorCount() != 0 {
				t.Fatalf("/rotate %s failed: %v", args, out.Errors())
			}
			got := cow.Rotation()
			if math.Abs(got.Yaw()-yaw) > 1e-6 || math.Abs(got.Pitch()-pitch) > 1e-6 {
				t.Fatalf("/rotate %s turned the cow to %v, want yaw %v pitch %v", args, got, yaw, pitch)
			}
		}
		check("@e[type=cow] 90 10", 90, 10)
		// The console looks south, so relative angles count from zero.
		check("@e[type=cow] ~45 ~", 45, 0)
		// Looking east is a yaw of -90 in Minecraft.
		check("@e[type=cow] facing 10.5 64 0.5", -90, 0)
		tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{3, 64, 0}}))
		if out := runTestCommand(rotate, "@e[type=cow] 0 0", tx); out.ErrorCount() == 0 {
			t.Fatal("rotating two cows at once succeeded")
		}
	})
}
