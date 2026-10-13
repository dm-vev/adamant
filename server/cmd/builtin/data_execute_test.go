package builtin

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

type executeTestSource struct{ position mgl64.Vec3 }

func (s executeTestSource) Position() mgl64.Vec3        { return s.position }
func (executeTestSource) SendCommandOutput(*cmd.Output) {}

type executeContextProbe struct{}

var executeContextProbeResult cmd.ExecutionContext

func (executeContextProbe) Run(src cmd.Source, _ *cmd.Output, _ *world.Tx) {
	executeContextProbeResult, _ = cmd.ExecutionContextOf(src)
}

func TestExecuteAsAtAndIfBlock(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	defer w.Close()
	const probeName = "_execute_context_probe_"
	cmd.Register(cmd.New(probeName, "test probe", nil, executeContextProbe{}))
	<-w.Exec(func(tx *world.Tx) {
		pos := mgl64.Vec3{2.5, 65, -3.5}
		target := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos}))
		source := executeTestSource{position: mgl64.Vec3{0, 64, 0}}
		out := &cmd.Output{}
		cmd.New("execute", "execute", nil, executeCommand{}).Execute("as @e at @s run "+probeName, source, tx)
		if executeContextProbeResult.Executor == nil || executeContextProbeResult.Executor.H() != target.H() || executeContextProbeResult.Position != pos {
			t.Fatalf("execute as/at context = executor %v at %v; want entity at %v", executeContextProbeResult.Executor, executeContextProbeResult.Position, pos)
		}
		stone, _ := world.BlockByName("minecraft:stone", nil)
		tx.SetBlock(cube.Pos{0, 64, 0}, stone, nil)
		executeContextProbeResult = cmd.ExecutionContext{}
		cmd.New("execute", "execute", nil, executeCommand{}).Execute("if block 0 64 0 minecraft:stone run "+probeName, source, tx)
		if out.ErrorCount() != 0 {
			t.Fatalf("execute if block errors: %v", out.Errors())
		}
		if executeContextProbeResult.Position != source.position {
			t.Fatalf("if block run position=%v, want %v", executeContextProbeResult.Position, source.position)
		}
		executeContextProbeResult = cmd.ExecutionContext{Position: mgl64.Vec3{9, 9, 9}}
		cmd.New("execute", "execute", nil, executeCommand{}).Execute("if block 0 64 0 minecraft:dirt run "+probeName, source, tx)
		if executeContextProbeResult.Position != (mgl64.Vec3{9, 9, 9}) {
			t.Fatal("execute if block ran despite a mismatched block")
		}
	})
}
