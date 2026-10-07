package javastructure

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

// ProcessorEnvironment supplies world queries and placement context used by Java processors.
type ProcessorEnvironment struct {
	BaseBlock    func(x, y, z int32) world.Block
	JavaBiomeAt  func(x, y, z int32) string
	BaseHeight   func(x, z int32, oceanFloor bool) int32
	WorldSeed    int64
	StartPos     [3]int32
	BlockCount   int
	Random       *java26.Random
	SkipEntities bool
	PlaceFeature func(feature any, pos [3]int32, random *java26.Random) bool
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
		return func(_ ProcessContext, b Block) (Block, bool, error) { return b, true, nil }, nil
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
		return nil, fmt.Errorf("unsupported Java structure processor %q", spec.Type)
	}
}

func compileGravity(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	offset := asInt32(config["offset"])
	heightmap, _ := config["heightmap"].(string)
	oceanFloor := strings.Contains(strings.ToLower(heightmap), "ocean_floor")
	return func(ctx ProcessContext, b Block) (Block, bool, error) {
		if env.BaseHeight == nil {
			return b, false, fmt.Errorf("Java gravity processor requires base-height queries")
		}
		height := env.BaseHeight(b.Pos[0], b.Pos[2], oceanFloor)
		b.Pos[1] = height + offset + ctx.RelativePosition[1]
		return b, true, nil
	}, nil
}

func compileRuleProcessor(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	rules, ok := config["rules"].([]any)
	if !ok {
		return nil, fmt.Errorf("Java rule processor has no rules")
	}
	return func(ctx ProcessContext, b Block) (Block, bool, error) {
		localRandom := java26.NewLegacyRandom(javaBlockPositionSeed(b.Pos))
		random := &localRandom
		for _, raw := range rules {
			rule, ok := raw.(map[string]any)
			if !ok {
				return b, false, fmt.Errorf("malformed Java processor rule")
			}
			input, _ := rule["input_predicate"].(map[string]any)
			location, _ := rule["location_predicate"].(map[string]any)
			if !testBlockRule(input, b, random) || !testLocationRule(location, ctx, env) || !testPositionRule(rule["position_predicate"], ctx, random) {
				continue
			}
			state, err := decodeOutputState(rule["output_state"])
			if err != nil {
				return b, false, err
			}
			b.State = state
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
	}, nil
}

func testBlockRule(predicate map[string]any, b Block, r *java26.Random) bool {
	if predicate == nil {
		return true
	}
	typeName, _ := predicate["predicate_type"].(string)
	typeName = strings.TrimPrefix(typeName, "minecraft:")
	switch typeName {
	case "always_true":
		return true
	case "always_false":
		return false
	case "block_match":
		name, _ := predicate["block"].(string)
		return b.State.Name == name
	case "random_block_match":
		name, _ := predicate["block"].(string)
		if b.State.Name != name {
			return false
		}
		return r.NextFloat() < asFloat32(predicate["probability"])
	case "block_state_match":
		state, err := decodeOutputState(predicate["block_state"])
		return err == nil && state.Name == b.State.Name && equalProperties(state.Properties, b.State.Properties)
	case "tag_match":
		tag, _ := predicate["tag"].(string)
		match, err := javadata.BlockTagContains(tag, b.State.Name)
		return err == nil && match
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
	if kind != "axis_aligned_linear_pos" {
		return false
	}
	axis := strings.TrimPrefix(stringConfigValue(predicate["axis"]), "minecraft:")
	if axis == "" {
		axis = "y"
	}
	index := map[string]int{"x": 0, "y": 1, "z": 2}[axis]
	distance := ctx.Position[index] - ctx.Origin[index]
	if distance < 0 {
		distance = -distance
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

func testLocationRule(predicate map[string]any, ctx ProcessContext, env ProcessorEnvironment) bool {
	if predicate == nil {
		return true
	}
	typeName, _ := predicate["predicate_type"].(string)
	typeName = strings.TrimPrefix(typeName, "minecraft:")
	switch typeName {
	case "always_true":
		return true
	case "always_false":
		return false
	case "block_match":
		if env.BaseBlock == nil {
			return false
		}
		name, _ := predicate["block"].(string)
		actual, _ := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2]).EncodeBlock()
		return actual == name
	case "block_tag_match":
		if env.BaseBlock == nil {
			return false
		}
		tag, _ := predicate["tag"].(string)
		actual, _ := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2]).EncodeBlock()
		match, err := javadata.BlockTagContains(tag, actual)
		return err == nil && match
	case "biome":
		if env.JavaBiomeAt == nil {
			return false
		}
		biome, _ := predicate["biome"].(string)
		return strings.TrimPrefix(env.JavaBiomeAt(ctx.Position[0], ctx.Position[1], ctx.Position[2]), "minecraft:") == strings.TrimPrefix(biome, "minecraft:")
	default:
		return false
	}
}

func compileBlockIgnore(config map[string]any) (Processor, error) {
	values, ok := config["blocks"].([]any)
	if !ok {
		return nil, fmt.Errorf("Java block_ignore has no blocks")
	}
	ignored := make(map[string]struct{}, len(values))
	for _, v := range values {
		if name, ok := v.(string); ok {
			ignored[name] = struct{}{}
		}
	}
	return func(_ ProcessContext, b Block) (Block, bool, error) {
		_, drop := ignored[b.State.Name]
		return b, !drop, nil
	}, nil
}
func compileBlockRot(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	probability := asFloat32(config["integrity"])
	tag := ""
	if value, ok := config["rottable_blocks"].(map[string]any); ok {
		tag, _ = value["tag"].(string)
	}
	names := make(map[string]struct{})
	if values, ok := config["rottable_blocks"].([]any); ok {
		for _, value := range values {
			if name, ok := value.(string); ok {
				names[name] = struct{}{}
			}
		}
	}
	return func(ctx ProcessContext, block Block) (Block, bool, error) {
		if tag != "" {
			match, err := javadata.BlockTagContains(tag, block.State.Name)
			if err != nil {
				return block, false, err
			}
			if !match {
				return block, true, nil
			}
		}
		if len(names) > 0 {
			if _, ok := names[block.State.Name]; !ok {
				return block, true, nil
			}
		}
		return block, processorRandom(env, ctx.Position).NextFloat() <= probability, nil
	}, nil
}
func compileBlackstoneReplace() (Processor, error) {
	replacements := map[string]string{"minecraft:cobblestone": "minecraft:blackstone", "minecraft:mossy_cobblestone": "minecraft:blackstone", "minecraft:stone": "minecraft:polished_blackstone", "minecraft:stone_bricks": "minecraft:polished_blackstone_bricks", "minecraft:mossy_stone_bricks": "minecraft:polished_blackstone_bricks", "minecraft:cobblestone_stairs": "minecraft:blackstone_stairs", "minecraft:mossy_cobblestone_stairs": "minecraft:blackstone_stairs", "minecraft:stone_stairs": "minecraft:polished_blackstone_stairs", "minecraft:stone_brick_stairs": "minecraft:polished_blackstone_brick_stairs", "minecraft:mossy_stone_brick_stairs": "minecraft:polished_blackstone_brick_stairs", "minecraft:cobblestone_slab": "minecraft:blackstone_slab", "minecraft:mossy_cobblestone_slab": "minecraft:blackstone_slab", "minecraft:smooth_stone_slab": "minecraft:polished_blackstone_slab", "minecraft:stone_slab": "minecraft:polished_blackstone_slab", "minecraft:stone_brick_slab": "minecraft:polished_blackstone_brick_slab", "minecraft:mossy_stone_brick_slab": "minecraft:polished_blackstone_brick_slab", "minecraft:stone_brick_wall": "minecraft:polished_blackstone_brick_wall", "minecraft:mossy_stone_brick_wall": "minecraft:polished_blackstone_brick_wall", "minecraft:cobblestone_wall": "minecraft:blackstone_wall", "minecraft:mossy_cobblestone_wall": "minecraft:blackstone_wall", "minecraft:chiseled_stone_bricks": "minecraft:chiseled_polished_blackstone", "minecraft:cracked_stone_bricks": "minecraft:cracked_polished_blackstone_bricks", "minecraft:iron_bars": "minecraft:iron_chain"}
	return func(_ ProcessContext, b Block) (Block, bool, error) {
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
	}, nil
}
func compileJigsawReplace() (Processor, error) {
	return func(_ ProcessContext, b Block) (Block, bool, error) {
		if b.State.Name != "minecraft:jigsaw" {
			return b, true, nil
		}
		finalState := b.NBT["final_state"]
		if finalState == nil {
			finalState = "minecraft:air"
		}
		state, err := decodeOutputState(finalState)
		if err != nil {
			return b, false, err
		}
		b.State = state
		b.NBT = nil
		return b, true, nil
	}, nil
}
func compileProtectedBlocks(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	tag, _ := config["value"].(string)
	if tag == "" {
		return nil, fmt.Errorf("Java protected_blocks processor missing value tag")
	}
	return func(ctx ProcessContext, b Block) (Block, bool, error) {
		if env.BaseBlock == nil {
			return b, true, nil
		}
		base, _ := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2]).EncodeBlock()
		protected, err := javadata.BlockTagContains(tag, base)
		return b, !protected, err
	}, nil
}
func compileBlockAge(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	mossiness := asFloat32(config["mossiness"])
	return func(ctx ProcessContext, b Block) (Block, bool, error) {
		random := processorRandom(env, ctx.Position)
		state := b.State
		switch state.Name {
		case "minecraft:stone", "minecraft:stone_bricks", "minecraft:chiseled_stone_bricks":
			if random.NextFloat() >= 0.5 {
				return b, true, nil
			}
			mossy := random.NextFloat() < mossiness
			choice := random.NextInt(2)
			if mossy && choice == 0 {
				state.Name = "minecraft:mossy_stone_bricks"
			} else if mossy {
				state.Name = "minecraft:mossy_stone_brick_stairs"
				state.Properties = randomAgedStairProperties(random)
			} else if choice == 0 {
				state.Name = "minecraft:cracked_stone_bricks"
			} else {
				state.Name = "minecraft:stone_brick_stairs"
				state.Properties = randomAgedStairProperties(random)
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
						state.Properties = agedSlabProperties(state.Properties)
					}
				} else if random.NextInt(2) == 0 {
					state.Name = "minecraft:stone_slab"
					state.Properties = agedSlabProperties(nil)
				} else {
					state.Name = "minecraft:stone_brick_slab"
					state.Properties = agedSlabProperties(nil)
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
	}, nil
}
func randomAgedStairProperties(random *java26.Random) map[string]any {
	facing := "north"
	switch random.NextInt(4) {
	case 1:
		facing = "east"
	case 2:
		facing = "south"
	case 3:
		facing = "west"
	}
	half := "bottom"
	if random.NextInt(2) == 1 {
		half = "top"
	}
	return map[string]any{
		"facing":      facing,
		"half":        half,
		"shape":       "straight",
		"waterlogged": "false",
	}
}

func agedSlabProperties(source map[string]any) map[string]any {
	waterlogged, ok := source["waterlogged"]
	if !ok {
		waterlogged = "false"
	}
	return map[string]any{"type": "bottom", "waterlogged": waterlogged}
}

func compileLavaSubmerged(env ProcessorEnvironment) (Processor, error) {
	return func(ctx ProcessContext, b Block) (Block, bool, error) {
		if env.BaseBlock == nil {
			return b, true, fmt.Errorf("Java lava-submerged processor requires base-block queries")
		}
		base := env.BaseBlock(ctx.Position[0], ctx.Position[1], ctx.Position[2])
		name, _ := base.EncodeBlock()
		if name != "minecraft:lava" {
			return b, true, nil
		}
		block, ok := world.BlockByName(b.State.Name, b.State.Properties)
		if !ok {
			return b, true, fmt.Errorf("unknown Java structure state %q", b.State.Name)
		}
		if _, fullBlock := block.Model().(model.Solid); fullBlock {
			return b, true, nil
		}
		b.State = world.BlockState{Name: "minecraft:lava"}
		return b, true, nil
	}, nil
}

func compileCappedProcessor(config map[string]any, env ProcessorEnvironment) (Processor, error) {
	delegateConfig, ok := config["delegate"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Java capped processor has no delegate")
	}
	processorType, _ := delegateConfig["processor_type"].(string)
	processorType = strings.TrimPrefix(processorType, "minecraft:")
	delegateConfig = mergeConfigWithoutType(delegateConfig)
	compiled, err := compileProcessor(ProcessorSpec{Type: processorType, Config: delegateConfig}, env)
	if err != nil {
		return nil, err
	}
	randomSource := java26.NewLegacyRandom(env.WorldSeed)
	random := randomSource.ForkPositional().At(env.StartPos[0], env.StartPos[1], env.StartPos[2])
	limit := cappedLimit(config["limit"], &random)
	if limit < 0 {
		return nil, fmt.Errorf("Java capped processor has negative limit %d", limit)
	}
	if limit > env.BlockCount {
		limit = env.BlockCount
	}
	indices := make([]int, env.BlockCount)
	for index := range indices {
		indices[index] = index
	}
	for index := len(indices) - 1; index > 0; index-- {
		swap := int(random.NextInt(int32(index + 1)))
		indices[index], indices[swap] = indices[swap], indices[index]
	}
	selected := make(map[int]struct{}, limit)
	for _, index := range indices[:limit] {
		selected[index] = struct{}{}
	}
	return func(ctx ProcessContext, b Block) (Block, bool, error) {
		if _, ok := selected[ctx.BlockIndex]; !ok {
			return b, true, nil
		}
		return compiled(ctx, b)
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
	if name, ok := value.(string); ok {
		return world.BlockState{Name: name}, nil
	}
	if m, ok := value.(map[string]any); ok {
		name, _ := m["Name"].(string)
		if name == "" {
			name, _ = m["name"].(string)
		}
		props, _ := m["Properties"].(map[string]any)
		if props == nil {
			props, _ = m["properties"].(map[string]any)
		}
		if name == "" {
			return world.BlockState{}, fmt.Errorf("Java processor output state has no name")
		}
		return world.BlockState{Name: name, Properties: world.DecodeStateProperties(props)}, nil
	}
	return world.BlockState{}, fmt.Errorf("invalid Java processor output state %T", value)
}
func javaBlockPositionSeed(pos [3]int32) int64 {
	seed := int64(pos[0]*3129871) ^ int64(pos[2])*116129781 ^ int64(pos[1])
	seed = seed*seed*42317861 + seed*11
	return seed >> 16
}

func processorRandom(env ProcessorEnvironment, pos [3]int32) *java26.Random {
	if env.Random != nil {
		return env.Random
	}
	random := java26.NewLegacyRandom(javaBlockPositionSeed(pos))
	return &random
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
