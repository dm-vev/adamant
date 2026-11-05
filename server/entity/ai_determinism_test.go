package entity_test

import (
	"fmt"
	"hash/fnv"
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// determinismScenario runs a pen with a wolf, a cat, rabbits, a witch, a
// villager pair and a player in a fresh synchronous world of the seed passed
// for ticks ticks, and returns a digest of every entity at the end of each
// tick: its position, rotation, health and effects.
func determinismScenario(t *testing.T, seed int64, ticks int) [][]string {
	t.Helper()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: entity.DefaultRegistry, Seed: seed}.New()
	defer func() { _ = w.Close() }()
	w.SetTime(18000)
	w.StopTime()
	w.SetDifficulty(world.DifficultyNormal)

	var handles []*world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
			}
		}
		pen(tx, 10)
		add := func(h *world.EntityHandle) { handles = append(handles, tx.AddEntity(h).H()) }
		handles = append(handles, addSurvivalPlayer(tx, mgl64.Vec3{5.5, 64, 5.5}).H())
		add(entity.NewWolf(world.EntitySpawnOpts{Position: mgl64.Vec3{-4.5, 64, 0.5}}, entity.WolfBehaviourConfig{}))
		add(entity.NewCat(world.EntitySpawnOpts{Position: mgl64.Vec3{-3.5, 64, 3.5}}))
		for i := range 3 {
			add(entity.NewRabbit(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i) - 1.5, 64, -3.5}}))
		}
		add(entity.NewWitch(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, 64, -5.5}}))
		for i := range 2 {
			add(entity.NewVillager(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i)*2 - 6.5, 64, 6.5}}))
		}
	})

	lines := make([][]string, 0, ticks)
	for range ticks {
		stepWorld(t, w, 1)
		var tick []string
		<-w.Exec(func(tx *world.Tx) {
			for i, handle := range handles {
				e, ok := handle.Entity(tx)
				if !ok {
					tick = append(tick, fmt.Sprintf("%d gone", i))
					continue
				}
				pos, rot := e.Position(), e.Rotation()
				line := fmt.Sprintf("%d %x %x %x %x %x", i, math.Float64bits(pos[0]), math.Float64bits(pos[1]), math.Float64bits(pos[2]), math.Float64bits(rot.Yaw()), math.Float64bits(rot.Pitch()))
				if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
					line += fmt.Sprintf(" vel %v", v.Velocity())
				}
				if l, ok := e.(entity.Living); ok {
					line += fmt.Sprintf(" hp %x", math.Float64bits(l.Health()))
				}
				if f, ok := e.(interface{ Effects() []effect.Effect }); ok {
					for _, eff := range f.Effects() {
						id, _ := effect.ID(eff.Type())
						line += fmt.Sprintf(" effect %d level %d for %v", id, eff.Level(), eff.Duration())
					}
				}
				tick = append(tick, line)
			}
			n := 0
			for range tx.Entities() {
				n++
			}
			tick = append(tick, fmt.Sprintf("entities %d", n))
		})
		lines = append(lines, tick)
	}
	return lines
}

// digestOf returns the digest of the lines of one tick.
func digestOf(lines []string) uint64 {
	h := fnv.New64a()
	for _, l := range lines {
		_, _ = h.Write([]byte(l + "\n"))
	}
	return h.Sum64()
}

// TestMobAIIsDeterministic checks that the same world seed gives the same AI:
// two fresh synchronous worlds of one seed digest equally in every tick, and a
// world of another seed does not. The randomness of mobs and of the effects
// around them comes from the world, so nothing but the seed may change a run.
func TestMobAIIsDeterministic(t *testing.T) {
	t.Parallel()
	const ticks = 300
	first := determinismScenario(t, 7, ticks)
	again := determinismScenario(t, 7, ticks)
	for i := range first {
		if digestOf(first[i]) != digestOf(again[i]) {
			for j := range first[i] {
				if j >= len(again[i]) || first[i][j] != again[i][j] {
					t.Logf("tick %d: %q != %q", i+1, first[i][j], again[i])
					break
				}
			}
			t.Fatalf("two worlds of one seed differ in tick %d", i+1)
		}
	}
	other := determinismScenario(t, 8, ticks)
	same := true
	for i := range first {
		same = same && digestOf(first[i]) == digestOf(other[i])
	}
	if same {
		t.Fatal("a world of another seed gave the same digest in every tick")
	}
}
