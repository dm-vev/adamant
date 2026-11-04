package block

// StructureLimits are the largest sizes, and offsets in either direction,
// the structure editor accepts along the X, Y and Z axes.
var StructureLimits = [3]int{64, 384, 64}

// StructureBlockEdit is what a player sets in the editor of a structure
// block. Edited bounds its values to those a structure block takes.
type StructureBlockEdit struct {
	// Mode is what the structure block does.
	Mode StructureBlockMode
	// Name is the name of the structure template, and DataField the data
	// of a structure block in data mode.
	Name, DataField string
	// IncludePlayers, ShowBoundingBox, IgnoreEntities and RemoveBlocks are
	// the options of the structure block of those names.
	IncludePlayers, ShowBoundingBox, IgnoreEntities, RemoveBlocks bool
	// Size is the size of the box of the structure and Offset the position
	// of its lowest corner relative to the structure block.
	Size, Offset [3]int
	// Rotation, Mirror and AnimationMode are those of the structure block,
	// taken modulo 4, 4 and 3.
	Rotation, Mirror, AnimationMode uint8
	// Integrity is the percentage of blocks placed, bounded to 0-100.
	Integrity float32
	// Seed seeds which blocks Integrity leaves out.
	Seed int64
	// AnimationSeconds is how long a load is animated, at least 0.
	AnimationSeconds float32
	// RedstoneSaveMode is where redstone saves the structure, taken modulo
	// 2.
	RedstoneSaveMode uint8
	// LastTouchedPlayerID is the unique ID of the player that edited the
	// structure block.
	LastTouchedPlayerID int64
}

// Edited returns b with the settings of e applied: the size bounded to
// StructureLimits, the offset to StructureLimits in either direction, the
// integrity to 0-100, the duration of the animation to at least 0, and the
// modes and rotation taken modulo the number of their values.
func (b StructureBlock) Edited(e StructureBlockEdit) StructureBlock {
	b.Mode = e.Mode
	b.Name, b.DataField = e.Name, e.DataField
	b.IncludePlayers, b.ShowBoundingBox = e.IncludePlayers, e.ShowBoundingBox
	b.IgnoreEntities, b.RemoveBlocks = e.IgnoreEntities, e.RemoveBlocks
	for axis, limit := range StructureLimits {
		b.Size[axis] = min(max(e.Size[axis], 0), limit)
		b.Offset[axis] = min(max(e.Offset[axis], -limit), limit)
	}
	b.Rotation, b.Mirror = e.Rotation%4, e.Mirror%4
	b.Integrity = min(max(e.Integrity, 0), 100)
	b.Seed = e.Seed
	b.AnimationMode, b.AnimationSeconds = e.AnimationMode%3, max(e.AnimationSeconds, 0)
	b.RedstoneSaveMode = int32(e.RedstoneSaveMode % 2)
	b.LastTouchedPlayerID = e.LastTouchedPlayerID
	return b
}
