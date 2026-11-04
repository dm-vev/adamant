package server

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
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

// levelOf returns the level of the player of the identity passed: that of
// its entry of ops.json, by its account on a server keyed by XUID and by
// its name on one keyed by name, or cmd.LevelAll if it is no operator.
func (srv *Server) levelOf(id client.Identity) cmd.Level {
	if en, ok := srv.operatorEntry(id); ok {
		return srv.entryLevel(en)
	}
	return cmd.LevelAll
}

// operatorEntry returns the entry of ops.json of the player of the
// identity passed, as operatorOf finds it.
func (srv *Server) operatorEntry(id client.Identity) (entry, bool) {
	if srv.operators == nil {
		return entry{}, false
	}
	if srv.IdentityKey() == identity.XUID {
		k, ok := accountKey(id)
		if !ok {
			return entry{}, false
		}
		return srv.operators.entry(k)
	}
	return srv.operators.match(id.Name)
}

// entryLevel returns the level of an operator: its own, or OpLevel.
func (srv *Server) entryLevel(en entry) cmd.Level {
	if en.op.hasLevel {
		return en.op.level
	}
	return srv.OpLevel()
}

// OpLevel returns the level /op gives, and that of the operators whose
// entry of ops.json sets none.
func (srv *Server) OpLevel() cmd.Level {
	return DefaultOperatorLevel
}

// OperatorLevel returns the level of the operator of the name passed, or
// "xuid:<XUID>", as Deop finds it, and reports whether it is an operator.
func (srv *Server) OperatorLevel(name string) (cmd.Level, bool) {
	if srv.operators == nil {
		return 0, false
	}
	t, err := srv.ResolveEntry(Operators, name)
	if err != nil {
		return 0, false
	}
	en, ok := srv.operators.entry(t.Key)
	if !ok {
		return 0, false
	}
	return srv.entryLevel(en), true
}

// SetOperatorLevel makes the player of the name passed, or "xuid:<XUID>",
// as Op resolves it, an operator of the level passed, written in its entry
// of ops.json: an operator of another level, or of none of its own, has
// its level changed. It reports whether the list changed, and refreshes
// the permissions of the player online if it did.
func (srv *Server) SetOperatorLevel(name string, level cmd.Level) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	if level > cmd.MaxLevel {
		return false, fmt.Errorf("level %d: want 0 to %d", level, cmd.MaxLevel)
	}
	en, _, err := srv.addition(name)
	if err != nil {
		return false, err
	}
	if old, ok := srv.operators.entry(en.key); ok {
		en.op = old.op
		if old.name != "" && en.name == "" {
			en.name = old.name
		}
	}
	en.op.level, en.op.hasLevel = level, true
	changed, err := srv.operators.set(en)
	if changed {
		srv.refreshPlayerPermissions(en)
	}
	return changed, err
}

// ReloadPermissions reads ops.json again, as /permission reload does, and
// refreshes the permissions of every player online. A file that cannot be
// read leaves the list as it was.
func (srv *Server) ReloadPermissions() error {
	if srv.operators == nil {
		return errAdminUnavailable
	}
	if err := srv.operators.reload(); err != nil {
		return err
	}
	srv.refreshAllPermissions()
	return nil
}

// refreshAllPermissions refreshes the permissions every player online is
// shown, each on its owner, from a goroutine of its own.
func (srv *Server) refreshAllPermissions() {
	for _, h := range srv.players.handles() {
		go player.Do(h, func(_ *world.Tx, p *player.Player) { p.RefreshPermissions() })
	}
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
