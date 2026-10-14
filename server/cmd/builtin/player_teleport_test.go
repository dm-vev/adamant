package builtin

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func teleportTestCommand() cmd.Command {
	return cmd.New("tp", "", nil, teleportCommand{}, teleportSelfCommand{}, teleportPositionCommand{}, teleportSelfPositionCommand{}, teleportRotationCommand{}, teleportFacingPositionCommand{}, teleportFacingEntityCommand{})
}

func TestTeleportCommandsAndSpawnpoint(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(player.Type, player.Config{Name: "Mover", Position: mgl64.Vec3{0, 64, 0}, GameMode: world.GameModeCreative})).(*player.Player)
		target := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{10, 64, 0}}.New(player.Type, player.Config{Name: "Anchor", Position: mgl64.Vec3{10, 64, 0}, Rotation: cube.Rotation{45, 10}, GameMode: world.GameModeCreative})).(*player.Player)
		if out := runTestCommand(teleportTestCommand(), "Mover Anchor", tx); out.ErrorCount() != 0 {
			t.Fatalf("tp to entity errors: %v", out.Errors())
		}
		if p.Position() != target.Position() {
			t.Fatalf("tp position=%v, want %v", p.Position(), target.Position())
		}
		if out := runTestCommand(teleportTestCommand(), "Mover 2 65 2 90 15", tx); out.ErrorCount() != 0 {
			t.Fatalf("tp rotation errors: %v", out.Errors())
		}
		if p.Position() != (mgl64.Vec3{2, 65, 2}) || math.Abs(p.Rotation().Yaw()-90) > 1e-6 || math.Abs(p.Rotation().Pitch()-15) > 1e-6 {
			t.Fatalf("tp rotation position=%v rotation=%v", p.Position(), p.Rotation())
		}
		spawnpoint := cmd.New("spawnpoint", "", nil, spawnpointCommand{})
		if out := runTestCommand(spawnpoint, "Mover 5 70 -3", tx); out.ErrorCount() != 0 {
			t.Fatalf("spawnpoint errors: %v", out.Errors())
		}
		if data := p.Data(); !data.HasRespawnPoint || data.RespawnPosition != (cube.Pos{5, 70, -3}) || data.RespawnDimension != world.Overworld {
			t.Fatalf("respawn=%v/%v/%v, want {5 70 -3} overworld", data.RespawnPosition, data.RespawnDimension, data.HasRespawnPoint)
		}
	})
}

func TestRideCommandMountsAndDismounts(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(player.Type, player.Config{Name: "Rider", Position: mgl64.Vec3{0, 64, 0}, GameMode: world.GameModeCreative})).(*player.Player)
		boat := tx.AddEntity(entity.NewBoat(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 0}}, 0)).(*entity.Boat)
		ride := cmd.New("ride", "", nil, rideCommand{}, dismountCommand{})
		if out := runTestCommand(ride, "Rider mount @e[type=minecraft:oak_boat,limit=1,sort=nearest]", tx); out.ErrorCount() != 0 {
			t.Fatalf("mount errors: %v", out.Errors())
		}
		if p.Riding() != boat.H() {
			t.Fatal("player did not mount the boat")
		}
		if out := runTestCommand(ride, "Rider dismount", tx); out.ErrorCount() != 0 {
			t.Fatalf("dismount errors: %v", out.Errors())
		}
		if p.Riding() != nil {
			t.Fatal("player remained mounted")
		}
		// Horse species embed *Mount in a field that hides the Mount method.
		horse := tx.AddEntity(entity.NewHorse(world.EntitySpawnOpts{Position: mgl64.Vec3{-1, 64, 0}}))
		if out := runTestCommand(ride, "Rider mount @e[type=minecraft:horse,limit=1]", tx); out.ErrorCount() != 0 {
			t.Fatalf("horse mount errors: %v", out.Errors())
		}
		if p.Riding() != horse.H() {
			t.Fatal("player did not mount the horse")
		}
		if out := runTestCommand(ride, "Rider dismount", tx); out.ErrorCount() != 0 {
			t.Fatalf("horse dismount errors: %v", out.Errors())
		}
		// Like Java's forced startRiding, /ride needs no saddle.
		pig := tx.AddEntity(entity.NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 2}}))
		if out := runTestCommand(ride, "Rider mount @e[type=minecraft:pig,limit=1]", tx); out.ErrorCount() != 0 {
			t.Fatalf("pig mount errors: %v", out.Errors())
		}
		if p.Riding() != pig.H() {
			t.Fatal("player did not mount the unsaddled pig")
		}
	})
}

func TestSpreadPlayersWithinRangeOnSafeGround(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		for x := -4; x <= 4; x++ {
			for z := -4; z <= 4; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		tx.SetBlock(cube.Pos{0, 63, 0}, block.Lava{Depth: 8, Still: true}, nil)
		players := make([]*player.Player, 4)
		targets := make([]cmd.Target, len(players))
		for i := range players {
			players[i] = tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(player.Type, player.Config{Position: mgl64.Vec3{0, 64, 0}, GameMode: world.GameModeCreative})).(*player.Player)
			targets[i] = players[i]
		}
		out := &cmd.Output{}
		spreadPlayers(tx, out, mgl64.Vec3{0, 64, 0}, 1, 3, 70, targets)
		if out.ErrorCount() != 0 {
			t.Fatalf("spreadplayers errors: %v", out.Errors())
		}
		for _, p := range players {
			pos := p.Position()
			if math.Abs(pos[0]) > 3.5 || math.Abs(pos[2]) > 3.5 {
				t.Errorf("player outside range: %v", pos)
			}
			if pos[1] != 64 {
				t.Errorf("player not on top of ground: %v", pos)
			}
			if cube.PosFromVec3(pos).Side(cube.FaceDown) == (cube.Pos{0, 63, 0}) {
				t.Errorf("player placed on lava at %v", pos)
			}
		}
	})
}
