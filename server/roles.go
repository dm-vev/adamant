package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/google/uuid"
)

// roleFields are what an entry of permissions.json holds beside the
// player: its role, and for RoleCustom its abilities.
type roleFields struct {
	role      client.Role
	abilities client.Abilities
}

// roleList is permissions.json, Bedrock Dedicated Server's list of the
// roles of players, keyed as the other lists are: by Xbox account on a
// server keyed by XUID, by name otherwise. A player it does not hold has
// the DefaultRole of the server, or the role of an operator if it is one.
type roleList struct {
	mu      sync.RWMutex
	file    string
	players entries
}

// roleEntry is an entry of permissions.json as BDS writes it, "permission"
// and "xuid", with the name the player last had, or the name of a player
// keyed by name, and the abilities of a custom role.
type roleEntry struct {
	Permission string   `json:"permission"`
	XUID       string   `json:"xuid,omitempty"`
	Name       string   `json:"name,omitempty"`
	Abilities  []string `json:"abilities,omitempty"`
}

// loadRoles reads permissions.json at the path passed. A missing file is
// an empty list, which is not written until it changes. It returns the
// operator entries of the file apart: BDS writes its operators there,
// which this server keeps in ops.json.
func loadRoles(path string) (*roleList, []entry, error) {
	r := &roleList{file: path}
	contents, err := os.ReadFile(path)
	if errNotExist(err) {
		return r, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("read permissions: %w", err)
	}
	var data []roleEntry
	if err := json.Unmarshal(contents, &data); err != nil {
		return nil, nil, fmt.Errorf("decode permissions: %w", err)
	}
	r.players = newEntries(len(data))
	var ops []entry
	for _, e := range data {
		en, err := e.entry()
		if err != nil {
			return nil, nil, fmt.Errorf("decode permissions: %w", err)
		}
		if en.role.role == client.RoleOperator {
			ops = append(ops, en)
			continue
		}
		r.players.put(en)
	}
	return r, ops, nil
}

// entry returns the entry of the player of e and its role.
func (e roleEntry) entry() (entry, error) {
	role, err := client.ParseRole(e.Permission)
	if err != nil {
		return entry{}, err
	}
	var en entry
	if e.XUID != "" {
		en, err = xuidEntry(e.XUID, e.Name)
	} else {
		en, err = nameEntry(e.Name)
	}
	if err != nil {
		return entry{}, fmt.Errorf("%s entry %q: %w", role, e.Name, err)
	}
	en.role.role = role
	if role == client.RoleCustom {
		if en.role.abilities, err = client.ParseAbilities(e.Abilities); err != nil {
			return entry{}, fmt.Errorf("custom entry %s: %w", en.key, err)
		}
	}
	return en, nil
}

// roleEntryOf returns the entry permissions.json writes of en.
func roleEntryOf(en entry) roleEntry {
	e := roleEntry{Permission: en.role.role.String(), Name: en.key.String()}
	if x, ok := en.key.XUID(); ok {
		e.XUID, e.Name = x, en.name
	}
	if en.role.role == client.RoleCustom {
		e.Abilities = en.role.abilities.Names()
		if e.Abilities == nil {
			e.Abilities = []string{}
		}
	}
	return e
}

func (r *roleList) writeLocked() error {
	data := make([]roleEntry, 0, len(r.players.order))
	for _, en := range r.players.order {
		data = append(data, roleEntryOf(en))
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode permissions: %w", err)
	}
	return writeFileAtomic(r.file, append(encoded, '\n'), "permissions")
}

// entry returns the entry of the key passed.
func (r *roleList) entry(k identity.Key) (entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.players.get(k)
}

// match returns the entry of the player of the name passed on a server
// keyed by name.
func (r *roleList) match(name string) (entry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.players.match(name)
}

// set puts en with its role on the list and writes the file, unless the
// list holds it with that role. It reports whether the list changed; it is
// left as it was if the write fails.
func (r *roleList) set(en entry) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.players.get(en.key); ok && old.role == en.role {
		return false, nil
	}
	before := r.players.clone()
	r.players.put(en)
	if err := r.writeLocked(); err != nil {
		r.players = before
		return false, err
	}
	return true, nil
}

// remove takes the entry of the key passed off the list and writes the
// file. It reports whether the list held it.
func (r *roleList) remove(k identity.Key) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.players.has(k) {
		return false, nil
	}
	before := r.players.clone()
	r.players.remove(k)
	if err := r.writeLocked(); err != nil {
		r.players = before
		return false, err
	}
	return true, nil
}

// view runs f with the entries of the list, which f must not change.
func (r *roleList) view(f func(e *entries)) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f(&r.players)
}

// bind binds the pending entry of the name passed to the account x, which
// joined with it, and writes the file.
func (r *roleList) bind(x identity.Key, name string) (entry, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	pending, ok := r.players.bind(x, name)
	if !ok {
		return entry{}, false, nil
	}
	return pending, true, r.writeLocked()
}

// update runs f with the entries of the list, and writes the file if f
// reports it changed them.
func (r *roleList) update(f func(e *entries) bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !f(&r.players) {
		return nil
	}
	return r.writeLocked()
}

// reload reads the file again. Operator entries are left out, as at
// start, and a file that cannot be read leaves the list as it was.
func (r *roleList) reload() error {
	next, _, err := loadRoles(r.file)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.players = next.players
	return nil
}

// path returns the path of the file of the list.
func (r *roleList) path() string { return r.file }

// moveOperatorRoles moves the operator entries BDS writes in
// permissions.json into ops.json, with no level of their own, as this
// server keeps its operators there, and writes permissions.json without
// them, after a copy of it as permissions.json.before-roles.
func moveOperatorRoles(roles *roleList, ops *operatorsList, moved []entry, log *slog.Logger) error {
	if len(moved) == 0 {
		return nil
	}
	backup := roles.file + ".before-roles"
	if _, err := os.Stat(backup); errNotExist(err) {
		data, err := os.ReadFile(roles.file)
		if err != nil {
			return fmt.Errorf("copy permissions: %w", err)
		}
		if err := writeFileAtomic(backup, data, "permissions copy"); err != nil {
			return err
		}
	}
	for _, en := range moved {
		en.role = roleFields{}
		if _, err := ops.add(en); err != nil {
			return err
		}
	}
	if err := roles.update(func(*entries) bool { return true }); err != nil {
		return err
	}
	log.Info(fmt.Sprintf("Moved %d operators of permissions.json to ops.json; the file before is permissions.json.before-roles.", len(moved)))
	return nil
}

// errOperatorRole is the error of setting the role of an operator with
// SetRole: Op makes operators.
var errOperatorRole = errors.New("the operator role is given by /op: name visitor, member or custom")

// PlayerPermissions returns what the player online of the UUID passed may
// do: its level, and its role, from permissions.json, or that of an
// operator if it is one, or the DefaultRole of the server, with the
// abilities of the role. A UUID of no player online has the permissions of
// a player who is no operator and has no entry.
func (srv *Server) PlayerPermissions(id uuid.UUID) client.Permissions {
	if p, ok := srv.players.player(id); ok {
		return srv.permissionsOf(p.id)
	}
	return srv.permissionsOf(client.Identity{})
}

// permissionsOf returns the permissions of the player of the identity
// passed.
func (srv *Server) permissionsOf(id client.Identity) client.Permissions {
	_, op := srv.operatorEntry(id)
	perms := client.Permissions{Level: uint8(srv.levelOf(id)), Operator: op}
	en, explicit := srv.roleEntry(id)
	switch {
	case explicit:
		perms.Role, perms.Abilities = en.role.role, en.role.abilities
	case op:
		perms.Role = client.RoleOperator
	default:
		perms.Role = srv.permissionConfig().DefaultRole
	}
	switch perms.Role {
	case client.RoleVisitor:
		perms.Abilities = 0
	case client.RoleMember:
		perms.Abilities = client.AbilitiesOfMembers
	case client.RoleOperator:
		perms.Abilities = client.AbilitiesOfMembers
		if cmd.Level(perms.Level) >= cmd.LevelGameMaster {
			perms.Abilities = client.AbilitiesOfOperators
		}
	}
	return perms
}

// roleEntry returns the entry of permissions.json of the player of the
// identity passed: by its account on a server keyed by XUID, by its name
// on one keyed by name.
func (srv *Server) roleEntry(id client.Identity) (entry, bool) {
	if srv.roles == nil {
		return entry{}, false
	}
	if srv.IdentityKey() == identity.XUID {
		k, ok := accountKey(id)
		if !ok {
			return entry{}, false
		}
		return srv.roles.entry(k)
	}
	return srv.roles.match(id.Name)
}

// SetRole gives the player of the name passed, or "xuid:<XUID>", as Op
// resolves it, the role passed in permissions.json: client.RoleVisitor,
// client.RoleMember, or client.RoleCustom with the abilities passed. It
// reports whether the list changed, and refreshes the permissions of the
// player online if it did. Op, not SetRole, makes operators.
func (srv *Server) SetRole(name string, role client.Role, abilities client.Abilities) (bool, error) {
	if srv.roles == nil {
		return false, errAdminUnavailable
	}
	if role == client.RoleOperator {
		return false, errOperatorRole
	}
	en, _, err := srv.addition(name)
	if err != nil {
		return false, err
	}
	en.role = roleFields{role: role}
	if role == client.RoleCustom {
		en.role.abilities = abilities
	}
	changed, err := srv.roles.set(en)
	if changed {
		srv.refreshPlayerPermissions(en)
	}
	return changed, err
}

// ClearRole takes the entry of the player of the name passed, or
// "xuid:<XUID>", as Deop finds it, off permissions.json, so that it has
// the role of an operator or the DefaultRole again. It reports whether the
// list held it.
func (srv *Server) ClearRole(name string) (bool, error) {
	if srv.roles == nil {
		return false, errAdminUnavailable
	}
	t, err := srv.ResolveEntry(Roles, name)
	if err != nil {
		return false, err
	}
	var en entry
	found := false
	srv.roles.view(func(e *entries) { en, found = e.get(t.Key) })
	if !found {
		return false, nil
	}
	removed, err := srv.roles.remove(en.key)
	if removed {
		srv.refreshPlayerPermissions(en)
	}
	return removed, err
}

// RoleEntries returns the entries of permissions.json as the commands show
// them: the player, then its role, and the abilities of a custom role.
func (srv *Server) RoleEntries() ([]string, error) {
	if srv.roles == nil {
		return nil, errAdminUnavailable
	}
	byXUID := srv.IdentityKey() == identity.XUID
	var out []string
	srv.roles.view(func(e *entries) {
		out = make([]string, 0, len(e.order))
		for _, en := range e.order {
			s := Target{Key: en.key, Name: en.name}.String()
			if _, ok := en.key.XUID(); !ok && byXUID {
				s += " (held by name)"
			}
			s += ": " + en.role.role.String()
			if en.role.role == client.RoleCustom {
				s += fmt.Sprintf(" %v", en.role.abilities.Names())
			}
			out = append(out, s)
		}
	})
	return out, nil
}
