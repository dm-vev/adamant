package world

import (
	"testing"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/internal/gamedata"
	"github.com/dm-vev/adamant/server/internal/vanilla"
	"github.com/dm-vev/adamant/server/world/chunk"
)

func TestTemplateShapeGrowthUsesBoundaryOrder(t *testing.T) {
	t.Parallel()
	// Executed Java 26.3 StructureTemplate fixture, legacy region stream seed 0.
	// Palette order is x-then-z, but upward boundary faces are visited z-then-x.
	positions := []cube.Pos{{8, 64, 9}, {9, 64, 8}}
	region := templateShapeFixture{
		states: map[cube.Pos]uint32{
			positions[0]: stateDefault("kelp_plant"),
			positions[1]: stateDefault("kelp_plant"),
		},
		random: vanilla.NewLegacyRandom(0),
	}
	UpdateTemplateShapes(&region, positions, 2)
	for i, age := range []int32{23, 10} {
		state := region.states[positions[i]]
		if stateName(state) != "kelp" || stateInt(state, "age") != age {
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
	states     map[cube.Pos]uint32
	random     vanilla.Random
	ticks      GenerationResult
	scheduled  []fixtureScheduledTick
	writes     []shapeWrite
	rejections map[cube.Pos]bool
}

type fixtureScheduledTick struct {
	pos   cube.Pos
	kind  chunk.ScheduledTickKind
	name  string
	delay int32
}

type shapeWrite struct {
	pos   cube.Pos
	state uint32
	flags int
}

func (r *templateShapeFixture) BlockStateID(pos cube.Pos) (uint32, error) {
	if state, ok := r.states[pos]; ok {
		return state, nil
	}
	return stateDefault("air"), nil
}
func (*templateShapeFixture) Range() cube.Range               { return cube.Range{0, 255} }
func (r *templateShapeFixture) RegionRandom() *vanilla.Random { return &r.random }
func (*templateShapeFixture) Light(cube.Pos) uint8            { panic("unexpected light query") }
func (*templateShapeFixture) FluidTickDelay(name string) int32 {
	if name != "minecraft:water" {
		panic("unexpected fluid " + name)
	}
	return 5
}
func (r *templateShapeFixture) ScheduleTick(pos cube.Pos, kind chunk.ScheduledTickKind, name string, delay int32) {
	r.scheduled = append(r.scheduled, fixtureScheduledTick{pos: pos, kind: kind, name: name, delay: delay})
	r.ticks.ScheduleTick(chunk.ScheduledTick{Pos: pos, Kind: kind, Type: name})
}
func (r *templateShapeFixture) HasBlockTick(pos cube.Pos, name string) bool {
	return r.ticks.HasScheduledTick(pos, chunk.BlockTick, name)
}
func (*templateShapeFixture) Signal(cube.Pos, cube.Face) int { panic("unexpected signal query") }
func (*templateShapeFixture) HiveHasOccupants(cube.Pos) bool { panic("unexpected hive query") }
func (r *templateShapeFixture) SetBlockState(pos cube.Pos, state uint32, flags int) bool {
	r.writes = append(r.writes, shapeWrite{pos: pos, state: state, flags: flags})
	if r.rejections[pos] {
		return false
	}
	r.states[pos] = state
	return true
}

func TestSimpleBlockPlacementDoublePlantOccupancyAndWaterlogging(t *testing.T) {
	t.Parallel()
	pos := cube.Pos{8, 64, 8}
	stone := stateDefault("clay")
	plant, err := gamedata.BlockStateID("minecraft:small_dripleaf", map[string]any{
		"half": "lower", "waterlogged": true, "facing": "north",
	})
	if err != nil {
		t.Fatal(err)
	}
	water := stateDefault("water")
	region := templateShapeFixture{
		states: map[cube.Pos]uint32{
			pos.Side(cube.FaceDown): stone,
			pos.Side(cube.FaceUp):   water,
		},
		random: vanilla.NewLegacyRandom(0),
	}
	if !PlaceGenerationSimpleBlock(&region, plant, pos, false) {
		t.Fatal("matching water fluid did not permit double-plant placement")
	}
	if got := region.states[pos]; stateName(got) != "small_dripleaf" || stateString(got, "half") != "lower" || stateBool(got, "waterlogged") {
		t.Fatalf("lower half = %s, want dry lower small dripleaf", stateName(got))
	}
	upper := region.states[pos.Side(cube.FaceUp)]
	if stateName(upper) != "small_dripleaf" || stateString(upper, "half") != "upper" || !stateBool(upper, "waterlogged") {
		t.Fatalf("upper half = %s, want waterlogged upper small dripleaf", stateName(upper))
	}
	if len(region.writes) != 2 || region.writes[0].pos != pos || region.writes[1].pos != pos.Side(cube.FaceUp) || region.writes[0].flags != 2 || region.writes[1].flags != 2 {
		t.Fatalf("double plant writes = %+v, want lower then upper with flags 2", region.writes)
	}

	region = templateShapeFixture{states: map[cube.Pos]uint32{
		pos.Side(cube.FaceDown): stone,
		pos:                     stateDefault("air"),
		pos.Side(cube.FaceUp):   stateDefault("lava"),
	}}
	if PlaceGenerationSimpleBlock(&region, plant, pos, false) || len(region.writes) != 0 {
		t.Fatal("mismatched upper fluid was accepted or wrote blocks")
	}
}

func TestSimpleBlockPlacementRejectsBlockedDoublePlantTop(t *testing.T) {
	t.Parallel()
	pos := cube.Pos{8, 64, 8}
	region := templateShapeFixture{states: map[cube.Pos]uint32{
		pos.Side(cube.FaceDown): stateDefault("dirt"),
		pos.Side(cube.FaceUp):   stateDefault("stone"),
	}}
	if PlaceGenerationSimpleBlock(&region, stateDefault("sunflower"), pos, false) {
		t.Fatal("double plant placed through non-replaceable top block")
	}
	if len(region.writes) != 0 {
		t.Fatalf("blocked double plant wrote blocks: %+v", region.writes)
	}
}

func TestSimpleBlockMossCarpetDoesNotTopAnExistingBase(t *testing.T) {
	t.Parallel()
	pos := cube.Pos{8, 64, 8}
	region := templateShapeFixture{
		states: map[cube.Pos]uint32{
			pos.Side(cube.FaceDown): stateDefault("stone"),
			pos.Side(cube.FaceUp):   stateDefault("pale_moss_carpet"),
		},
		random: vanilla.NewLegacyRandom(0),
	}
	if !PlaceGenerationSimpleBlock(&region, stateDefault("pale_moss_carpet"), pos, false) {
		t.Fatal("supported moss carpet placement failed")
	}
	if len(region.writes) != 1 || region.writes[0].pos != pos {
		t.Fatalf("placement wrote %d blocks with an existing base above, want only origin", len(region.writes))
	}
	if got, want := region.random.NextLong(), int64(-4962768465676381896); got != want {
		t.Fatalf("following region random = %d, want unchanged stream %d", got, want)
	}
}

func TestSimpleBlockPlacementUsesRegionRandomAndSchedulesCurrentBlock(t *testing.T) {
	t.Parallel()
	pos := cube.Pos{8, 64, 8}
	region := templateShapeFixture{
		states: map[cube.Pos]uint32{
			pos.Side(cube.FaceDown):                   stateDefault("stone"),
			pos.Side(cube.FaceEast):                   stateDefault("stone"),
			pos.Side(cube.FaceEast).Side(cube.FaceUp): stateDefault("stone"),
		},
		random: vanilla.NewLegacyRandom(0),
	}
	if !PlaceGenerationSimpleBlock(&region, stateDefault("pale_moss_carpet"), pos, true) {
		t.Fatal("supported moss carpet placement failed")
	}
	if len(region.writes) < 2 || region.writes[0].pos != pos || region.writes[0].flags != 2 || region.writes[1].pos != pos.Side(cube.FaceUp) || region.writes[1].flags != 2 {
		t.Fatalf("moss carpet writes = %+v, want base then topper with flags 2", region.writes)
	}
	if stateName(region.states[pos]) != "pale_moss_carpet" || !stateBool(region.states[pos], "bottom") || stateString(region.states[pos], "east") != "tall" {
		t.Fatalf("recomputed moss carpet base = %s", stateName(region.states[pos]))
	}
	topper := region.states[pos.Side(cube.FaceUp)]
	if stateName(topper) != "pale_moss_carpet" || stateBool(topper, "bottom") || stateString(topper, "east") != "low" {
		t.Fatalf("moss carpet topper = %s", stateName(topper))
	}
	if len(region.scheduled) != 1 || region.scheduled[0] != (fixtureScheduledTick{
		pos: pos, kind: chunk.BlockTick, name: "minecraft:pale_moss_carpet", delay: 1,
	}) {
		t.Fatalf("scheduled ticks = %+v, want current origin moss carpet block tick after one tick", region.scheduled)
	}
	if got, want := region.random.NextLong(), int64(-3109364765729502342); got != want {
		t.Fatalf("following region random = %d, want %d after one horizontal topper side", got, want)
	}
}

func TestSimpleBlockPlacementReturnsTrueWhenSetIsRejected(t *testing.T) {
	t.Parallel()
	pos := cube.Pos{8, 64, 8}
	region := templateShapeFixture{
		states:     map[cube.Pos]uint32{},
		rejections: map[cube.Pos]bool{pos: true},
		random:     vanilla.NewLegacyRandom(0),
	}
	if !PlaceGenerationSimpleBlock(&region, stateDefault("stone"), pos, true) {
		t.Fatal("valid feature returned false after setBlock rejection")
	}
	current, _ := region.BlockStateID(pos)
	if current != stateDefault("air") {
		t.Fatalf("rejected block write changed origin to %s", stateName(current))
	}
	if len(region.scheduled) != 1 || region.scheduled[0] != (fixtureScheduledTick{
		pos: pos, kind: chunk.BlockTick, name: "minecraft:air", delay: 1,
	}) {
		t.Fatalf("scheduled ticks = %+v, want tick for current air block at delay 1", region.scheduled)
	}
	if len(region.writes) != 1 || region.writes[0].flags != 2 {
		t.Fatalf("writes = %+v, want one flags-2 attempt", region.writes)
	}
}
