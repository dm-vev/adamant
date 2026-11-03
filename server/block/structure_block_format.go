package block

// The values of the rotation, mirror and animationMode fields of a structure
// block. They are values of the Bedrock world format, stored as they are in the
// block entity data of structure blocks, and match the fields Rotation, Mirror
// and AnimationMode of StructureBlock.
const (
	// StructureBlockRotationNone loads a structure without rotating it.
	StructureBlockRotationNone uint8 = iota
	// StructureBlockRotation90 rotates a loaded structure clockwise by a
	// quarter turn.
	StructureBlockRotation90
	// StructureBlockRotation180 rotates a loaded structure by a half turn.
	StructureBlockRotation180
	// StructureBlockRotation270 rotates a loaded structure clockwise by three
	// quarter turns.
	StructureBlockRotation270
)

const (
	// StructureBlockMirrorNone loads a structure without mirroring it.
	StructureBlockMirrorNone uint8 = iota
	// StructureBlockMirrorX mirrors a loaded structure along the X axis.
	StructureBlockMirrorX
	// StructureBlockMirrorZ mirrors a loaded structure along the Z axis.
	StructureBlockMirrorZ
	// StructureBlockMirrorBoth mirrors a loaded structure along both the X and
	// the Z axis.
	StructureBlockMirrorBoth
)

const (
	// StructureBlockAnimationNone places a loaded structure at once.
	StructureBlockAnimationNone uint8 = iota
	// StructureBlockAnimationLayers places a loaded structure layer by layer.
	StructureBlockAnimationLayers
	// StructureBlockAnimationBlocks places a loaded structure block by block.
	StructureBlockAnimationBlocks
)
