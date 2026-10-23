package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
)

func TestTemplatePlacementWaterlogsNativeWaterSources(t *testing.T) {
	registry := world.DefaultBlockRegistry
	water, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:water"})
	if err != nil {
		t.Fatal(err)
	}
	dry, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:air"})
	if err != nil {
		t.Fatal(err)
	}
	waterLayer, ok := world.WaterSourceRuntimeID(registry)
	if !ok {
		t.Fatal("registry has no native source-water layer")
	}
	flowing, err := world.ResolveBlock(registry, block.Water{Depth: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		fluid    world.NativeState
		props    map[string]any
		wet      bool
		wantType string
		wantTick bool
	}{
		{name: "dry", fluid: dry, props: map[string]any{"type": "bottom", "waterlogged": false}, wantType: "bottom"},
		{name: "source water", fluid: water, props: map[string]any{"type": "bottom", "waterlogged": false}, wet: true, wantType: "bottom", wantTick: true},
		{name: "flowing water", fluid: flowing, props: map[string]any{"type": "top", "waterlogged": false}, wantType: "top"},
		{name: "double slab rejects", fluid: water, props: map[string]any{"type": "double", "waterlogged": false}, wantType: "double"},
		{name: "direct wet state", fluid: dry, props: map[string]any{"type": "bottom", "waterlogged": true}, wet: true, wantType: "bottom"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := templateFluidTestRegion()
			pos := featurePos{8, 80, 8}
			region.Set(pos, tc.fluid)
			entry := structure.Block{Pos: [3]int32{pos.x, pos.y, pos.z}, State: world.BlockState{Name: "minecraft:oak_slab", Properties: tc.props}}
			applyTemplateFluids(region, []structure.Block{entry})
			assertTemplateSlab(t, region, pos, tc.wantType, tc.wet, waterLayer)
			ticks := region.result.ScheduledTicks
			if tc.wantTick {
				want := chunk.ScheduledTick{Pos: cube.Pos{8, 80, 8}, Kind: chunk.FluidTick, Type: "minecraft:water"}
				if len(ticks) != 1 || ticks[0] != want {
					t.Fatalf("liquid placement ticks = %+v, want %+v", ticks, want)
				}
			} else if len(ticks) != 0 {
				t.Fatalf("direct/rejected placement created liquid ticks: %+v", ticks)
			}
		})
	}
}

func TestTemplateFluidSourcePropagationAndCurrentState(t *testing.T) {
	water, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:water"})
	if err != nil {
		t.Fatal(err)
	}
	waterLayer, ok := world.WaterSourceRuntimeID(world.DefaultBlockRegistry)
	if !ok {
		t.Fatal("registry has no native source-water layer")
	}
	slabState := world.BlockState{Name: "minecraft:oak_slab", Properties: map[string]any{"type": "bottom", "waterlogged": false}}
	makeSlab := func(pos featurePos, state world.BlockState) structure.Block {
		return structure.Block{Pos: [3]int32{pos.x, pos.y, pos.z}, State: state}
	}
	t.Run("ambient source fills dry slab", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		region.Set(source, water)
		applyTemplateFluids(region, []structure.Block{makeSlab(pos, slabState)})
		assertTemplateSlab(t, region, pos, "bottom", true, waterLayer)
	})
	t.Run("template source is locked", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		applyTemplateFluids(region, []structure.Block{makeSlab(pos, slabState), makeSlab(source, world.BlockState{Name: "minecraft:water"})})
		assertTemplateSlab(t, region, pos, "bottom", false, waterLayer)
	})
	t.Run("iterative fill propagates on the next pass", func(t *testing.T) {
		region := templateFluidTestRegion()
		first, second, ambient := featurePos{8, 80, 8}, featurePos{9, 80, 8}, featurePos{10, 80, 8}
		region.Set(ambient, water)
		applyTemplateFluids(region, []structure.Block{makeSlab(first, slabState), makeSlab(second, slabState)})
		assertTemplateSlab(t, region, first, "bottom", true, waterLayer)
		assertTemplateSlab(t, region, second, "bottom", true, waterLayer)
	})
	t.Run("later duplicate overwrites queued container", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		region.Set(source, water)
		stone, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:stone"})
		if err != nil {
			t.Fatal(err)
		}
		applyTemplateFluids(region, []structure.Block{makeSlab(pos, slabState), makeSlab(pos, world.BlockState{Name: "minecraft:stone"})})
		if got := region.nativeState(pos); got != stone {
			t.Fatalf("overwritten position=%+v, want stone %+v", got, stone)
		}
	})
	t.Run("omitted waterlogged property uses default container state", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		region.Set(source, water)
		applyTemplateFluids(region, []structure.Block{makeSlab(pos, world.BlockState{Name: "minecraft:oak_slab"})})
		assertTemplateSlab(t, region, pos, "bottom", true, waterLayer)
	})
	t.Run("default waterlogged template source is locked", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		applyTemplateFluids(region, []structure.Block{makeSlab(pos, slabState), makeSlab(source, world.BlockState{Name: "minecraft:sea_pickle"})})
		assertTemplateSlab(t, region, pos, "bottom", false, waterLayer)
	})
	t.Run("upper lava precedes eastern water", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos := featurePos{8, 80, 8}
		lava, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:lava"})
		if err != nil {
			t.Fatal(err)
		}
		region.Set(featurePos{8, 81, 8}, lava)
		region.Set(featurePos{9, 80, 8}, water)
		applyTemplateFluids(region, []structure.Block{makeSlab(pos, slabState)})
		assertTemplateSlab(t, region, pos, "bottom", false, waterLayer)
	})
}

func templateFluidTestRegion() *featureRegion {
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
	source := pillarAirSource{ModernFeatureSource: modernTerrainSource{}, air: world.DefaultBlockRegistry.AirRuntimeID()}
	region := newFeatureRegion(&ModernFeatures{source: source}, c, world.ChunkPos{}, &world.GenerationResult{})
	region.SetOrigin(0, 0)
	return region
}

func assertTemplateSlab(t *testing.T, region *featureRegion, pos featurePos, wantType string, wantWet bool, waterLayer uint32) {
	t.Helper()
	got := region.nativeState(pos)
	canonical, err := world.CanonicalState(region.c.BlockRegistry(), got)
	if err != nil {
		t.Fatal(err)
	}
	if canonical.Properties["type"] != wantType {
		t.Fatalf("slab type=%v, want %s", canonical.Properties["type"], wantType)
	}
	if gotWet := canonical.Properties["waterlogged"] == true; gotWet != wantWet {
		t.Fatalf("waterlogged=%v, want %v (native=%+v)", gotWet, wantWet, got)
	}
	if wantWet && got.Liquid != waterLayer {
		t.Fatalf("liquid layer=%d, want source water %d", got.Liquid, waterLayer)
	}
}

func TestTemplateFluidExtinguishesNativePayload(t *testing.T) {
	for _, ambient := range []bool{false, true} {
		region := templateFluidTestRegion()
		pos := featurePos{8, 80, 8}
		water, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:water"})
		if err != nil {
			t.Fatal(err)
		}
		source := pos
		if ambient {
			source.x++
		}
		region.Set(source, water)
		campfire := block.Campfire{Type: block.NormalFire(), Facing: cube.North}
		campfire.Items[0].Time = 123
		entry := structure.Block{Pos: [3]int32{8, 80, 8}, State: world.BlockState{Name: "minecraft:campfire", Properties: map[string]any{"lit": true}}, NativeBlock: campfire}
		payload := applyTemplateFluids(region, []structure.Block{entry})
		got, err := world.CanonicalState(world.DefaultBlockRegistry, region.nativeState(pos))
		if err != nil {
			t.Fatal(err)
		}
		if got.Properties["lit"] != false || got.Properties["waterlogged"] != true {
			t.Fatalf("ambient=%v campfire=%+v, want wet and extinguished", ambient, got)
		}
		if len(payload) != 1 {
			t.Fatalf("ambient=%v payload=%+v, want surviving campfire actor", ambient, payload)
		}
		actor, ok := payload[0].NativeBlock.(block.Campfire)
		if !ok || !actor.Extinguished || actor.Items[0].Time != 123 {
			t.Fatalf("ambient=%v actor=%+v, want extinguished with retained cooking data", ambient, payload[0].NativeBlock)
		}
	}
}

func TestTemplateFluidDropsOverwrittenActor(t *testing.T) {
	region := templateFluidTestRegion()
	campfire := structure.Block{Pos: [3]int32{8, 80, 8}, State: world.BlockState{Name: "minecraft:campfire"}, NativeBlock: block.Campfire{Type: block.NormalFire()}}
	stone := structure.Block{Pos: campfire.Pos, State: world.BlockState{Name: "minecraft:stone"}}
	if payload := applyTemplateFluids(region, []structure.Block{campfire, stone}); len(payload) != 0 {
		t.Fatalf("overwritten campfire survived in actor payload: %+v", payload)
	}
}

func TestTemplateRefillTicksFollowSuccessfulFillOrder(t *testing.T) {
	region := templateFluidTestRegion()
	water, err := world.ResolveState(world.DefaultBlockRegistry, world.BlockState{Name: "minecraft:water"})
	if err != nil {
		t.Fatal(err)
	}
	region.Set(featurePos{10, 80, 8}, water)
	state := world.BlockState{Name: "minecraft:oak_slab", Properties: map[string]any{"type": "bottom", "waterlogged": false}}
	applyTemplateFluids(region, []structure.Block{
		{Pos: [3]int32{8, 80, 8}, State: state},
		{Pos: [3]int32{9, 80, 8}, State: state},
	})
	ticks := region.result.ScheduledTicks
	want := []cube.Pos{{9, 80, 8}, {8, 80, 8}}
	if len(ticks) != len(want) {
		t.Fatalf("refill ticks = %+v, want positions %v", ticks, want)
	}
	for i, pos := range want {
		if ticks[i] != (chunk.ScheduledTick{Pos: pos, Kind: chunk.FluidTick, Type: "minecraft:water"}) {
			t.Fatalf("refill tick %d = %+v, want water at %v", i, ticks[i], pos)
		}
	}
}

func TestSpringReplayPublishesOnlyTargetOwnedTicks(t *testing.T) {
	registry := world.DefaultBlockRegistry
	stone, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:stone"})
	if err != nil {
		t.Fatal(err)
	}
	air, err := world.ResolveState(registry, world.BlockState{Name: "minecraft:air"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		target   world.ChunkPos
		wantTick bool
	}{
		{"own target", world.ChunkPos{}, true},
		{"neighbor replay", world.ChunkPos{1, 0}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := templateFluidTestRegion()
			region.target = tc.target
			pos := featurePos{8, 80, 8}
			for _, offset := range []featurePos{{0, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}} {
				region.Set(featurePos{pos.x + offset.x, pos.y + offset.y, pos.z + offset.z}, stone)
			}
			region.Set(featurePos{9, 80, 8}, air)
			config := map[string]any{
				"state":        map[string]any{"id": "minecraft:water"},
				"valid_blocks": []any{"minecraft:stone"}, "requires_block_below": true,
				"rock_count": float64(4), "hole_count": float64(1),
			}
			if !placeModernSpringRegion(region, config, pos, nil) {
				t.Fatal("valid spring was not placed")
			}
			name, _ := region.State(pos)
			if name != "minecraft:water" {
				t.Fatalf("spring left %s, want water", name)
			}
			ticks := region.result.ScheduledTicks
			if tc.wantTick {
				if len(ticks) != 1 || ticks[0] != (chunk.ScheduledTick{Pos: cube.Pos{8, 80, 8}, Kind: chunk.FluidTick, Type: "minecraft:water"}) {
					t.Fatalf("spring tick = %+v, want own water update", ticks)
				}
			} else {
				if len(ticks) != 0 {
					t.Fatalf("replay published foreign spring ticks: %+v", ticks)
				}
				if rid := region.c.Block(8, 80, 8, 0); rid != registry.AirRuntimeID() {
					t.Fatalf("foreign spring wrote target block %d", rid)
				}
			}
		})
	}
}
