package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// forkCountProbe counts how many execute branches reached run.
type forkCountProbe struct{}

var forkCountProbeRuns int

func (forkCountProbe) Run(cmd.Source, *cmd.Output, *world.Tx) { forkCountProbeRuns++ }

func TestExecuteStopsAtMaxCommandForks(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry}.New()
	defer w.Close()
	const probe = "_execute_fork_probe_"
	cmd.Register(cmd.New(probe, "test probe", nil, forkCountProbe{}))
	execute := cmd.New("execute", "", nil, executeCommand{})
	<-w.Exec(func(tx *world.Tx) {
		for i := range 3 {
			tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i), 64, 0}}))
		}
		forkCountProbeRuns = 0
		if out := runTestCommand(execute, "as @e[type=cow] run "+probe, tx); out.ErrorCount() != 0 || forkCountProbeRuns != 3 {
			t.Fatalf("default limit: runs=%d errors=%v", forkCountProbeRuns, out.Errors())
		}
		if err := tx.World().SetGameRule("max_command_forks", 3); err != nil {
			t.Fatal(err)
		}
		forkCountProbeRuns = 0
		out := runTestCommand(execute, "as @e[type=cow] run "+probe, tx)
		if out.ErrorCount() == 0 || forkCountProbeRuns != 0 {
			t.Fatalf("max_command_forks 3 with 3 forks: runs=%d errors=%v", forkCountProbeRuns, out.Errors())
		}
	})
}
