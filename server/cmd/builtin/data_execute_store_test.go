package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/scoreboard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestExecuteAsKeepsPositionAndStoresResults(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	const probe = "_execute_store_probe_"
	cmd.Register(cmd.New(probe, "test probe", nil, executeContextProbe{}))
	cmd.Register(cmd.New("scoreboard", "", nil, scoreboardObjectiveAdd{}, scoreboardScoreSet{}, scoreboardScoreGet{}, scoreboardScoreReset{}, scoreboardScoreList{}))
	cmd.Register(cmd.New("time", "", nil, timeQueryCommand{}))
	cmd.Register(cmd.New("setblock", "", nil, setBlockCommand{}))
	execute := cmd.New("execute", "", nil, executeCommand{})
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{5.5, 64, 5.5}}))
		tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: mgl64.Vec3{40.5, 64, 0.5}}))
		src := consoleTestSource{pos: mgl64.Vec3{0, 64, 0}, out: &cmd.Output{}}

		// `as` changes only the executor; `at` moves the position.
		execute.Execute("as @e[type=cow] run "+probe, src, tx)
		if executeContextProbeResult.Executor == nil || executeContextProbeResult.Executor.H() != cow.H() || executeContextProbeResult.Position != src.pos {
			t.Fatalf("execute as context = %v at %v", executeContextProbeResult.Executor, executeContextProbeResult.Position)
		}
		execute.Execute("as @e[type=cow] at @s positioned ~ ~2 ~ run "+probe, src, tx)
		if executeContextProbeResult.Position != (mgl64.Vec3{5.5, 66, 5.5}) {
			t.Fatalf("execute at/positioned position = %v", executeContextProbeResult.Position)
		}
		// The distance filter excludes the zombie 40 blocks away; the probe
		// records the last executor, so a match on it would replace the cow.
		executeContextProbeResult = cmd.ExecutionContext{}
		execute.Execute("as @e[distance=..20] run "+probe, src, tx)
		if executeContextProbeResult.Executor == nil || executeContextProbeResult.Executor.H() != cow.H() {
			t.Fatal("distance-limited selector did not select only the nearby cow")
		}

		m := tx.World().Scoreboard()
		if err := m.AddObjective("obj", "obj", scoreboard.Dummy); err != nil {
			t.Fatal(err)
		}
		w.SetTime(1234)
		execute.Execute("store result score Fake obj run time query daytime", src, tx)
		if v, ok := m.Score("obj", "Fake"); !ok || v != 1234 {
			t.Fatalf("stored time = %d/%v, want 1234", v, ok)
		}
		execute.Execute("store success score Fake obj run setblock 0 400 0 minecraft:stone", src, tx)
		if v, _ := m.Score("obj", "Fake"); v != 0 {
			t.Fatalf("store success of failing command = %d, want 0", v)
		}
		out := &cmd.Output{}
		execute.Execute("if entity @e[type=cow]", consoleTestSource{out: out}, tx)
		if out.ErrorCount() != 0 || out.Result() != 1 {
			t.Fatalf("trailing if entity: errors=%v result=%d", out.Errors(), out.Result())
		}
		out = &cmd.Output{}
		execute.Execute("unless entity @e[type=cow]", consoleTestSource{out: out}, tx)
		if out.ErrorCount() == 0 {
			t.Fatal("trailing unless entity passed although a cow exists")
		}
		tx.SetBlock(cube.Pos{1, 64, 1}, nil, nil)
		execute.Execute("if score Fake obj matches 0 run setblock 1 64 1 minecraft:stone", src, tx)
		if name, _ := tx.Block(cube.Pos{1, 64, 1}).EncodeBlock(); name != "minecraft:stone" {
			t.Fatal("if score matches did not run")
		}
	})
}

func TestScoreboardPlayersSelectorsAndReset(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	board := cmd.New("scoreboard", "", nil, scoreboardObjectiveAdd{}, scoreboardScoreSet{}, scoreboardScoreAdd{}, scoreboardScoreRemove{}, scoreboardScoreGet{}, scoreboardScoreReset{}, scoreboardScoreList{})
	<-w.Exec(func(tx *world.Tx) {
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 64, 1}}))
		if out := runTestCommand(board, "objectives add kills dummy", tx); out.ErrorCount() != 0 {
			t.Fatalf("objective add: %v", out.Errors())
		}
		runTestCommand(board, "players set @e[type=cow] kills 5", tx)
		runTestCommand(board, "players remove @e[type=cow] kills 2", tx)
		got := runTestCommand(board, "players get @e[type=cow] kills", tx)
		if got.Result() != 3 {
			t.Fatalf("cow score result = %d, errors %v", got.Result(), got.Errors())
		}
		if v, ok := tx.World().Scoreboard().Score("kills", cow.H().UUID().String()); !ok || v != 3 {
			t.Fatalf("entity holder name is not its UUID: %d/%v", v, ok)
		}
		runTestCommand(board, "players set Alex kills 1", tx)
		runTestCommand(board, "players reset * kills", tx)
		if holders := tx.World().Scoreboard().Holders(); len(holders) != 0 {
			t.Fatalf("reset * left holders %v", holders)
		}
	})
}
