package overworld

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
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
	}{
		{name: "dry", fluid: dry, props: map[string]any{"type": "bottom", "waterlogged": false}, wantType: "bottom"},
		{name: "source water", fluid: water, props: map[string]any{"type": "bottom", "waterlogged": false}, wet: true, wantType: "bottom"},
		{name: "flowing water", fluid: flowing, props: map[string]any{"type": "top", "waterlogged": false}, wantType: "top"},
		{name: "double slab rejects", fluid: water, props: map[string]any{"type": "double", "waterlogged": false}, wantType: "double"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			region := templateFluidTestRegion()
			pos := featurePos{8, 80, 8}
			region.Set(pos, tc.fluid)
			entry := javastructure.Block{Pos: [3]int32{pos.x, pos.y, pos.z}, State: world.BlockState{Name: "minecraft:oak_slab", Properties: tc.props}}
			applyTemplateFluids(region, []javastructure.Block{entry})
			assertTemplateSlab(t, region, pos, tc.wantType, tc.wet, waterLayer)
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
	makeSlab := func(pos featurePos, state world.BlockState) javastructure.Block {
		return javastructure.Block{Pos: [3]int32{pos.x, pos.y, pos.z}, State: state}
	}
	t.Run("ambient source fills dry slab", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		region.Set(source, water)
		applyTemplateFluids(region, []javastructure.Block{makeSlab(pos, slabState)})
		assertTemplateSlab(t, region, pos, "bottom", true, waterLayer)
	})
	t.Run("template source is locked", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		applyTemplateFluids(region, []javastructure.Block{makeSlab(pos, slabState), makeSlab(source, world.BlockState{Name: "minecraft:water"})})
		assertTemplateSlab(t, region, pos, "bottom", false, waterLayer)
	})
	t.Run("iterative fill propagates on the next pass", func(t *testing.T) {
		region := templateFluidTestRegion()
		first, second, ambient := featurePos{8, 80, 8}, featurePos{9, 80, 8}, featurePos{10, 80, 8}
		region.Set(ambient, water)
		applyTemplateFluids(region, []javastructure.Block{makeSlab(first, slabState), makeSlab(second, slabState)})
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
		applyTemplateFluids(region, []javastructure.Block{makeSlab(pos, slabState), makeSlab(pos, world.BlockState{Name: "minecraft:stone"})})
		if got := region.nativeState(pos); got != stone {
			t.Fatalf("overwritten position=%+v, want stone %+v", got, stone)
		}
	})
	t.Run("omitted waterlogged property uses default container state", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		region.Set(source, water)
		applyTemplateFluids(region, []javastructure.Block{makeSlab(pos, world.BlockState{Name: "minecraft:oak_slab"})})
		assertTemplateSlab(t, region, pos, "bottom", true, waterLayer)
	})
	t.Run("default waterlogged template source is locked", func(t *testing.T) {
		region := templateFluidTestRegion()
		pos, source := featurePos{8, 80, 8}, featurePos{9, 80, 8}
		applyTemplateFluids(region, []javastructure.Block{makeSlab(pos, slabState), makeSlab(source, world.BlockState{Name: "minecraft:sea_pickle"})})
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
		applyTemplateFluids(region, []javastructure.Block{makeSlab(pos, slabState)})
		assertTemplateSlab(t, region, pos, "bottom", false, waterLayer)
	})
}

func templateFluidTestRegion() *featureRegion {
	c := chunk.New(world.DefaultBlockRegistry, cube.Range{0, 255})
	region := newFeatureRegion(&ModernFeatures{}, c, world.ChunkPos{}, &world.GenerationResult{})
	region.SetOrigin(0, 0)
	return region
}

func assertTemplateSlab(t *testing.T, region *featureRegion, pos featurePos, wantType string, wantWet bool, waterLayer uint32) {
	t.Helper()
	got := region.nativeState(pos)
	java, err := world.JavaState(region.c.BlockRegistry(), got)
	if err != nil {
		t.Fatal(err)
	}
	if java.Properties["type"] != wantType {
		t.Fatalf("slab type=%v, want %s", java.Properties["type"], wantType)
	}
	if gotWet := java.Properties["waterlogged"] == true; gotWet != wantWet {
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
		entry := javastructure.Block{Pos: [3]int32{8, 80, 8}, State: world.BlockState{Name: "minecraft:campfire", Properties: map[string]any{"lit": true}}, NativeBlock: campfire}
		payload := applyTemplateFluids(region, []javastructure.Block{entry})
		got, err := world.JavaState(world.DefaultBlockRegistry, region.nativeState(pos))
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
	campfire := javastructure.Block{Pos: [3]int32{8, 80, 8}, State: world.BlockState{Name: "minecraft:campfire"}, NativeBlock: block.Campfire{Type: block.NormalFire()}}
	stone := javastructure.Block{Pos: campfire.Pos, State: world.BlockState{Name: "minecraft:stone"}}
	if payload := applyTemplateFluids(region, []javastructure.Block{campfire, stone}); len(payload) != 0 {
		t.Fatalf("overwritten campfire survived in actor payload: %+v", payload)
	}
}
