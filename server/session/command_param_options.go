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
