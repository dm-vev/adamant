package session

import (
	"fmt"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world/mcdb/leveldat"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
		{"StructureBlockData", int64(block.StructureBlockData), mc.StructureBlockData},
		{"StructureBlockSave", int64(block.StructureBlockSave), mc.StructureBlockSave},
		{"StructureBlockLoad", int64(block.StructureBlockLoad), mc.StructureBlockLoad},
		{"StructureBlockCorner", int64(block.StructureBlockCorner), mc.StructureBlockCorner},
		{"StructureBlockInvalid", int64(block.StructureBlockInvalid), mc.StructureBlockInvalid},
		{"StructureBlockExport", int64(block.StructureBlockExport), mc.StructureBlockExport},
		{"StructureBlockRotationNone", int64(block.StructureBlockRotationNone), mc.StructureRotationNone},
		{"StructureBlockRotation90", int64(block.StructureBlockRotation90), mc.StructureRotationRotate90},
		{"StructureBlockRotation180", int64(block.StructureBlockRotation180), mc.StructureRotationRotate180},
		{"StructureBlockRotation270", int64(block.StructureBlockRotation270), mc.StructureRotationRotate270},
		{"StructureBlockMirrorNone", int64(block.StructureBlockMirrorNone), mc.StructureMirrorNone},
		{"StructureBlockMirrorX", int64(block.StructureBlockMirrorX), mc.StructureMirrorXAxis},
		{"StructureBlockMirrorZ", int64(block.StructureBlockMirrorZ), mc.StructureMirrorZAxis},
		{"StructureBlockMirrorBoth", int64(block.StructureBlockMirrorBoth), mc.StructureMirrorBothAxes},
		{"StructureBlockAnimationNone", int64(block.StructureBlockAnimationNone), mc.AnimationModeNone},
		{"StructureBlockAnimationLayers", int64(block.StructureBlockAnimationLayers), mc.AnimationModeLayers},
		{"StructureBlockAnimationBlocks", int64(block.StructureBlockAnimationBlocks), mc.AnimationModeBlocks},
	})
}

// TestCloudParticleValuesMatchProtocol checks that the world-format ParticleId
// of the particles of area effect clouds equals the particle type the protocol
// sends for them.
func TestCloudParticleValuesMatchProtocol(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"CloudParticleDragonBreath", int64(entity.CloudParticleDragonBreath), mc.ParticleTypeDragonBreath},
	})
}

// TestParamOptionValuesMatchProtocol checks that the options of command
// parameters the game owns keep the values of the flags the protocol sends for
// them, and that each maps to its own flag.
func TestParamOptionValuesMatchProtocol(t *testing.T) {
	checkGameValues(t, []gameValue{
		{"ParamOptionCollapseEnum", int64(cmd.ParamOptionCollapseEnum), mc.ParamOptionCollapseEnum},
		{"ParamOptionHasSemanticConstraint", int64(cmd.ParamOptionHasSemanticConstraint), mc.ParamOptionHasSemanticConstraint},
		{"ParamOptionAsChainedCommand", int64(cmd.ParamOptionAsChainedCommand), mc.ParamOptionAsChainedCommand},
	})
	all := cmd.ParamOptionCollapseEnum | cmd.ParamOptionHasSemanticConstraint | cmd.ParamOptionAsChainedCommand
	checkGameValues(t, []gameValue{
		{"paramOptions(0)", int64(paramOptions(0)), 0},
		{"paramOptions(all)", int64(paramOptions(all)), mc.ParamOptionCollapseEnum | mc.ParamOptionHasSemanticConstraint | mc.ParamOptionAsChainedCommand},
	})
	for _, f := range paramOptionFlags {
		checkGameValues(t, []gameValue{{fmt.Sprintf("paramOptions(%d)", f.option), int64(paramOptions(f.option)), int64(f.flag)}})
	}
}

// TestVersionsMatchProtocol checks that the release the game implements is the
// release of the protocol the session speaks, and that the NetworkVersion that
// level.dat stores is that protocol's number.
func TestVersionsMatchProtocol(t *testing.T) {
	if version.Minecraft != mc.CurrentVersion {
		t.Errorf("version.Minecraft = %q, protocol has %q", version.Minecraft, mc.CurrentVersion)
	}
	if version.Minecraft != mc.Version {
		t.Errorf("version.Minecraft = %q, protocol has Version %q", version.Minecraft, mc.Version)
	}
	checkGameValues(t, []gameValue{
		{"leveldat.NetworkVersion", leveldat.NetworkVersion, mc.CurrentProtocol},
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
		{"EnumConstraintCheatsEnabled", int64(ids[0]), mc.CommandEnumConstraintCheatsEnabled},
		{"EnumConstraintOperatorPermissions", int64(ids[1]), mc.CommandEnumConstraintOperatorPermissions},
		{"EnumConstraintHostPermissions", int64(ids[2]), mc.CommandEnumConstraintHostPermissions},
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
