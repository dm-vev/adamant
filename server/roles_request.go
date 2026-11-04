package server

import (
	"errors"
	"fmt"

	"github.com/df-mc/dragonfly/server/audit"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

// errRoleDenied and errRoleRefused are why a role asked for from the list
// of players of a client is not given: the player who asks is below
// [Permissions] RoleLevel, or the change is above its own level.
var (
	errRoleDenied  = errors.New("below RoleLevel")
	errRoleRefused = errors.New("refused")
)

// requestRole is the RequestRole of the Game of every client: it gives the
// player online of target the role the player online of from asks for it
// from the list of players of its client, off the goroutine of the client,
// whose world never waits for the lists.
func (srv *Server) requestRole(from, target uuid.UUID, r client.RoleRequest) {
	go srv.applyRoleRequest(from, target, r)
}

// applyRoleRequest gives the role, if the player who asks may give it, and
// records the request in the audit log. A role not given is shown again
// as it holds to every client that sees the player.
func (srv *Server) applyRoleRequest(from, target uuid.UUID, r client.RoleRequest) {
	by, ok := srv.players.player(from)
	if !ok {
		return
	}
	to, ok := srv.players.player(target)
	if !ok {
		return
	}
	actor := audit.Actor{Kind: audit.KindPlayer, Name: by.id.Name, Key: srv.identityKeyOf(by.id), Level: int(srv.levelOf(by.id))}
	if by.addr != nil {
		actor.Address = hostOf(by.addr)
	}
	shown := r.Role.String()
	if r.Role == client.RoleCustom {
		shown += fmt.Sprintf(" %v", r.Abilities.Names())
	}
	rec := audit.Record{Actor: actor, Action: "role", Target: to.id.Name, Result: audit.ResultOK,
		Command: "player list: " + shown}
	err := srv.changeRoleOf(by.id, to.id, r)
	switch {
	case err == nil:
		rec.Output = []string{fmt.Sprintf("Set the role of %s to %s.", to.id.Name, shown)}
	case errors.Is(err, errRoleDenied):
		rec.Result = audit.ResultDenied
	case errors.Is(err, errRoleRefused):
		rec.Result, rec.Output = audit.ResultRefused, []string{err.Error()}
	default:
		rec.Result, rec.Output = audit.ResultError, []string{err.Error()}
	}
	srv.Audit(rec)
	if err != nil {
		srv.conf.Log.Debug("Role request not granted.", "by", by.id.Name, "target", to.id.Name, "role", shown, "err", err)
		srv.refreshPlayerOnline(to)
	}
}

// changeRoleOf gives the player of the identity to the role asked by the
// player of the identity by, as an operator of BDS gives roles from the
// list of players: by needs [Permissions] RoleLevel and a level above that
// of to. The operator role is /op's, as by may give it: from level 3 on, as
// /op, and no level above its own. Another role of an operator takes it off
// ops.json, as BDS's list of players makes it a member, visitor or custom.
func (srv *Server) changeRoleOf(by, to client.Identity, r client.RoleRequest) error {
	own, level := srv.levelOf(by), srv.levelOf(to)
	if own < srv.permissionConfig().RoleLevel {
		return errRoleDenied
	}
	if level >= own {
		return fmt.Errorf("%w: %s is at level %v, not below %v", errRoleRefused, to.Name, level, own)
	}
	key := srv.identityKeyOf(to)
	if key == "" {
		return fmt.Errorf("%s has no account the lists know", to.Name)
	}
	_, op := srv.operatorEntry(to)
	_, explicit := srv.roleEntry(to)
	if r.Role == client.RoleOperator {
		if own < srv.CommandLevel("op", cmd.LevelAdmin) || srv.OpLevel() > own {
			return fmt.Errorf("%w: level %v gives no operators of level %v", errRoleRefused, own, srv.OpLevel())
		}
		if _, err := srv.Op(key); err != nil {
			return err
		}
		if explicit {
			_, err := srv.ClearRole(key)
			return err
		}
		return nil
	}
	if op {
		if _, err := srv.Deop(key); err != nil {
			return err
		}
	}
	_, err := srv.SetRole(key, r.Role, r.Abilities)
	return err
}

// refreshPlayerOnline refreshes the permissions the player online p and
// the clients that see it are shown.
func (srv *Server) refreshPlayerOnline(p *onlinePlayer) {
	go player.Do(p.handle, func(_ *world.Tx, p *player.Player) { p.RefreshPermissions() })
}
