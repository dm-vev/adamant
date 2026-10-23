package structure

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// NativeBlockForState returns an actor-backed native block for Java states
// whose state properties are stored in a Bedrock block actor.
func NativeBlockForState(state world.BlockState) (world.Block, bool, error) {
	name := world.NormaliseBlockName(state.Name)
	if native, adapted, err := nativeStateBlock(state, name); adapted || err != nil {
		return native, adapted, err
	}
	if native, adapted, err := nativeStateActor(state, strings.TrimPrefix(name, "minecraft:")); adapted || err != nil {
		return native, adapted, err
	}
	if !strings.HasSuffix(name, "_copper_golem_statue") && name != "minecraft:copper_golem_statue" {
		return nil, false, nil
	}
	waxed := strings.HasPrefix(name, "minecraft:waxed_")
	if waxed {
		name = "minecraft:" + strings.TrimPrefix(name, "minecraft:waxed_")
	}
	var oxidation block.OxidationType
	switch name {
	case "minecraft:copper_golem_statue":
		oxidation = block.UnoxidisedOxidation()
	case "minecraft:exposed_copper_golem_statue":
		oxidation = block.ExposedOxidation()
	case "minecraft:weathered_copper_golem_statue":
		oxidation = block.WeatheredOxidation()
	case "minecraft:oxidized_copper_golem_statue":
		oxidation = block.OxidisedOxidation()
	default:
		return nil, false, nil
	}
	pose := "standing"
	if raw, exists := state.Properties["copper_golem_pose"]; exists {
		var ok bool
		pose, ok = actorStringProperty(raw)
		if !ok {
			return nil, false, fmt.Errorf("copper golem statue: invalid copper_golem_pose %v", raw)
		}
	}
	var nativePose block.CopperGolemPose
	switch pose {
	case "standing":
		nativePose = block.StandingPose()
	case "sitting":
		nativePose = block.SittingPose()
	case "running":
		nativePose = block.RunningPose()
	case "star":
		nativePose = block.StarPose()
	default:
		return nil, false, fmt.Errorf("copper golem statue: unsupported pose %q", pose)
	}
	direction, err := actorDirection(state)
	if err != nil {
		return nil, false, err
	}
	return block.CopperGolemStatue{Facing: direction, Pose: nativePose, Oxidation: oxidation, Waxed: waxed}, true, nil
}

var nativeActorSkulls = map[string]block.SkullType{
	"skeleton_skull": block.SkeletonSkull(), "wither_skeleton_skull": block.WitherSkeletonSkull(),
	"zombie_head": block.ZombieHead(), "player_head": block.PlayerHead(), "creeper_head": block.CreeperHead(),
	"dragon_head": block.DragonHead(), "piglin_head": block.PiglinHead(),
}

func nativeStateBlock(state world.BlockState, name string) (world.Block, bool, error) {
	switch nativeName := world.ActorBlockName(name); {
	case nativeName == "minecraft:bed":
		native, err := (block.Bed{}).WithCanonicalState(name, state.Properties)
		return native, true, err
	case nativeName == "minecraft:standing_banner", nativeName == "minecraft:wall_banner":
		native, err := (block.Banner{}).WithCanonicalState(name, state.Properties)
		return native, true, err
	case strings.HasSuffix(nativeName, "_shulker_box"):
		native, err := block.NewShulkerBox().WithCanonicalState(name, state.Properties)
		return native, true, err
	}
	return nil, false, nil
}

func nativeStateActor(state world.BlockState, name string) (world.Block, bool, error) {
	switch {
	case name == "sculk_sensor", name == "calibrated_sculk_sensor":
		phase := block.SculkSensorInactive
		if raw, exists := state.Properties["sculk_sensor_phase"]; exists {
			switch raw {
			case "inactive":
			case "active":
				phase = block.SculkSensorActive
			case "cooldown":
				phase = block.SculkSensorCooldown
			default:
				return nil, false, fmt.Errorf("sensor: invalid phase %v", raw)
			}
		}
		var power int64
		if raw, exists := state.Properties["power"]; exists {
			var valid bool
			power, valid = integer(raw)
			if !valid || power < 0 || power > 15 {
				return nil, false, fmt.Errorf("sensor: invalid power %v", raw)
			}
		}
		sensor := block.NewSculkSensorState(phase, int(power), 0)
		if name == "sculk_sensor" {
			return sensor, true, nil
		}
		facing, err := actorDirection(state)
		if err != nil {
			return nil, false, err
		}
		return block.CalibratedSculkSensor{SculkSensor: sensor, Facing: facing}, true, nil
	case strings.HasSuffix(name, "_head"), strings.HasSuffix(name, "_skull"):
		skull, ok := nativeActorSkulls[strings.Replace(name, "_wall_", "_", 1)]
		if !ok {
			return nil, false, nil
		}
		attach, err := actorAttachment(state, strings.Contains(name, "_wall_"), 0)
		if err != nil {
			return nil, false, err
		}
		powered, err := actorOptionalBool(state, "powered")
		if err != nil {
			return nil, false, err
		}
		return block.Skull{Type: skull, Attach: attach, Powered: powered}, true, nil
	}
	return nil, false, nil
}

func actorDirection(state world.BlockState) (cube.Direction, error) {
	facing := "north"
	if raw, exists := state.Properties["facing"]; exists {
		var ok bool
		facing, ok = actorStringProperty(raw)
		if !ok {
			return 0, fmt.Errorf("actor %s: invalid facing %v", state.Name, raw)
		}
	}
	switch facing {
	case "north":
		return cube.North, nil
	case "south":
		return cube.South, nil
	case "west":
		return cube.West, nil
	case "east":
		return cube.East, nil
	}
	return 0, fmt.Errorf("actor %s: invalid facing %q", state.Name, facing)
}

func actorAttachment(state world.BlockState, wall bool, defaultRotation int64) (block.Attachment, error) {
	if wall {
		facing, err := actorDirection(state)
		return block.WallAttachment(facing), err
	}
	rotation := defaultRotation
	if raw, exists := state.Properties["rotation"]; exists {
		var ok bool
		rotation, ok = integer(raw)
		if text, isString := raw.(string); isString {
			var err error
			rotation, err = strconv.ParseInt(text, 10, 64)
			ok = err == nil
		}
		if !ok || rotation < 0 || rotation > 15 {
			return block.Attachment{}, fmt.Errorf("actor %s: invalid rotation %v", state.Name, raw)
		}
	}
	return block.StandingAttachment(cube.Orientation(rotation)), nil
}

func actorOptionalBool(state world.BlockState, key string) (bool, error) {
	if raw, exists := state.Properties[key]; exists {
		value, ok := actorBoolProperty(raw)
		if !ok {
			return false, fmt.Errorf("actor %s: invalid %s %v", state.Name, key, raw)
		}
		return value, nil
	}
	return false, nil
}

func stateDerivedActor(native world.Block) bool {
	switch native.(type) {
	case block.CopperGolemStatue, block.Bed, block.Banner, block.Skull,
		block.SculkSensor, block.CalibratedSculkSensor, block.ShulkerBox:
		return true
	}
	return false
}

// ResolveNative resolves a Java state normally, or a native block override
// directly through the Bedrock registry for Java entities adapted to blocks.
func (b Block) ResolveNative(registry chunk.BlockRegistry, existing ...world.NativeState) (world.NativeState, error) {
	if b.NativeBlock == nil {
		return world.ResolveState(registry, b.State)
	}
	state, err := world.ResolveBlock(registry, b.NativeBlock)
	if err != nil {
		return world.NativeState{}, err
	}
	waterlogged, hasWaterlogged := actorBoolProperty(b.State.Properties["waterlogged"])
	if hasWaterlogged {
		if waterlogged {
			liquid, ok := world.WaterSourceRuntimeID(registry)
			if !ok {
				return world.NativeState{}, fmt.Errorf("native block resolver: waterlogged state has no native water state")
			}
			state.Liquid = liquid
		} else {
			state.Liquid = registry.AirRuntimeID()
		}
	}
	if displacer, ok := b.NativeBlock.(world.LiquidDisplacer); ok && len(existing) != 0 && !hasWaterlogged {
		current := existing[0]
		fluid := current.Liquid
		if fluid == registry.AirRuntimeID() && registry.LiquidBlock(current.Block) {
			fluid = current.Block
		}
		if fluid != registry.AirRuntimeID() && registry.LiquidBlock(fluid) {
			blockRegistry, ok := registry.(world.BlockRegistry)
			if !ok {
				return world.NativeState{}, fmt.Errorf("native block resolver: registry cannot resolve fluid block %d", fluid)
			}
			fluidBlock, ok := blockRegistry.BlockByRuntimeID(fluid)
			if !ok {
				return world.NativeState{}, fmt.Errorf("native block resolver: unknown fluid block %d", fluid)
			}
			liquid, ok := fluidBlock.(world.Liquid)
			if !ok {
				return world.NativeState{}, fmt.Errorf("native block resolver: block %d is flagged liquid but does not implement world.Liquid", fluid)
			}
			if displacer.CanDisplace(liquid) {
				state.Liquid = fluid
			}
		}
	}
	if stateDerivedActor(b.NativeBlock) {
		source := b.State
		if !hasWaterlogged && state.Liquid != registry.AirRuntimeID() && gamedata.BlockHasProperty(source.Name, "waterlogged") {
			source.Properties = world.DecodeStateProperties(source.Properties)
			if source.Properties == nil {
				source.Properties = make(map[string]any, 1)
			}
			source.Properties["waterlogged"] = true
		}
		return state.WithCanonicalState(registry, source)
	}
	return state, nil
}
func actorStringProperty(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok
}

func actorBoolProperty(value any) (bool, bool) {
	switch value := value.(type) {
	case bool:
		return value, true
	case uint8:
		return value != 0, true
	case int32:
		return value != 0, true
	case string:
		switch value {
		case "true":
			return true, true
		case "false":
			return false, true
		}
	}
	return false, false
}
