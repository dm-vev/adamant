package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func TestTemplateShapeGrowthUsesBoundaryOrder(t *testing.T) {
	// Executed Java 26.3 StructureTemplate fixture, legacy region stream seed 0.
	// Palette order is x-then-z, but upward boundary faces are visited z-then-x.
	positions := []cube.Pos{{8, 64, 9}, {9, 64, 8}}
	region := templateShapeFixture{
		states: map[cube.Pos]uint32{
			positions[0]: javaDefault("kelp_plant"),
			positions[1]: javaDefault("kelp_plant"),
		},
		random: java26.NewLegacyRandom(0),
	}
	UpdateJavaTemplateShapes(&region, positions, 2)
	for i, age := range []int32{23, 10} {
		state := region.states[positions[i]]
		if javaName(state) != "kelp" || javaInt(state, "age") != age {
			t.Fatalf("kelp at %v = state %d, want head age %d", positions[i], state, age)
		}
		// Unsupported body and replacement head retain distinct block tick
		// identities; neither may hide the fluid tick at the same position.
		for _, name := range []string{"minecraft:kelp_plant", "minecraft:kelp"} {
			if !region.ticks.HasScheduledTick(positions[i], chunk.BlockTick, name) {
				t.Errorf("missing %s block tick at %v", name, positions[i])
			}
		}
		if !region.ticks.HasScheduledTick(positions[i], chunk.FluidTick, "minecraft:water") {
			t.Errorf("missing water tick at %v", positions[i])
		}
	}
	if len(region.ticks.ScheduledTicks) != 6 {
		t.Errorf("generated ticks = %v, want exactly the two bodies, two heads and two fluids", region.ticks.ScheduledTicks)
	}
	if next := region.random.NextLong(); next != 4437113781045784766 {
		t.Errorf("following region random value = %d, want 4437113781045784766", next)
	}
}

type templateShapeFixture struct {
	states map[cube.Pos]uint32
	random java26.Random
	ticks  GenerationResult
}

func (r *templateShapeFixture) JavaBlockStateID(pos cube.Pos) (uint32, error) {
	if state, ok := r.states[pos]; ok {
		return state, nil
	}
	return javaDefault("air"), nil
}
func (*templateShapeFixture) Range() cube.Range            { return cube.Range{0, 255} }
func (r *templateShapeFixture) JavaRandom() *java26.Random { return &r.random }
func (*templateShapeFixture) Light(cube.Pos) uint8         { panic("unexpected light query") }
func (*templateShapeFixture) JavaFluidTickDelay(name string) int32 {
	if name != "minecraft:water" {
		panic("unexpected fluid " + name)
	}
	return 5
}
func (r *templateShapeFixture) ScheduleJavaTick(pos cube.Pos, kind chunk.ScheduledTickKind, name string, _ int32) {
	r.ticks.ScheduleTick(chunk.ScheduledTick{Pos: pos, Kind: kind, Type: name})
}
func (r *templateShapeFixture) HasJavaBlockTick(pos cube.Pos, name string) bool {
	return r.ticks.HasScheduledTick(pos, chunk.BlockTick, name)
}
func (*templateShapeFixture) JavaSignal(cube.Pos, cube.Face) int { panic("unexpected signal query") }
func (*templateShapeFixture) JavaHiveHasOccupants(cube.Pos) bool { panic("unexpected hive query") }
func (r *templateShapeFixture) SetJavaBlockState(pos cube.Pos, state uint32, _ int) bool {
	r.states[pos] = state
	return true
}
