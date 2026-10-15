package builtin

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"iter"
	"testing"
)

type executeDimensionAdapter struct {
	serverAdapter
	world, nether, end *world.World
}

func (a executeDimensionAdapter) Players(*world.Tx) iter.Seq[*player.Player] {
	return func(yield func(*player.Player) bool) {}
}
func (a executeDimensionAdapter) World() *world.World  { return a.world }
func (a executeDimensionAdapter) Nether() *world.World { return a.nether }
func (a executeDimensionAdapter) End() *world.World    { return a.end }
func (executeDimensionAdapter) IsOperator(string) bool { return true }

func TestExecuteInTeleportMovesPlayerAcrossDimensions(t *testing.T) {
	overworld := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	nether := world.Config{Dim: world.Nether, Synchronous: true, RandomTickSpeed: -1}.New()
	end := world.Config{Dim: world.End, Synchronous: true, RandomTickSpeed: -1}.New()
	defer overworld.Close()
	defer nether.Close()
	defer end.Close()
	adapter := executeDimensionAdapter{world: overworld, nether: nether, end: end}
	previousServer := commandServer
	previousTP, hadTP := cmd.ByAlias("tp")
	previousEffect, hadEffect := cmd.ByAlias("effect")
	previousGive, hadGive := cmd.ByAlias("give")
	defer func() {
		commandServer = previousServer
		if hadTP {
			cmd.Register(previousTP)
		}
		if hadEffect {
			cmd.Register(previousEffect)
		}
		if hadGive {
			cmd.Register(previousGive)
		}
	}()
	commandServer = adapter
	cmd.Register(cmd.New("effect", "", nil, effectCommand{}, clearEffectCommand{}))
	cmd.Register(newGiveCommand())
	cmd.Register(cmd.New("tp", "", nil, teleportPositionCommand{srv: adapter}, teleportSelfPositionCommand{srv: adapter}))
	execute := cmd.New("execute", "", nil, executeCommand{})
	for _, test := range []struct {
		name string
		from *world.World
		into string
		to   *world.World
	}{
		{name: "overworld to nether", from: overworld, into: "minecraft:the_nether", to: nether},
		{name: "overworld to end", from: overworld, into: "minecraft:the_end", to: end},
		{name: "nether to overworld", from: nether, into: "minecraft:overworld", to: overworld},
	} {
		t.Run(test.name, func(t *testing.T) {
			pos := mgl64.Vec3{300000, 70, 300000}
			var handle *world.EntityHandle
			<-test.from.Exec(func(tx *world.Tx) {
				p := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}.New(player.Type, player.Config{Name: "Executor", Position: mgl64.Vec3{0, 64, 0}, GameMode: world.GameModeCreative})).(*player.Player)
				handle = p.H()
				src := cmd.WithContext(executeTestSource{position: p.Position()}, cmd.ExecutionContext{Executor: p, Position: p.Position(), Rotation: p.Rotation(), World: tx.World(), Anchor: cmd.Feet})
				execute.Execute("in "+test.into+" run effect give @s speed 10", src, tx)
				execute.Execute("in "+test.into+" run give @s minecraft:stone 1", src, tx)
				if _, ok := p.Effect(effect.Speed); !ok {
					t.Fatal("execute in effect did not apply speed to executor")
				}
				stack, _ := p.Inventory().Item(0)
				if stack.Count() != 1 {
					t.Fatalf("execute in give inventory count = %d, want 1", stack.Count())
				}
				execute.Execute("in "+test.into+" run tp @s 300000 70 300000", src, tx)
			})
			if got := handle.World(); got != test.to {
				t.Fatalf("player world = %v, want %v", got, test.to)
			}
			<-test.to.Exec(func(tx *world.Tx) {
				p, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("player missing from destination world")
				}
				if p.Position() != pos {
					t.Fatalf("player position = %v, want %v", p.Position(), pos)
				}
			})
		})
	}
}
