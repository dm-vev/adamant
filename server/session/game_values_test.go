package session

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// gameValue pairs a value the game owns with the protocol's value it must stay
// equal to.
type gameValue struct {
	name       string
	game, want int64
}

// checkGameValues fails the test for every game-owned value that drifted from
// the value of the protocol.
func checkGameValues(t *testing.T, values []gameValue) {
	t.Helper()
	for _, v := range values {
		if v.game != v.want {
			t.Errorf("%s = %d, protocol has %d", v.name, v.game, v.want)
		}
	}
}

// TestStructureBlockValuesMatchProtocol checks that the world-format values of
// structure blocks the game owns equal those of the Bedrock protocol, which
// sends the same values in StructureBlockUpdate.
func TestStructureBlockValuesMatchProtocol(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"StructureBlockData", int64(block.StructureBlockData), v2193.StructureBlockData},
		{"StructureBlockSave", int64(block.StructureBlockSave), v2193.StructureBlockSave},
		{"StructureBlockLoad", int64(block.StructureBlockLoad), v2193.StructureBlockLoad},
		{"StructureBlockCorner", int64(block.StructureBlockCorner), v2193.StructureBlockCorner},
		{"StructureBlockInvalid", int64(block.StructureBlockInvalid), v2193.StructureBlockInvalid},
		{"StructureBlockExport", int64(block.StructureBlockExport), v2193.StructureBlockExport},
		{"StructureBlockRotationNone", int64(block.StructureBlockRotationNone), v2193.StructureRotationNone},
		{"StructureBlockRotation90", int64(block.StructureBlockRotation90), v2193.StructureRotationRotate90},
		{"StructureBlockRotation180", int64(block.StructureBlockRotation180), v2193.StructureRotationRotate180},
		{"StructureBlockRotation270", int64(block.StructureBlockRotation270), v2193.StructureRotationRotate270},
		{"StructureBlockMirrorNone", int64(block.StructureBlockMirrorNone), v2193.StructureMirrorNone},
		{"StructureBlockMirrorX", int64(block.StructureBlockMirrorX), v2193.StructureMirrorXAxis},
		{"StructureBlockMirrorZ", int64(block.StructureBlockMirrorZ), v2193.StructureMirrorZAxis},
		{"StructureBlockMirrorBoth", int64(block.StructureBlockMirrorBoth), v2193.StructureMirrorBothAxes},
		{"StructureBlockAnimationNone", int64(block.StructureBlockAnimationNone), v2193.AnimationModeNone},
		{"StructureBlockAnimationLayers", int64(block.StructureBlockAnimationLayers), v2193.AnimationModeLayers},
		{"StructureBlockAnimationBlocks", int64(block.StructureBlockAnimationBlocks), v2193.AnimationModeBlocks},
	})
}

// TestCloudParticleValuesMatchProtocol checks that the world-format ParticleId
// of the particles of area effect clouds equals the particle type the protocol
// sends for them.
func TestCloudParticleValuesMatchProtocol(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"CloudParticleDragonBreath", int64(entity.CloudParticleDragonBreath), v2193.ParticleTypeDragonBreath},
	})
}
