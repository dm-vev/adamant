package block

import (
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// StructureBlockMode is the mode of a structure block, in Bedrock's order.
type StructureBlockMode uint8

const (
	// StructureBlockData marks a position for structure processors, using its
	// data field.
	StructureBlockData StructureBlockMode = iota
	// StructureBlockSave saves the blocks in its box as a structure.
	StructureBlockSave
	// StructureBlockLoad places a saved structure.
	StructureBlockLoad
	// StructureBlockCorner marks a corner of a structure.
	StructureBlockCorner
	// StructureBlockInvalid is a structure block without a mode.
	StructureBlockInvalid
	// StructureBlockExport exports a structure to a file on the client.
	StructureBlockExport
)

// String returns the Bedrock name of the mode.
func (m StructureBlockMode) String() string {
	switch m {
	case StructureBlockData:
		return "data"
	case StructureBlockSave:
		return "save"
	case StructureBlockLoad:
		return "load"
	case StructureBlockCorner:
		return "corner"
	case StructureBlockExport:
		return "export"
	default:
		return "invalid"
	}
}

// StructureBlockModes returns all structure block modes.
func StructureBlockModes() []StructureBlockMode {
	return []StructureBlockMode{StructureBlockData, StructureBlockSave, StructureBlockLoad, StructureBlockCorner, StructureBlockInvalid, StructureBlockExport}
}

var structureBlockHash = NextHash()

// StructureBlock saves the blocks in a box as a structure template and loads
// templates into the world. Only operators in creative mode can use it.
type StructureBlock struct {
	solid
	// Mode is what the structure block does.
	Mode StructureBlockMode
	// Name is the name of the structure template saved or loaded.
	Name string
	// DataField is the data a structure block in data mode passes to
	// structure processors.
	DataField string
	// Offset is the position of the lowest corner of the box of the structure
	// relative to the structure block.
	Offset cube.Pos
	// Size is the size of the box of the structure.
	Size [3]int
	// IgnoreEntities leaves out entities when saving and loading.
	IgnoreEntities bool
	// IncludePlayers is the Bedrock option to save players. Like Java, players
	// are never saved into structures.
	IncludePlayers bool
	// RemoveBlocks loads only the entities of a structure.
	RemoveBlocks bool
	// ShowBoundingBox shows the box of the structure to clients.
	ShowBoundingBox bool
	// Rotation is the clockwise rotation of a loaded structure in quarter
	// turns, and Mirror mirrors it along X (1), Z (2) or both (3).
	Rotation, Mirror uint8
	// Integrity is the percentage of blocks of a loaded structure placed.
	Integrity float32
	// Seed seeds which blocks Integrity leaves out. Zero picks a new seed for
	// each load.
	Seed int64
	// RedstoneSaveMode is where redstone saves the structure: in memory (0) or
	// in the world save (1).
	RedstoneSaveMode int32
	// AnimationMode places a loaded structure layer by layer (1) or block by
	// block (2) over AnimationSeconds.
	AnimationMode    uint8
	AnimationSeconds float32
	// Powered is whether the structure block receives redstone power.
	Powered bool
	// LastTouchedPlayerID is the unique ID of the player that last edited the
	// structure block.
	LastTouchedPlayerID int64

	// Author, Strict and ShowAir are Java settings Bedrock has no field for,
	// kept for structure blocks from Java templates. A strict structure block
	// loads structures without updating the shapes of their blocks.
	Author          string
	Strict, ShowAir bool
	// javaFields records which Java settings were present, so that they are
	// written back as they were read.
	javaFields uint8
}

const (
	structureJavaAuthor = 1 << iota
	structureJavaStrict
	structureJavaShowAir
)

// NewStructureBlock returns a structure block in a mode with the settings
// Bedrock places it with.
func NewStructureBlock(mode StructureBlockMode) StructureBlock {
	return StructureBlock{Mode: mode, Offset: cube.Pos{0, -1, 0}, Size: [3]int{5, 5, 5}, ShowBoundingBox: true, Integrity: 100, LastTouchedPlayerID: -1}
}

// EncodeItem ...
func (StructureBlock) EncodeItem() (string, int16) { return "minecraft:structure_block", 0 }

// EncodeBlock ...
func (b StructureBlock) EncodeBlock() (string, map[string]any) {
	return "minecraft:structure_block", map[string]any{"structure_block_type": b.Mode.String()}
}

// Hash ...
func (b StructureBlock) Hash() (uint64, uint64) {
	return structureBlockHash, uint64(b.Mode)
}

// UseOnBlock places a structure block in save mode, which Bedrock's editor
// opens with.
func (b StructureBlock) UseOnBlock(pos cube.Pos, face cube.Face, _ mgl64.Vec3, tx *world.Tx, user item.User, ctx *item.UseContext) bool {
	pos, _, ok := firstReplaceable(tx, pos, face, b)
	if !ok || user != nil && !canUseGameMasterBlocks(user) {
		// GameMasterBlockItem: only operators in creative mode place it.
		return false
	}
	place(tx, pos, NewStructureBlock(StructureBlockSave), user, ctx)
	return placed(ctx)
}

// Activate opens the editor of the structure block for operators in creative
// mode, as Player.canUseGameMasterBlocks.
func (b StructureBlock) Activate(_ cube.Pos, _ cube.Face, _ *world.Tx, u item.User, _ *item.UseContext) bool {
	return canUseGameMasterBlocks(u)
}

// canUseGameMasterBlocks ports Player.canUseGameMasterBlocks: operators in
// creative mode may edit structure, jigsaw and command blocks.
func canUseGameMasterBlocks(u item.User) bool {
	player, ok := u.(interface {
		Name() string
		GameMode() world.GameMode
	})
	return ok && player.GameMode().CreativeInventory() && cmd.IsOperator(player.Name())
}

// RedstonePowerUpdate ...
func (b StructureBlock) RedstonePowerUpdate(_ cube.Pos, _ *world.Tx, power int) (world.Block, bool) {
	powered := power > 0
	if powered == b.Powered {
		return b, false
	}
	b.Powered = powered
	return b, true
}

// RedstonePowerPostUpdate ports StructureBlock.trigger: when powered, a
// structure block in save mode saves its structure, one in load mode places
// it and one in corner mode forgets the structure kept in memory.
func (b StructureBlock) RedstonePowerPostUpdate(pos cube.Pos, tx *world.Tx, _, after world.Block, oldPower, newPower int) {
	updated, ok := after.(StructureBlock)
	if !ok || oldPower != 0 || newPower == 0 {
		return
	}
	switch updated.Mode {
	case StructureBlockSave:
		updated.Save(pos, tx, updated.RedstoneSaveMode == 1)
	case StructureBlockLoad:
		updated.Load(pos, tx)
	case StructureBlockCorner:
		if updated.Name != "" {
			tx.World().UnloadStructureTemplate(updated.Name)
		}
	}
}

// Save ports StructureBlockEntity.saveStructure: the blocks in the box of the
// structure block, and its entities unless ignored, are saved under its name,
// in the world save if disk is true. It reports whether the structure was
// saved.
func (b StructureBlock) Save(pos cube.Pos, tx *world.Tx, disk bool) bool {
	if b.Name == "" {
		return false
	}
	template := tx.CaptureStructure(pos.Add(b.Offset), b.Size, !b.IgnoreEntities)
	return tx.World().SaveStructureTemplate(b.Name, template, disk) == nil
}

// Load ports StructureBlockEntity.placeStructure: the structure named by the
// structure block is placed in its box, mirrored and rotated the way Bedrock
// keeps it in the box. The structure block takes the size of the structure.
// It reports whether the structure was found.
func (b StructureBlock) Load(pos cube.Pos, tx *world.Tx) bool {
	template, ok, err := tx.World().StructureTemplate(b.Name)
	if err != nil || !ok {
		return false
	}
	placement := b.Placement()
	low, _ := template.TransformedBounds(placement.Mirror, placement.Rotation)
	tx.PlaceStructure(template, pos.Add(b.Offset).Sub(low), placement)
	if b.Size != template.Size() {
		if current, ok := tx.Block(pos).(StructureBlock); ok {
			current.Size = template.Size()
			tx.SetBlock(pos, current, nil)
		}
	}
	return true
}

// Placement returns how the structure block places a structure.
func (b StructureBlock) Placement() world.StructurePlacement {
	p := world.StructurePlacement{
		Rotation:       world.StructureRotation(b.Rotation % 4),
		Integrity:      float64(min(max(b.Integrity, 0), 100)) / 100,
		Seed:           b.Seed,
		IgnoreEntities: b.IgnoreEntities,
		IgnoreBlocks:   b.RemoveBlocks,
	}
	switch b.Mirror {
	case 1:
		p.Mirror = world.StructureMirrorLeftRight
	case 2:
		p.Mirror = world.StructureMirrorFrontBack
	case 3:
		// Mirroring both axes turns the structure around.
		p.Rotation = (p.Rotation + 2) % 4
	}
	switch b.AnimationMode {
	case 1:
		p.Animation = world.StructureAnimationByLayer
	case 2:
		p.Animation = world.StructureAnimationByBlock
	}
	p.AnimationDuration = time.Duration(float64(max(b.AnimationSeconds, 0)) * float64(time.Second))
	p.KnownShape = b.Strict
	return p
}

// EncodeNBT encodes the structure block with the block entity keys of Bedrock
// Dedicated Server.
func (b StructureBlock) EncodeNBT() map[string]any {
	m := map[string]any{
		"id":                  "StructureBlock",
		"BlockEntityVersion":  int32(0),
		"data":                int32(b.Mode),
		"structureName":       b.Name,
		"dataField":           b.DataField,
		"xStructureOffset":    int32(b.Offset[0]),
		"yStructureOffset":    int32(b.Offset[1]),
		"zStructureOffset":    int32(b.Offset[2]),
		"xStructureSize":      int32(b.Size[0]),
		"yStructureSize":      int32(b.Size[1]),
		"zStructureSize":      int32(b.Size[2]),
		"ignoreEntities":      boolByte(b.IgnoreEntities),
		"includePlayers":      boolByte(b.IncludePlayers),
		"removeBlocks":        boolByte(b.RemoveBlocks),
		"showBoundingBox":     boolByte(b.ShowBoundingBox),
		"rotation":            b.Rotation,
		"mirror":              b.Mirror,
		"integrity":           b.Integrity,
		"seed":                b.Seed,
		"redstoneSaveMode":    b.RedstoneSaveMode,
		"animationMode":       b.AnimationMode,
		"animationSeconds":    b.AnimationSeconds,
		"isPowered":           boolByte(b.Powered),
		"lastTouchedPlayerId": b.LastTouchedPlayerID,
	}
	java := map[string]any{}
	if b.javaFields&structureJavaAuthor != 0 || b.Author != "" {
		java["author"] = b.Author
	}
	if b.javaFields&structureJavaStrict != 0 || b.Strict {
		java["strict"] = boolByte(b.Strict)
	}
	if b.javaFields&structureJavaShowAir != 0 || b.ShowAir {
		java["showair"] = boolByte(b.ShowAir)
	}
	if len(java) != 0 {
		m["adamant:java"] = java
	}
	return m
}

// DecodeNBT decodes the structure block from Bedrock block entity data.
func (b StructureBlock) DecodeNBT(m map[string]any) any {
	d := NewStructureBlock(b.Mode)
	if _, ok := m["data"]; ok {
		if mode := nbtconv.Int32(m, "data"); mode >= 0 && mode <= int32(StructureBlockExport) {
			d.Mode = StructureBlockMode(mode)
		}
	}
	d.Name = nbtconv.String(m, "structureName")
	d.DataField = nbtconv.String(m, "dataField")
	if _, ok := m["xStructureOffset"]; ok {
		d.Offset = cube.Pos{int(nbtconv.Int32(m, "xStructureOffset")), int(nbtconv.Int32(m, "yStructureOffset")), int(nbtconv.Int32(m, "zStructureOffset"))}
	}
	if _, ok := m["xStructureSize"]; ok {
		d.Size = [3]int{int(nbtconv.Int32(m, "xStructureSize")), int(nbtconv.Int32(m, "yStructureSize")), int(nbtconv.Int32(m, "zStructureSize"))}
	}
	d.IgnoreEntities = nbtconv.Bool(m, "ignoreEntities")
	d.IncludePlayers = nbtconv.Bool(m, "includePlayers")
	d.RemoveBlocks = nbtconv.Bool(m, "removeBlocks")
	if _, ok := m["showBoundingBox"]; ok {
		d.ShowBoundingBox = nbtconv.Bool(m, "showBoundingBox")
	}
	d.Rotation = nbtconv.Uint8(m, "rotation") % 4
	d.Mirror = nbtconv.Uint8(m, "mirror") % 4
	if _, ok := m["integrity"]; ok {
		d.Integrity = nbtconv.Float32(m, "integrity")
	}
	d.Seed = nbtconv.Int64(m, "seed")
	d.RedstoneSaveMode = nbtconv.Int32(m, "redstoneSaveMode")
	d.AnimationMode = nbtconv.Uint8(m, "animationMode")
	d.AnimationSeconds = nbtconv.Float32(m, "animationSeconds")
	d.Powered = nbtconv.Bool(m, "isPowered")
	if _, ok := m["lastTouchedPlayerId"]; ok {
		d.LastTouchedPlayerID = nbtconv.Int64(m, "lastTouchedPlayerId")
	}
	if java, ok := m["adamant:java"].(map[string]any); ok {
		if author, ok := java["author"].(string); ok {
			d.Author, d.javaFields = author, d.javaFields|structureJavaAuthor
		}
		if _, ok := java["strict"]; ok {
			d.Strict, d.javaFields = nbtconv.Bool(java, "strict"), d.javaFields|structureJavaStrict
		}
		if _, ok := java["showair"]; ok {
			d.ShowAir, d.javaFields = nbtconv.Bool(java, "showair"), d.javaFields|structureJavaShowAir
		}
	}
	return d
}
