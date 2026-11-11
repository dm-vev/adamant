package player

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/client/clienttest"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// tickBenchmarkGround is the height the players of BenchmarkPlayerTick stand at: the top of the four layers of
// the flat world, which start at the bottom of the Overworld.
var tickBenchmarkGround = float64(world.Overworld.Range().Min() + 4)

// tickBenchmarkWorld returns a synchronous flat world of grass on dirt and bedrock, with the chunks within five of
// the origin loaded.
func tickBenchmarkWorld(b testing.TB) *world.World {
	b.Helper()
	world.DefaultBlockRegistry.Finalize()
	w := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Log:             slog.New(slog.DiscardHandler),
		Entities:        entity.DefaultRegistry,
		Generator:       generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}}),
	}.New()
	loader := world.NewLoader(5, w, clienttest.New())
	b.Cleanup(func() {
		<-w.Exec(func(tx *world.Tx) { loader.Close(tx) })
		_ = w.Close()
	})
	<-w.Exec(func(tx *world.Tx) {
		loader.Move(tx, mgl64.Vec3{0.5, tickBenchmarkGround, 0.5})
		loader.Load(tx, 121)
		if _, ok := tx.Block(cube.Pos{0, int(tickBenchmarkGround) - 1, 0}).(block.Grass); !ok {
			b.Fatalf("the flat world has %v under the players, want grass", tx.Block(cube.Pos{0, int(tickBenchmarkGround) - 1, 0}))
		}
	})
	return w
}

// BenchmarkPlayerTick measures a tick of 1 and of 50 survival players, each with a clienttest client, standing in
// a flat world: one op is one tick of every player.
//
//   - idle: the players stand still.
//   - walking: before each tick, each player moves 0.2 blocks along x, through Player.ClientMove, and turns back
//     every 200 ticks.
//   - using: the players hold a bow drawn, with arrows in the inventory, for the whole run.
//
// Timings need a quiet machine; allocations do not.
func BenchmarkPlayerTick(b *testing.B) {
	w := tickBenchmarkWorld(b)
	for _, kind := range []string{"idle", "walking", "using"} {
		for _, n := range []int{1, 50} {
			b.Run(fmt.Sprintf("%s/players=%d", kind, n), func(b *testing.B) {
				benchmarkPlayerTick(b, w, kind, n)
			})
		}
	}
}

// benchmarkPlayerTick runs one case of BenchmarkPlayerTick with n players.
func benchmarkPlayerTick(b *testing.B, w *world.World, kind string, n int) {
	<-w.Exec(func(tx *world.Tx) {
		players := make([]*Player, n)
		for i := range players {
			// Ten to a row, three blocks apart, within x -15 to 15 and z -6 to 6.
			start := mgl64.Vec3{float64(i%10*3-15) + 0.5, tickBenchmarkGround, float64(i/10*3-6) + 0.5}
			h := world.EntitySpawnOpts{Position: start, ID: uuid.New()}.New(Type, Config{
				Name: fmt.Sprintf("bench%d", i), Position: start, GameMode: world.GameModeSurvival, Client: clienttest.New(),
			})
			players[i] = tx.AddEntity(h).(*Player)
		}
		defer func() {
			for _, p := range players {
				_ = p.Close()
			}
		}()
		if kind == "using" {
			for _, p := range players {
				p.SetHeldItems(item.NewStack(item.Bow{}, 1), item.Stack{})
				if _, err := p.Inventory().AddItem(item.NewStack(item.Arrow{}, 64)); err != nil {
					b.Fatal(err)
				}
			}
		}
		step := func(tick int64) {
			for _, p := range players {
				switch kind {
				case "walking":
					x := 0.2
					if tick%400 >= 200 {
						x = -0.2
					}
					p.ClientMove(client.Move{Position: p.Position().Add(mgl64.Vec3{x, 0, 0})})
				case "using":
					if !p.UsingItem() {
						p.UseItem()
					}
				}
				p.Tick(tx, tick)
			}
		}
		// Past the grace of the join.
		for tick := range int64(62) {
			for _, p := range players {
				p.ClientMove(client.Move{Position: p.Position()})
				p.Tick(tx, tick)
			}
		}
		if kind == "using" {
			for _, p := range players {
				p.UseItem()
			}
			if !players[0].UsingItem() {
				b.Fatal("the player does not draw the bow")
			}
		}
		b.ReportAllocs()
		b.ResetTimer()
		for i := range b.N {
			step(int64(62 + i))
		}
		b.StopTimer()
		if kind == "using" && !players[0].UsingItem() {
			b.Fatal("the player stopped drawing the bow")
		}
		if !players[0].OnGround() {
			b.Fatalf("the player is not on the ground at %v", players[0].Position())
		}
	})
}
