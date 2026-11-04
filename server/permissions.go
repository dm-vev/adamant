package server

import (
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/google/uuid"
)

// DefaultOperatorLevel is the level of an operator whose entry of ops.json
// sets none, as Java's op-permission-level: cmd.LevelOwner, every command.
const DefaultOperatorLevel = cmd.LevelOwner

// DefaultCommandBlockLevel is the level command blocks run commands at,
// as Java fixes it: cmd.LevelGameMaster.
const DefaultCommandBlockLevel = cmd.LevelGameMaster

// PlayerLevel returns the level of the player online of the UUID passed:
// the level of an operator, by its account on a server keyed by XUID and
// by its name on one keyed by name, or cmd.LevelAll. A UUID of no player
// online is cmd.LevelAll. It implements cmd.Permissions, which
// builtin.Register installs.
func (srv *Server) PlayerLevel(id uuid.UUID) cmd.Level {
	if srv == nil {
		return cmd.LevelAll
	}
	p, ok := srv.players.player(id)
	if !ok {
		return cmd.LevelAll
	}
	return srv.levelOf(p.id)
}

// levelOf returns the level of the player of the identity passed.
func (srv *Server) levelOf(id client.Identity) cmd.Level {
	if srv.operatorOf(id) {
		return DefaultOperatorLevel
	}
	return cmd.LevelAll
}

// CommandLevel returns the level a runnable of the command of the name
// passed needs: the level it declares. It implements cmd.Permissions.
func (srv *Server) CommandLevel(_ string, declared cmd.Level) cmd.Level {
	return declared
}

// CommandBlockLevel returns the level command blocks and command-block
// minecarts run commands at. It implements cmd.Permissions.
func (srv *Server) CommandBlockLevel() cmd.Level {
	return DefaultCommandBlockLevel
}

var _ cmd.Permissions = (*Server)(nil)
