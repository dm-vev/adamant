package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
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

// TestParamOptionValuesMatchProtocol checks that the options of command
// parameters the game owns keep the values of the flags the protocol sends for
// them, and that each maps to its own flag.
func TestParamOptionValuesMatchProtocol(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"ParamOptionCollapseEnum", int64(cmd.ParamOptionCollapseEnum), v2193.ParamOptionCollapseEnum},
		{"ParamOptionHasSemanticConstraint", int64(cmd.ParamOptionHasSemanticConstraint), v2193.ParamOptionHasSemanticConstraint},
		{"ParamOptionAsChainedCommand", int64(cmd.ParamOptionAsChainedCommand), v2193.ParamOptionAsChainedCommand},
	})
	all := cmd.ParamOptionCollapseEnum | cmd.ParamOptionHasSemanticConstraint | cmd.ParamOptionAsChainedCommand
	checkGameValues(t, []gameValue{
		{"paramOptions(0)", int64(paramOptions(0)), 0},
		{"paramOptions(all)", int64(paramOptions(all)), v2193.ParamOptionCollapseEnum | v2193.ParamOptionHasSemanticConstraint | v2193.ParamOptionAsChainedCommand},
	})
	for _, f := range paramOptionFlags {
		checkGameValues(t, []gameValue{{fmt.Sprintf("paramOptions(%d)", f.option), int64(paramOptions(f.option)), int64(f.flag)}})
	}
}

// TestVersionsMatchProtocol checks that the release the game implements is the
// release of the protocol the session speaks, and that the NetworkVersion that
// level.dat stores is that protocol's number.
func TestVersionsMatchProtocol(t *testing.T) {
	if version.Minecraft != v2193.CurrentVersion {
		t.Errorf("version.Minecraft = %q, protocol has %q", version.Minecraft, v2193.CurrentVersion)
	}
	if version.Minecraft != v2193.Version {
		t.Errorf("version.Minecraft = %q, protocol has Version %q", version.Minecraft, v2193.Version)
	}
	checkGameValues(t, []gameValue{
		{"leveldat.NetworkVersion", leveldat.NetworkVersion, v2193.CurrentProtocol},
	})
}

// TestEnumConstraintValuesMatchProtocol checks that the constraints of enum
// options the game owns keep the IDs AvailableCommands sends for them: the
// protocol's constants, and 3 for RequiresAllowAliases, which BDS 1.26.51
// sends of full registry names (testdata/bedrock_1_26_51_registry_arguments.json
// of the builtin commands).
func TestEnumConstraintValuesMatchProtocol(t *testing.T) {
	all := []cmd.EnumConstraint{cmd.EnumConstraintCheatsEnabled, cmd.EnumConstraintOperatorPermissions, cmd.EnumConstraintHostPermissions, cmd.EnumConstraintAllowAliases}
	ids := enumConstraints(all)
	if len(ids) != len(all) {
		t.Fatalf("enumConstraints(%v) = %v, want one ID each", all, ids)
	}
	checkGameValues(t, []gameValue{
		{"EnumConstraintCheatsEnabled", int64(ids[0]), v2193.CommandEnumConstraintCheatsEnabled},
		{"EnumConstraintOperatorPermissions", int64(ids[1]), v2193.CommandEnumConstraintOperatorPermissions},
		{"EnumConstraintHostPermissions", int64(ids[2]), v2193.CommandEnumConstraintHostPermissions},
		{"EnumConstraintAllowAliases", int64(ids[3]), 3},
	})
	for i, c := range all {
		if int(c) != int(ids[i]) {
			t.Errorf("%d: the game's value %d differs from the ID %d", i, c, ids[i])
		}
	}
	if got := enumConstraints(nil); got != nil {
		t.Errorf("enumConstraints(nil) = %v, want nil", got)
	}
}
