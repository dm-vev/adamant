package session

import (
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

// paramOptionFlags pairs each option of a command parameter with the flag
// AvailableCommands sends for it.
var paramOptionFlags = [...]struct {
	option cmd.ParamOption
	flag   byte
}{
	{cmd.ParamOptionCollapseEnum, v2193.ParamOptionCollapseEnum},
	{cmd.ParamOptionHasSemanticConstraint, v2193.ParamOptionHasSemanticConstraint},
	{cmd.ParamOptionAsChainedCommand, v2193.ParamOptionAsChainedCommand},
}

// paramOptions returns the flags AvailableCommands sends for the options of a
// command parameter.
func paramOptions(options cmd.ParamOption) byte {
	var flags byte
	for _, f := range paramOptionFlags {
		if options&f.option != 0 {
			flags |= f.flag
		}
	}
	return flags
}

// enumConstraintRequiresAllowAliases is the ID of BDS's
// SemanticConstraint::RequiresAllowAliases in 1.26.51, which the protocol
// has no constant of.
const enumConstraintRequiresAllowAliases = 3

// enumConstraints returns the IDs AvailableCommands sends for the
// constraints of an enum option.
func enumConstraints(constraints []cmd.EnumConstraint) []byte {
	if len(constraints) == 0 {
		return nil
	}
	ids := make([]byte, 0, len(constraints))
	for _, c := range constraints {
		switch c {
		case cmd.EnumConstraintCheatsEnabled:
			ids = append(ids, v2193.CommandEnumConstraintCheatsEnabled)
		case cmd.EnumConstraintOperatorPermissions:
			ids = append(ids, v2193.CommandEnumConstraintOperatorPermissions)
		case cmd.EnumConstraintHostPermissions:
			ids = append(ids, v2193.CommandEnumConstraintHostPermissions)
		case cmd.EnumConstraintAllowAliases:
			ids = append(ids, enumConstraintRequiresAllowAliases)
		}
	}
	return ids
}
