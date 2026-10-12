package javastructure

import (
	"fmt"
	"log/slog"
	"maps"
	"reflect"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

// ProcessorEnvironment supplies world queries and placement context used by Java processors.
type ProcessorEnvironment struct {
	BaseBlock   func(x, y, z int32) world.Block
	JavaBiomeAt func(x, y, z int32) string
	BaseHeight  func(x, z int32, oceanFloor bool) int32
	SeaLevel    int32
	// MotionBlockingNoLeavesHeight queries the live post-decoration heightmap,
	// not the frozen *_WG terrain height used by BaseHeight.
	MotionBlockingNoLeavesHeight func(x, z int32) int32
	// JavaBlockStateID preserves source properties not represented by the native block model.
	JavaBlockStateID func(x, y, z int32) (uint32, error)
	// Template marker callbacks query the live actor after template placement.
	HasChestBlockEntity func(x, y, z int32) bool
	// SetBlockEntityLootTable changes actor NBT without writing the block again.
	SetBlockEntityLootTable func(x, y, z int32, table string, seed int64)
	// CanWriteBlock applies WorldGenRegion's write radius in addition to Range.
	CanWriteBlock func(x, y, z int32) bool
	Range         cube.Range
	// PieceState holds the source piece's persisted postProcess flags.
	// Its owner serializes accesses across chunk render sessions.
	PieceState *StructurePieceState
	// TransientStructureState marks an uncommitted neighbour-decoration preview.
	// It reads persisted flags but must not consume one-time placements.
	TransientStructureState bool
	WorldSeed               int64
	StartPos                [3]int32
	BlockCount              int
	Random                  *java26.Random
	// LevelRandom is WorldGenRegion.getRandom(), independent of postProcess Random.
	LevelRandom *java26.Random
	// GenerationContext is the owner-thread snapshot of Source level inputs.
	GenerationContext world.GenerationContext
	// SourceDimension is the Java registry key used by GlobalPos memories.
	SourceDimension string
	// CatsSpawnAsBlackAt mirrors StructureManager.getStructureWithPieceAt.
	CatsSpawnAsBlackAt func(x, y, z int32) bool
	// SettingsRandom is StructurePlaceSettings.random; Random is the supplied feature/decorator stream.
	SettingsRandom *java26.Random
	SkipEntities   bool
	PlaceFeature   func(feature any, pos [3]int32, random *java26.Random) bool
}

// StructurePieceState holds the mutable source state of a postProcess piece.
type StructurePieceState struct {
	ChestPlaced   bool
	SpawnerPlaced bool
	// Scattered pieces retain the vertical move made by their first ground query.
	// Negative HPos values are moved but sampled again by the source.
	GroundHeightSet bool
	GroundHeight    int32
	// BoundingBoxMinY retains a source piece's move separately from HPos.
	// Pyramid offsets and template-piece moves need not equal ground height.
	BoundingBoxYSet bool
	BoundingBoxMinY int32
	// TemplatePositionY tracks TP independently of the bounding box. Shipwreck
	// constructor adjustments deliberately leave the constructor box unmoved.
	TemplatePositionYSet bool
	TemplatePositionY    int32
	// HeightAdjusted is ShipwreckPiece's persisted height_adjusted flag.
	HeightAdjusted bool
	// LootPlaced holds up to four independently placed chest/dispenser slots.
	LootPlaced uint8
	// EntitiesSpawned holds the one-shot witch/cat slots of a swamp hut.
	EntitiesSpawned uint8
	// Pyramid is unpersisted source runtime state. Source NBT deliberately
	// omits archaeology candidates and the collapsed-roof position.
	Pyramid *PyramidPieceRuntimeState
}

// PyramidPieceRuntimeState belongs to the live source piece, not its saved NBT.
type PyramidPieceRuntimeState struct {
	SandPositions [][3]int32
	RoofPosition  [3]int32
}

// LoadProcessors resolves named vanilla processor lists and inline processors into executable processors.
func LoadProcessors(names []string, inline []ProcessorSpec, env ProcessorEnvironment) ([]Processor, error) {
	var specs []ProcessorSpec
	for _, name := range names {
		list, err := LoadProcessorList(name)
		if err != nil {
			return nil, err
		}
		specs = append(specs, list...)
	}
	specs = append(specs, inline...)
	out := make([]Processor, 0, len(specs))
	for _, spec := range specs {
		p, err := compileProcessor(spec, env)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

func compileProcessor(spec ProcessorSpec, env ProcessorEnvironment) (Processor, error) {
	switch spec.Type {
	case "nop":
		return processor(func(_ ProcessContext, b Block) (Block, bool, error) { return b, true, nil }), nil
	case "rule":
		return compileRuleProcessor(spec.Config, env)
	case "block_ignore":
		return compileBlockIgnore(spec.Config)
	case "gravity":
		return compileGravity(spec.Config, env)
	case "block_rot":
		return compileBlockRot(spec.Config, env)
	case "blackstone_replace":
		return compileBlackstoneReplace()
	case "jigsaw_replacement":
		return compileJigsawReplace()
	case "protected_blocks":
		return compileProtectedBlocks(spec.Config, env)
	case "block_age":
		return compileBlockAge(spec.Config, env)
	case "lava_submerged_block":
		return compileLavaSubmerged(env)
	case "capped":
		return compileCappedProcessor(spec.Config, env)
	default:
		return Processor{}, fmt.Errorf("unsupported Java structure processor %q", spec.Type)
	}
}

func compileGravity(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	offset := asInt32(config["offset"])
	heightmap, _ := config["heightmap"].(string)
	oceanFloor := strings.Contains(strings.ToLower(heightmap), "ocean_floor")
	return processor(func(ctx ProcessContext, b Block) (Block, bool, error) {
		if env.BaseHeight == nil {
			return b, false, fmt.Errorf("Java gravity processor requires base-height queries")
		}
		height := env.BaseHeight(b.Pos[0], b.Pos[2], oceanFloor)
		b.Pos[1] = height + offset + ctx.RelativePosition[1]
		return b, true, nil
	}), nil
}

func compileRuleProcessor(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	rawRules, ok := config["rules"].([]any)
	if !ok {
		return Processor{}, fmt.Errorf("Java rule processor has no rules")
	}
	rules := make([]struct {
		config, input, location map[string]any
		state                   world.BlockState
	}, len(rawRules))
	for i, raw := range rawRules {
		rule, ok := raw.(map[string]any)
		if !ok {
			return Processor{}, fmt.Errorf("malformed Java processor rule")
		}
		input, _ := rule["input_predicate"].(map[string]any)
		location, _ := rule["location_predicate"].(map[string]any)
		var err error
		rules[i].config = rule
		rules[i].input, err = compileStateRule(input)
		if err != nil {
			return Processor{}, err
		}
		rules[i].location, err = compileStateRule(location)
		if err != nil {
			return Processor{}, err
		}
		rules[i].state, err = decodeOutputState(rule["output_state"])
		if err != nil {
			return Processor{}, err
		}
	}
	return processor(func(ctx ProcessContext, b Block) (Block, bool, error) {
		localRandom := java26.NewLegacyRandom(javaBlockPositionSeed(b.Pos))
		random := &localRandom
		for _, compiled := range rules {
			rule := compiled.config
			if !testBlockRule(compiled.input, b, random) || !testLocationRule(compiled.location, ctx, env, random) || !testPositionRule(rule["position_predicate"], ctx, random) {
				continue
			}
			b.State = compiled.state
			if outputNBT, ok := rule["output_nbt"].(map[string]any); ok {
				b.NBT = mergeNBT(b.NBT, outputNBT)
			}
			if modifier, ok := rule["block_entity_modifier"].(map[string]any); ok {
				kind := strings.TrimPrefix(stringConfigValue(modifier["type"]), "minecraft:")
				if kind != "append_loot" {
					return b, false, fmt.Errorf("unsupported Java block entity modifier %q", kind)
				}
				lootTable := stringConfigValue(modifier["loot_table"])
				if lootTable == "" {
					return b, false, fmt.Errorf("Java append_loot modifier has no loot_table")
				}
				b.NBT = mergeNBT(b.NBT, map[string]any{"LootTable": lootTable, "LootTableSeed": random.NextLong()})
			}
			return b, true, nil
		}
		return b, true, nil
	}), nil
}

func testBlockRule(predicate map[string]any, b Block, r *java26.Random) bool {
	return testStateRule(predicate, b.State, b.Pos, r)
}

// compileStateRule replaces encoded states with immutable, default-completed
// values once per processor, rather than allocating property maps per block.
// Only modified predicate paths are copied; the resource remains unchanged.
func compileStateRule(predicate map[string]any) (map[string]any, error) {
	if predicate == nil {
		return nil, nil
	}
	kind := strings.TrimPrefix(stringConfigValue(predicate["predicate_type"]), "minecraft:")
	switch kind {
	case "blockstate_match", "random_blockstate_match":
		state, err := decodeOutputState(predicate["block_state"])
		if err != nil {
			return nil, err
		}
		compiled := maps.Clone(predicate)
		compiled["block_state"] = state
		return compiled, nil
	case "all_of", "any_of":
		children, ok := predicate["rules"].([]any)
		if !ok {
			return nil, fmt.Errorf("Java %s predicate has no rules", kind)
		}
		compiledChildren := make([]any, len(children))
		for i, raw := range children {
			child, ok := raw.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("malformed Java %s child predicate", kind)
			}
			compiled, err := compileStateRule(child)
			if err != nil {
				return nil, err
			}
			compiledChildren[i] = compiled
		}
		compiled := maps.Clone(predicate)
		compiled["rules"] = compiledChildren
		return compiled, nil
	case "not":
		child, ok := predicate["rule"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Java not predicate has no rule")
		}
		compiledChild, err := compileStateRule(child)
		if err != nil {
			return nil, err
		}
		compiled := maps.Clone(predicate)
		compiled["rule"] = compiledChild
		return compiled, nil
	case "always_true", "block_match", "random_block_match", "height_match", "tag_match":
		return predicate, nil
	default:
		return nil, fmt.Errorf("unsupported Java rule predicate %q", kind)
	}
}

func testStateRule(predicate map[string]any, state world.BlockState, pos [3]int32, r *java26.Random) bool {
	if predicate == nil {
		return true
	}
	typeName, _ := predicate["predicate_type"].(string)
	typeName = strings.TrimPrefix(typeName, "minecraft:")
	switch typeName {
	case "always_true":
		return true
	case "block_match":
		return world.NormaliseBlockName(stringConfigValue(predicate["block"])) == world.NormaliseBlockName(state.Name)
	case "random_block_match":
		if world.NormaliseBlockName(stringConfigValue(predicate["block"])) != world.NormaliseBlockName(state.Name) {
			return false
		}
		return r.NextFloat() < asFloat32(predicate["probability"])
	case "blockstate_match", "random_blockstate_match":
		expected, ok := predicate["block_state"].(world.BlockState)
		if !ok || expected.Name != world.NormaliseBlockName(state.Name) || !equalProperties(expected.Properties, javadata.BlockPropertiesWithDefaults(state.Name, state.Properties)) {
			return false
		}
		if typeName == "random_blockstate_match" {
			return r.NextFloat() < asFloat32(predicate["probability"])
		}
		return true
	case "height_match":
		y := int(pos[1])
		return intConfigValue(predicate["min_inclusive"]) <= y && y <= intConfigValue(predicate["max_inclusive"])
	case "tag_match":
		match, err := javadata.BlockTagContains(stringConfigValue(predicate["tag"]), world.NormaliseBlockName(state.Name))
		return err == nil && match
	case "all_of", "any_of":
		rules, ok := predicate["rules"].([]any)
		if !ok {
			return false
		}
		for _, raw := range rules {
			child, ok := raw.(map[string]any)
			if !ok {
				return false
			}
			matched := testStateRule(child, state, pos, r)
			if typeName == "all_of" && !matched {
				return false
			}
			if typeName == "any_of" && matched {
				return true
			}
		}
		return typeName == "all_of"
	case "not":
		child, ok := predicate["rule"].(map[string]any)
		return ok && !testStateRule(child, state, pos, r)
	default:
		return false
	}
}

func testPositionRule(raw any, ctx ProcessContext, r *java26.Random) bool {
	if raw == nil {
		return true
	}
	predicate, ok := raw.(map[string]any)
	if !ok {
		return false
	}
	kind := strings.TrimPrefix(stringConfigValue(predicate["predicate_type"]), "minecraft:")
	if kind == "always_true" {
		return true
	}
	if kind != "axis_aligned_linear_pos" && kind != "linear_pos" {
		return false
	}
	dx, dy, dz := ctx.Position[0]-ctx.Origin[0], ctx.Position[1]-ctx.Origin[1], ctx.Position[2]-ctx.Origin[2]
	distance := absInt32(dx) + absInt32(dy) + absInt32(dz)
	if kind == "axis_aligned_linear_pos" {
		axis := strings.TrimPrefix(stringConfigValue(predicate["axis"]), "minecraft:")
		if axis == "" {
			axis = "y"
		}
		index, ok := map[string]int{"x": 0, "y": 1, "z": 2}[axis]
		if !ok {
			return false
		}
		distance = absInt32(ctx.Position[index] - ctx.Origin[index])
	}
	minDistance, maxDistance := int32(intConfigValue(predicate["min_dist"])), int32(intConfigValue(predicate["max_dist"]))
	if maxDistance <= minDistance {
		return false
	}
	t := float32(distance-minDistance) / float32(maxDistance-minDistance)
	t = max(0, min(1, t))
	probability := asFloat32(predicate["min_chance"]) + (asFloat32(predicate["max_chance"])-asFloat32(predicate["min_chance"]))*t
	return r.NextFloat() <= probability
}

func testLocationRule(predicate map[string]any, ctx ProcessContext, env ProcessorEnvironment, random *java26.Random) bool {
	if predicate == nil {
		return true
	}
	typeName, _ := predicate["predicate_type"].(string)
	switch strings.TrimPrefix(typeName, "minecraft:") {
	case "always_true":
		return true
	}
	if env.BaseBlock == nil {
		return false
	}
	native := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2])
	if native == nil {
		return false
	}
	nativeState, err := world.ResolveBlock(world.DefaultBlockRegistry, native)
	if err != nil {
		return false
	}
	state, err := world.JavaState(world.DefaultBlockRegistry, nativeState)
	if err != nil {
		return false
	}
	return testStateRule(predicate, state, ctx.Position, random)
}
func absInt32(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}

func compileBlockIgnore(config map[string]any) (Processor, error) {
	values, ok := config["blocks"].([]any)
	if !ok {
		return Processor{}, fmt.Errorf("Java block_ignore has no blocks")
	}
	ignored := make(map[string]struct{}, len(values))
	for _, v := range values {
		if name, ok := v.(string); ok {
			ignored[name] = struct{}{}
		}
	}
	return processor(func(_ ProcessContext, b Block) (Block, bool, error) {
		_, drop := ignored[b.State.Name]
		return b, !drop, nil
	}), nil
}
func compileBlockRot(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	probability := asFloat32(config["integrity"])
	tag := ""
	var names map[string]struct{}
	selection, restricted := config["rottable_blocks"]
	switch values := selection.(type) {
	case nil:
	case string:
		if value, isTag := strings.CutPrefix(values, "#"); isTag {
			tag = value
		} else {
			names = map[string]struct{}{world.NormaliseBlockName(values): {}}
		}
	case []any:
		names = make(map[string]struct{}, len(values))
		for _, value := range values {
			name, ok := value.(string)
			if !ok {
				return Processor{}, fmt.Errorf("Java block-rot holder is not a block identifier")
			}
			names[world.NormaliseBlockName(name)] = struct{}{}
		}
	default:
		return Processor{}, fmt.Errorf("invalid Java block-rot holder set %T", selection)
	}
	return processor(func(ctx ProcessContext, block Block) (Block, bool, error) {
		if tag != "" {
			match, err := javadata.BlockTagContains(tag, block.State.Name)
			if err != nil {
				return block, false, err
			}
			if !match {
				return block, true, nil
			}
		}
		if restricted && tag == "" {
			if _, ok := names[block.State.Name]; !ok {
				return block, true, nil
			}
		}
		var positional java26.Random
		random := processorRandom(env.SettingsRandom, ctx.Position, &positional)
		return block, random.NextFloat() <= probability, nil
	}), nil
}
func compileBlackstoneReplace() (Processor, error) {
	replacements := map[string]string{"minecraft:cobblestone": "minecraft:blackstone", "minecraft:mossy_cobblestone": "minecraft:blackstone", "minecraft:stone": "minecraft:polished_blackstone", "minecraft:stone_bricks": "minecraft:polished_blackstone_bricks", "minecraft:mossy_stone_bricks": "minecraft:polished_blackstone_bricks", "minecraft:cobblestone_stairs": "minecraft:blackstone_stairs", "minecraft:mossy_cobblestone_stairs": "minecraft:blackstone_stairs", "minecraft:stone_stairs": "minecraft:polished_blackstone_stairs", "minecraft:stone_brick_stairs": "minecraft:polished_blackstone_brick_stairs", "minecraft:mossy_stone_brick_stairs": "minecraft:polished_blackstone_brick_stairs", "minecraft:cobblestone_slab": "minecraft:blackstone_slab", "minecraft:mossy_cobblestone_slab": "minecraft:blackstone_slab", "minecraft:smooth_stone_slab": "minecraft:polished_blackstone_slab", "minecraft:stone_slab": "minecraft:polished_blackstone_slab", "minecraft:stone_brick_slab": "minecraft:polished_blackstone_brick_slab", "minecraft:mossy_stone_brick_slab": "minecraft:polished_blackstone_brick_slab", "minecraft:stone_brick_wall": "minecraft:polished_blackstone_brick_wall", "minecraft:mossy_stone_brick_wall": "minecraft:polished_blackstone_brick_wall", "minecraft:cobblestone_wall": "minecraft:blackstone_wall", "minecraft:mossy_cobblestone_wall": "minecraft:blackstone_wall", "minecraft:chiseled_stone_bricks": "minecraft:chiseled_polished_blackstone", "minecraft:cracked_stone_bricks": "minecraft:cracked_polished_blackstone_bricks", "minecraft:iron_bars": "minecraft:iron_chain"}
	return processor(func(_ ProcessContext, b Block) (Block, bool, error) {
		name, ok := replacements[b.State.Name]
		if !ok {
			return b, true, nil
		}
		properties := make(map[string]any)
		for key, value := range b.State.Properties {
			if strings.Contains(b.State.Name, "stairs") && (key == "facing" || key == "half" || key == "shape") || strings.Contains(b.State.Name, "slab") && key == "type" {
				properties[key] = value
			}
		}
		b.State = world.BlockState{Name: name, Properties: properties}
		return b, true, nil
	}), nil
}
func compileJigsawReplace() (Processor, error) {
	return processor(func(ctx ProcessContext, b Block) (Block, bool, error) {
		if b.State.Name != "minecraft:jigsaw" {
			return b, true, nil
		}
		if b.NBT == nil {
			slog.Warn("Java jigsaw block is missing NBT", "position", ctx.Position)
			return b, true, nil
		}
		finalState, ok := b.NBT["final_state"].(string)
		if !ok {
			finalState = "minecraft:air"
		}
		state, err := world.ParseJavaBlockState(finalState)
		if err != nil {
			slog.Error("Invalid Java jigsaw final_state", "state", finalState, "position", ctx.Position, "error", err)
			return b, false, nil
		}
		if state.Name == "minecraft:structure_void" {
			return b, false, nil
		}
		b.State = state
		b.NBT = nil
		return b, true, nil
	}), nil
}
func compileProtectedBlocks(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	tag, _ := config["value"].(string)
	if tag == "" {
		return Processor{}, fmt.Errorf("Java protected_blocks processor missing value tag")
	}
	return processor(func(ctx ProcessContext, b Block) (Block, bool, error) {
		if env.BaseBlock == nil {
			return b, true, nil
		}
		base, _ := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2]).EncodeBlock()
		protected, err := javadata.BlockTagContains(tag, base)
		return b, !protected, err
	}), nil
}
func compileBlockAge(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	mossiness := asFloat32(config["mossiness"])
	return processor(func(ctx ProcessContext, b Block) (Block, bool, error) {
		var positional java26.Random
		random := processorRandom(env.SettingsRandom, ctx.Position, &positional)
		state := b.State
		switch state.Name {
		case "minecraft:stone", "minecraft:stone_bricks", "minecraft:chiseled_stone_bricks":
			if random.NextFloat() >= 0.5 {
				return b, true, nil
			}
			// Java eagerly constructs both stair alternatives before selecting
			// the replacement. Consume both pairs without allocating unused maps.
			facing, half := random.NextInt(4), random.NextInt(2)
			mossyFacing, mossyHalf := random.NextInt(4), random.NextInt(2)
			mossy := random.NextFloat() < mossiness
			choice := random.NextInt(2)
			state = world.BlockState{Name: "minecraft:cracked_stone_bricks"}
			if mossy {
				state.Name = "minecraft:mossy_stone_bricks"
			}
			if choice == 1 {
				state.Name = "minecraft:stone_brick_stairs"
				if mossy {
					state.Name = "minecraft:mossy_stone_brick_stairs"
					facing, half = mossyFacing, mossyHalf
				}
				state.Properties = map[string]any{
					"facing": [...]string{"north", "east", "south", "west"}[facing],
					"half":   [...]string{"top", "bottom"}[half],
					"shape":  "straight", "waterlogged": false,
				}
			}
		case "minecraft:obsidian":
			if random.NextFloat() < 0.15 {
				state.Name = "minecraft:crying_obsidian"
			}
		default:
			if matches, _ := javadata.BlockTagContains("minecraft:stairs", state.Name); matches {
				if random.NextFloat() >= 0.5 {
					return b, true, nil
				}
				if random.NextFloat() < mossiness {
					if random.NextInt(2) == 0 {
						state.Name = "minecraft:mossy_stone_brick_stairs"
					} else {
						state.Name = "minecraft:mossy_stone_brick_slab"
						state.Properties = agedSlabProperties()
					}
				} else if random.NextInt(2) == 0 {
					state.Name = "minecraft:stone_slab"
					state.Properties = agedSlabProperties()
				} else {
					state.Name = "minecraft:stone_brick_slab"
					state.Properties = agedSlabProperties()
				}
			} else if matches, _ := javadata.BlockTagContains("minecraft:slabs", state.Name); matches {
				if random.NextFloat() < mossiness {
					state.Name = "minecraft:mossy_stone_brick_slab"
				} else {
					return b, true, nil
				}
			} else if matches, _ := javadata.BlockTagContains("minecraft:walls", state.Name); matches {
				if random.NextFloat() < mossiness {
					state.Name = "minecraft:mossy_stone_brick_wall"
				} else {
					return b, true, nil
				}
			} else {
				return b, true, nil
			}
		}
		b.State = state
		return b, true, nil
	}), nil
}

func agedSlabProperties() map[string]any {
	return map[string]any{"type": "bottom", "waterlogged": false}
}

func compileLavaSubmerged(env ProcessorEnvironment) (Processor, error) {
	return processor(func(ctx ProcessContext, b Block) (Block, bool, error) {
		if env.BaseBlock == nil {
			return b, true, fmt.Errorf("Java lava-submerged processor requires base-block queries")
		}
		base := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2])
		name, _ := base.EncodeBlock()
		if name != "minecraft:lava" && name != "minecraft:flowing_lava" {
			return b, true, nil
		}
		shape, err := javadata.BlockGenerationShape(b.State.Name, b.State.Properties)
		if err != nil {
			return b, true, fmt.Errorf("resolve Java lava-submerged shape %q: %w", b.State.Name, err)
		}
		if shape.OutlineFull {
			return b, true, nil
		}
		b.State = world.BlockState{Name: "minecraft:lava"}
		return b, true, nil
	}), nil
}

func compileCappedProcessor(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	delegateConfig, ok := config["delegate"].(map[string]any)
	if !ok {
		return Processor{}, fmt.Errorf("Java capped processor has no delegate")
	}
	processorType, _ := delegateConfig["processor_type"].(string)
	processorType = strings.TrimPrefix(processorType, "minecraft:")
	delegateConfig = mergeConfigWithoutType(delegateConfig)
	compiled, err := compileProcessor(ProcessorSpec{Type: processorType, Config: delegateConfig}, env)
	if err != nil {
		return Processor{}, err
	}
	return Processor{
		EvaluatesEntirePieceState: true,
		finalize: func(ctx ProcessContext, original []Block, originalIndices []int, blocks []Block) ([]Block, error) {
			if len(blocks) == 0 || len(originalIndices) != len(blocks) {
				return blocks, nil
			}
			randomSource := java26.NewLegacyRandom(env.WorldSeed)
			origin := ctx.Origin
			random := randomSource.ForkPositional().At(origin[0], origin[1], origin[2])
			limit := cappedLimit(config["limit"], &random)
			if limit < 0 {
				return nil, fmt.Errorf("Java capped processor has negative limit %d", limit)
			}
			limit = min(limit, len(blocks))
			if limit == 0 || compiled.Process == nil {
				return blocks, nil
			}
			indices := make([]int, len(blocks))
			for index := range indices {
				indices[index] = index
			}
			for index := len(indices) - 1; index > 0; index-- {
				swap := int(random.NextInt(int32(index + 1)))
				indices[index], indices[swap] = indices[swap], indices[index]
			}
			replaced := 0
			for _, index := range indices {
				before := blocks[index]
				ctx.BlockIndex = originalIndices[index]
				ctx.RelativePosition = original[ctx.BlockIndex].Pos
				ctx.Position = before.Pos
				after, keep, err := compiled.Process(ctx, before)
				if err != nil {
					return nil, err
				}
				if keep && (before.Pos != after.Pos || before.State.Name != after.State.Name || !equalProperties(before.State.Properties, after.State.Properties) || !reflect.DeepEqual(before.NBT, after.NBT)) {
					blocks[index] = after
					replaced++
					if replaced == limit {
						break
					}
				}
			}
			return blocks, nil
		},
	}, nil
}

func cappedLimit(value any, random *java26.Random) int {
	switch provider := value.(type) {
	case float64:
		return int(provider)
	case int:
		return provider
	case int32:
		return int(provider)
	case map[string]any:
		kind := strings.TrimPrefix(stringConfigValue(provider["type"]), "minecraft:")
		if kind != "uniform" {
			return intConfigValue(provider["value"])
		}
		minimum := int32(intConfigValue(provider["min_inclusive"]))
		maximum := int32(intConfigValue(provider["max_inclusive"]))
		if maximum < minimum {
			return int(minimum)
		}
		return int(minimum + random.NextInt(maximum-minimum+1))
	default:
		return 0
	}
}

func mergeConfigWithoutType(config map[string]any) map[string]any {
	out := make(map[string]any, len(config)-1)
	for key, value := range config {
		if key != "processor_type" {
			out[key] = value
		}
	}
	return out
}
func decodeOutputState(value any) (world.BlockState, error) {
	var state world.BlockState
	switch encoded := value.(type) {
	case string:
		state.Name = encoded
	case map[string]any:
		state.Name, _ = encoded["id"].(string)
		properties, _ := encoded["properties"].(map[string]any)
		state.Properties = world.DecodeStateProperties(properties)
	default:
		return state, fmt.Errorf("invalid Java processor block state %T", value)
	}
	if state.Name == "" {
		return state, fmt.Errorf("Java processor block state has no id")
	}
	state.Name = world.NormaliseBlockName(state.Name)
	state.Properties = javadata.BlockPropertiesWithDefaults(state.Name, state.Properties)
	return state, nil
}
func javaBlockPositionSeed(pos [3]int32) int64 {
	seed := int64(pos[0]*3129871) ^ int64(pos[2])*116129781 ^ int64(pos[1])
	seed = seed*seed*42317861 + seed*11
	return seed >> 16
}

func processorRandom(settings *java26.Random, pos [3]int32, positional *java26.Random) *java26.Random {
	if settings != nil {
		return settings
	}
	*positional = java26.NewLegacyRandom(javaBlockPositionSeed(pos))
	return positional
}
func mergeNBT(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
func equalProperties(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
func asFloat32(v any) float32 {
	switch n := v.(type) {
	case float64:
		return float32(n)
	case float32:
		return n
	case int:
		return float32(n)
	case int32:
		return float32(n)
	}
	return 0
}
func stringConfigValue(value any) string { s, _ := value.(string); return s }
func intConfigValue(value any) int {
	switch n := value.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int32:
		return int(n)
	}
	return 0
}
