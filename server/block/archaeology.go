package block

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

var (
	hashSuspiciousSand    = NextHash()
	hashSuspiciousGravel  = NextHash()
)

// SuspiciousSand is an archaeological block that yields its buried item after ten brush strokes.
type SuspiciousSand struct {
	gravityAffected
	solid
	snare
	Dusted uint8
	// Hanging preserves the native palette flag; it is not a Java block property.
	Hanging bool
	LootTable string
	LootTableSeed int64
	Item item.Stack
	HitDirection cube.Face
	HasHitDirection bool
	brushCount uint8
	brushCountResetsAtTick int64
	coolDownEndsAtTick int64
}

// SuspiciousGravel is an archaeological block that yields its buried item after ten brush strokes.
type SuspiciousGravel struct {
	gravityAffected
	solid
	snare
	Dusted uint8
	// Hanging preserves the native palette flag; it is not a Java block property.
	Hanging bool
	LootTable string
	LootTableSeed int64
	Item item.Stack
	HitDirection cube.Face
	HasHitDirection bool
	brushCount uint8
	brushCountResetsAtTick int64
	coolDownEndsAtTick int64
}

// NewSuspiciousSand creates suspicious sand configured with a Java archaeology loot table.
func NewSuspiciousSand(table string, seed int64) SuspiciousSand {
	return SuspiciousSand{LootTable: table, LootTableSeed: seed}
}

// NewSuspiciousGravel creates suspicious gravel configured with a Java archaeology loot table.
func NewSuspiciousGravel(table string, seed int64) SuspiciousGravel {
	return SuspiciousGravel{LootTable: table, LootTableSeed: seed}
}

func (s SuspiciousSand) EncodeItem() (string, int16) { return "minecraft:suspicious_sand", 0 }
func (s SuspiciousGravel) EncodeItem() (string, int16) { return "minecraft:suspicious_gravel", 0 }
func (s SuspiciousSand) EncodeBlock() (string, map[string]any) {
	return "minecraft:suspicious_sand", map[string]any{"brushed_progress": int32(min(s.Dusted, 3)), "hanging": boolByte(s.Hanging)}
}
func (s SuspiciousGravel) EncodeBlock() (string, map[string]any) {
	return "minecraft:suspicious_gravel", map[string]any{"brushed_progress": int32(min(s.Dusted, 3)), "hanging": boolByte(s.Hanging)}
}
func (s SuspiciousSand) Hash() (uint64, uint64) { return hashSuspiciousSand, uint64(min(s.Dusted, 3)) | uint64(boolByte(s.Hanging))<<2 }
func (s SuspiciousGravel) Hash() (uint64, uint64) { return hashSuspiciousGravel, uint64(min(s.Dusted, 3)) | uint64(boolByte(s.Hanging))<<2 }
func (SuspiciousSand) BrushSound() string { return "item.brush.brushing.sand" }
func (SuspiciousGravel) BrushSound() string { return "item.brush.brushing.gravel" }
func (SuspiciousSand) BreakInfo() BreakInfo {
	return newBreakInfo(0.25, alwaysHarvestable, shovelEffective, oneOf(Sand{}))
}
func (SuspiciousGravel) BreakInfo() BreakInfo {
	return newBreakInfo(0.25, alwaysHarvestable, shovelEffective, oneOf(Gravel{}))
}
func (s SuspiciousSand) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) { s.fall(s, pos, tx) }
func (s SuspiciousGravel) NeighbourUpdateTick(pos, _ cube.Pos, tx *world.Tx) { s.fall(s, pos, tx) }
func (s SuspiciousSand) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { s.tick(pos, tx) }
func (s SuspiciousGravel) ScheduledTick(pos cube.Pos, tx *world.Tx, _ *rand.Rand) { s.tick(pos, tx) }

func (s SuspiciousSand) EncodeNBT() map[string]any {
	return encodeBrushableNBT("minecraft:suspicious_sand", s.LootTable, s.LootTableSeed, s.Item, s.brushCount, s.HitDirection, s.HasHitDirection)
}
func (s SuspiciousGravel) EncodeNBT() map[string]any {
	return encodeBrushableNBT("minecraft:suspicious_gravel", s.LootTable, s.LootTableSeed, s.Item, s.brushCount, s.HitDirection, s.HasHitDirection)
}
func (s SuspiciousSand) DecodeNBT(data map[string]any) any {
	s.LootTable, s.LootTableSeed, s.Item, s.HitDirection, s.HasHitDirection, s.brushCount = decodeBrushableNBT(data)
	return s
}
func (s SuspiciousGravel) DecodeNBT(data map[string]any) any {
	s.LootTable, s.LootTableSeed, s.Item, s.HitDirection, s.HasHitDirection, s.brushCount = decodeBrushableNBT(data)
	return s
}

// noBrushDirection is the brush_direction BDS saves for a block nobody brushed.
const noBrushDirection = 6

// encodeBrushableNBT encodes a brushable block actor as BDS saves it. The
// client shows the item uncovered while brushing from the lowercase item.
func encodeBrushableNBT(name, table string, seed int64, stack item.Stack, brushCount uint8, direction cube.Face, hasDirection bool) map[string]any {
	data := map[string]any{"id": "BrushableBlock", "BlockEntityVersion": int32(0), "type": name, "brush_count": int32(brushCount), "brush_direction": uint8(noBrushDirection)}
	if hasDirection {
		data["brush_direction"] = uint8(direction)
	}
	if table != "" {
		data["LootTable"] = loot.BedrockTablePath(table)
		data["LootTableSeed"] = seed
	} else if !stack.Empty() {
		data["item"] = item.WriteNBT(stack, true)
	}
	return data
}

// decodeBrushableNBT reads a brushable block actor saved by BDS or, with the
// Item and hit_direction keys, by earlier Adamant versions.
func decodeBrushableNBT(data map[string]any) (string, int64, item.Stack, cube.Face, bool, uint8) {
	table, _ := data["LootTable"].(string)
	table = loot.JavaTableKey(table)
	seed, _ := nbtconv.Integer(data, "LootTableSeed")
	var stack item.Stack
	if table == "" {
		raw, ok := data["item"].(map[string]any)
		if !ok {
			raw, ok = data["Item"].(map[string]any)
		}
		if ok && len(raw) > 0 {
			stack = item.ReadNBT(raw, nil, world.BlockRegistryFromNBT(data))
		}
	}
	direction, hasDirection := cube.FaceUp, false
	if id, ok := nbtconv.Integer(data, "brush_direction"); ok {
		direction, hasDirection = legacyDirection(int32(id))
	} else if id, ok := nbtconv.Integer(data, "hit_direction"); ok {
		direction, hasDirection = legacyDirection(int32(id))
	}
	count, _ := nbtconv.Integer(data, "brush_count")
	return table, seed, stack, direction, hasDirection, uint8(min(max(count, 0), 255))
}


func legacyDirection(id int32) (cube.Face, bool) {
	switch id {
	case 0: return cube.FaceDown, true
	case 1: return cube.FaceUp, true
	case 2: return cube.FaceNorth, true
	case 3: return cube.FaceSouth, true
	case 4: return cube.FaceWest, true
	case 5: return cube.FaceEast, true
	default: return cube.FaceUp, false
	}
}

func (s SuspiciousSand) BrushAt(pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, brush item.Stack) (bool, error) {
	state := &brushState{
		Dusted: s.Dusted, LootTable: s.LootTable, LootTableSeed: s.LootTableSeed, Item: s.Item,
		HitDirection: s.HitDirection, HasHitDirection: s.HasHitDirection, BrushCount: s.brushCount,
		BrushCountResetsAtTick: s.brushCountResetsAtTick, CooldownUntilTick: s.coolDownEndsAtTick,
	}
	return brushSuspicious(pos, face, tx, user, brush, state, func(state *brushState) world.Block { return applySandBrushState(s, state) })
}
func (s SuspiciousGravel) BrushAt(pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, brush item.Stack) (bool, error) {
	state := &brushState{
		Dusted: s.Dusted, LootTable: s.LootTable, LootTableSeed: s.LootTableSeed, Item: s.Item,
		HitDirection: s.HitDirection, HasHitDirection: s.HasHitDirection, BrushCount: s.brushCount,
		BrushCountResetsAtTick: s.brushCountResetsAtTick, CooldownUntilTick: s.coolDownEndsAtTick,
	}
	return brushSuspicious(pos, face, tx, user, brush, state, func(state *brushState) world.Block { return applyGravelBrushState(s, state) })
}

// brushState is the small mutable view shared by the two concrete source blocks.
type brushState struct {
	Dusted uint8
	LootTable string
	LootTableSeed int64
	Item item.Stack
	HitDirection cube.Face
	HasHitDirection bool
	BrushCount uint8
	BrushCountResetsAtTick int64
	CooldownUntilTick int64
}

func applySandBrushState(original SuspiciousSand, state *brushState) world.Block {
	original.Dusted, original.LootTable, original.LootTableSeed = state.Dusted, state.LootTable, state.LootTableSeed
	original.Item, original.HitDirection, original.HasHitDirection = state.Item, state.HitDirection, state.HasHitDirection
	original.brushCount, original.brushCountResetsAtTick, original.coolDownEndsAtTick = state.BrushCount, state.BrushCountResetsAtTick, state.CooldownUntilTick
	return original
}
func applyGravelBrushState(original SuspiciousGravel, state *brushState) world.Block {
	original.Dusted, original.LootTable, original.LootTableSeed = state.Dusted, state.LootTable, state.LootTableSeed
	original.Item, original.HitDirection, original.HasHitDirection = state.Item, state.HitDirection, state.HasHitDirection
	original.brushCount, original.brushCountResetsAtTick, original.coolDownEndsAtTick = state.BrushCount, state.BrushCountResetsAtTick, state.CooldownUntilTick
	return original
}

func brushSuspicious(pos cube.Pos, face cube.Face, tx *world.Tx, user item.User, brush item.Stack, state *brushState, replace func(*brushState) world.Block) (bool, error) {
	current := tx.World().CurrentTick()
	if !state.HasHitDirection {
		state.HitDirection, state.HasHitDirection = face, true
	}
	state.BrushCountResetsAtTick = current + 40
	if current < state.CooldownUntilTick {
		tx.SetBlock(pos, replace(state), nil)
		return false, nil
	}
	state.CooldownUntilTick = current + 10
	if state.LootTable != "" {
		sequence, err := loot.RandomSequence(state.LootTable)
		if err != nil {
			return false, err
		}
		stream := tx.LootRandomSequence(sequence, state.LootTableSeed)
		luck := 0.0
		if l, ok := user.(interface{ Luck() float64 }); ok {
			luck = l.Luck()
		}
		generated, err := loot.Generate(state.LootTable, state.LootTableSeed, loot.Context{
			Random: stream, Origin: pos.Vec3Centre(), Tool: brush.Item(), ToolStack: brush,
			Luck: luck, ThisEntity: user,
		})
		if err != nil {
			return false, err
		}
		if len(generated) > 0 {
			state.Item = generated[0]
		}
		state.LootTable = ""
		state.LootTableSeed = 0
	}
	previous := brushCompletionState(state.BrushCount)
	state.BrushCount++
	if state.BrushCount >= 10 {
		if !state.Item.Empty() {
			split := 10 + rand.IntN(21)
			if split > state.Item.Count() {
				split = state.Item.Count()
			}
			dropPos := pos.Side(state.HitDirection)
			spawnPos := mgl64.Vec3{float64(dropPos.X()) + 0.5, float64(dropPos.Y()) + 0.625, float64(dropPos.Z()) + 0.5}
			create := tx.World().EntityRegistry().Config().Item
			tx.AddEntity(create(world.EntitySpawnOpts{Position: spawnPos}, state.Item.Grow(-state.Item.Count()+split)))
		}
		currentBlock := tx.Block(pos)
		name, _ := currentBlock.EncodeBlock()
		turnsInto := world.Block(Sand{})
		soundName := "item.brush.brushing.sand.complete"
		if name == "minecraft:suspicious_gravel" {
			turnsInto, soundName = Gravel{}, "item.brush.brushing.gravel.complete"
		}
		tx.SetBlock(pos, turnsInto, nil)
		tx.PlaySound(pos.Vec3Centre(), sound.Custom{Name: soundName, Volume: 1, Pitch: 1})
		return true, nil
	}
	if completion := brushCompletionState(state.BrushCount); completion != previous {
		state.Dusted = completion
	}
	tx.SetBlock(pos, replace(state), nil)
	tx.ScheduleBlockUpdate(pos, tx.Block(pos), 2*time.Second/20)
	return false, nil
}



func brushCompletionState(count uint8) uint8 {
	if count == 0 {
		return 0
	}
	if count < 3 {
		return 1
	}
	if count < 6 {
		return 2
	}
	return 3
}

func (s SuspiciousSand) tick(pos cube.Pos, tx *world.Tx) {
	current := tx.World().CurrentTick()
	if s.brushCount != 0 && current >= s.brushCountResetsAtTick {
		previous := brushCompletionState(s.brushCount)
		s.brushCount -= min(s.brushCount, 2)
		s.brushCountResetsAtTick = current + 4
		if progress := brushCompletionState(s.brushCount); progress != previous {
			s.Dusted = progress
		}
	}
	if s.brushCount == 0 {
		s.HasHitDirection = false
		s.brushCountResetsAtTick, s.coolDownEndsAtTick = 0, 0
	}
	tx.SetBlock(pos, s, nil)
	if s.brushCount != 0 {
		tx.ScheduleBlockUpdate(pos, s, 2*time.Second/20)
	}
	s.fall(s, pos, tx)
}
func (s SuspiciousGravel) tick(pos cube.Pos, tx *world.Tx) {
	current := tx.World().CurrentTick()
	if s.brushCount != 0 && current >= s.brushCountResetsAtTick {
		previous := brushCompletionState(s.brushCount)
		s.brushCount -= min(s.brushCount, 2)
		s.brushCountResetsAtTick = current + 4
		if progress := brushCompletionState(s.brushCount); progress != previous {
			s.Dusted = progress
		}
	}
	if s.brushCount == 0 {
		s.HasHitDirection = false
		s.brushCountResetsAtTick, s.coolDownEndsAtTick = 0, 0
	}
	tx.SetBlock(pos, s, nil)
	if s.brushCount != 0 {
		tx.ScheduleBlockUpdate(pos, s, 2*time.Second/20)
	}
	s.fall(s, pos, tx)
}

func allSuspiciousBlocks() []world.Block {
	blocks := make([]world.Block, 0, 16)
	for progress := uint8(0); progress <= 3; progress++ {
		for hanging := range 2 {
			blocks = append(blocks, SuspiciousSand{Dusted: progress, Hanging: hanging == 1}, SuspiciousGravel{Dusted: progress, Hanging: hanging == 1})
		}
	}
	return blocks
}

