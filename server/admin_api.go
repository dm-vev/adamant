package server

import (
	"errors"
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
)

var errAdminUnavailable = errors.New("admin persistence unavailable")

// IdentityKey returns what the server keys its players by: identity.XUID or
// identity.Name, never identity.Auto, which a Server keys by name.
func (srv *Server) IdentityKey() identity.Mode {
	if srv.conf.IdentityKey == identity.XUID {
		return identity.XUID
	}
	return identity.Name
}

// IsOperator reports whether the player of the name passed is an
// operator. On a server keyed by XUID, the name resolves as ResolvePlayer
// resolves it, without adding anything: the account of the player online,
// or the account that last joined with the name, is checked; an entry still
// held by the name, which no account joined with yet, is no one's.
func (srv *Server) IsOperator(name string) bool {
	if srv.operators == nil {
		return false
	}
	if srv.IdentityKey() == identity.XUID {
		t, err := srv.ResolvePlayer(name)
		return err == nil && !t.Pending && srv.operators.hasKey(t.Key)
	}
	return srv.operators.isOperator(name)
}

// operatorOf reports whether the player of the identity passed is an
// operator: by its account on a server keyed by XUID, by its name on one
// keyed by name.
func (srv *Server) operatorOf(id client.Identity) bool {
	if srv.operators == nil {
		return false
	}
	if srv.IdentityKey() == identity.XUID {
		k, ok := accountKey(id)
		return ok && srv.operators.hasKey(k)
	}
	return srv.operators.isOperator(id.Name)
}

// addition returns the entry a list gains for a player an admin names, as
// ResolvePlayer resolves it.
func (srv *Server) addition(name string) (entry, Target, error) {
	t, err := srv.ResolvePlayer(name)
	if err != nil {
		return entry{}, t, err
	}
	en := entry{key: t.Key, name: t.Name}
	if n, ok := t.Key.Name(); ok {
		en.name = n
	}
	return en, t, nil
}

// removal returns the key of the entry of a list an admin names for
// removal, as ResolveEntry resolves it.
func (srv *Server) removal(list List, name string) (identity.Key, error) {
	t, err := srv.ResolveEntry(list, name)
	return t.Key, err
}

func (srv *Server) Op(name string) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	en, _, err := srv.addition(name)
	if err != nil {
		return false, err
	}
	added, err := srv.operators.add(en)
	if added {
		srv.refreshPlayerPermissions(en)
	}
	return added, err
}
func (srv *Server) Deop(name string) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	k, err := srv.removal(Operators, name)
	if errors.Is(err, identity.ErrEmptyName) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	var en entry
	found := false
	srv.operators.view(func(e *entries) { en, found = e.get(k) })
	if !found {
		return false, nil
	}
	removed, err := srv.operators.remove(en)
	if removed {
		srv.refreshPlayerPermissions(en)
	}
	return removed, err
}

// refreshPlayerPermissions runs on the player's current owner after releasing
// the operator lock. Read the latest status there so queued changes cannot
// restore stale permissions after a subsequent op/deop or a world transfer.
// The player is that of the account of the entry on a server keyed by XUID,
// and of its name otherwise.
func (srv *Server) refreshPlayerPermissions(en entry) {
	if handle, ok := srv.playerOf(en); ok {
		go player.Do(handle, func(_ *world.Tx, p *player.Player) { p.RefreshPermissions() })
	}
}
func (srv *Server) OperatorEntries() ([]string, error) {
	if srv.operators == nil {
		return nil, errAdminUnavailable
	}
	return srv.entryStrings(srv.operators.view), nil
}
func (srv *Server) BanPlayer(name, reason string) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	en, _, err := srv.addition(name)
	if err != nil {
		return false, err
	}
	added, err := srv.bans.add(en, reason)
	if added {
		if h, ok := srv.playerOf(en); ok {
			go player.Do(h, func(_ *world.Tx, p *player.Player) { p.Disconnect("You are banned from this server.") })
		}
	}
	return added, err
}

// playerOf returns the handle of the player online an entry is of: the
// player of its account on a server keyed by XUID, the player of its name,
// in any case, otherwise.
func (srv *Server) playerOf(en entry) (*world.EntityHandle, bool) {
	if x, ok := en.key.XUID(); ok && srv.IdentityKey() == identity.XUID {
		return srv.players.byXUID(x)
	}
	if p, ok := srv.players.named(en.name); ok {
		return p.handle, true
	}
	return nil, false
}
func (srv *Server) PardonPlayer(name string) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	k, err := srv.removal(Banned, name)
	if errors.Is(err, identity.ErrEmptyName) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return srv.bans.remove(k)
}
func (srv *Server) BanIP(ip, reason string) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	added, err := srv.bans.addIP(ip, reason)
	if added {
		target := normalizeIP(ip)
		go func() {
			for p := range srv.Players(nil) {
				if p.Addr() != nil && normalizeIP(p.Addr().String()) == target {
					p.Disconnect("Your IP address is banned from this server.")
				}
			}
		}()
	}
	return added, err
}
func (srv *Server) PardonIP(ip string) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	return srv.bans.removeIP(ip)
}
func (srv *Server) BanEntries(ip bool) ([]string, error) {
	if srv.bans == nil {
		return nil, errAdminUnavailable
	}
	if ip {
		return srv.bans.entries(true), nil
	}
	return srv.entryStrings(srv.bans.view), nil
}

// Banned reports whether the player of the name passed, or the address
// passed, is banned, and the reason. On a server keyed by XUID, the name is
// that of a player online, whose account is checked, or the name of a ban
// held until an account joins with it.
func (srv *Server) Banned(addr, name string) (string, bool) {
	if srv.bans == nil {
		return "", false
	}
	if srv.IdentityKey() != identity.XUID {
		return srv.bans.banned(addr, name)
	}
	t, _ := srv.ResolvePlayer(name)
	return srv.bans.bannedAccount(addr, t.Key)
}

// SaveAll saves every dimension and waits until they reach the disk. It waits
// for the owners of the worlds: call it off any world owner only, such as from
// a goroutine of the console. From a transaction, use SaveAllFrom.
func (srv *Server) SaveAll() error {
	seen := map[*world.World]struct{}{}
	for _, w := range srv.dimensions {
		if w == nil {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		w.Save()
	}
	return nil
}

// SaveAllFrom saves every dimension like SaveAll for code running in tx, such
// as a command, and returns at once: the worlds are saved from a goroutine of
// their own, as tx's world cannot be saved while tx runs and the owner of
// another may be waiting for it. done, if not nil, runs in a transaction of
// tx's world once every dimension was saved, with the error of SaveAll. If tx
// is nil, done runs with a nil transaction on that goroutine.
func (srv *Server) SaveAllFrom(tx *world.Tx, done func(tx *world.Tx, err error)) {
	var w *world.World
	if tx != nil {
		w = tx.World()
	}
	go func() {
		err := srv.SaveAll()
		switch {
		case done == nil:
		case w == nil:
			done(nil, err)
		default:
			w.Do(func(tx *world.Tx) { done(tx, err) })
		}
	}()
}
func (srv *Server) SaveEnabled(enabled bool) {
	seen := map[*world.World]struct{}{}
	for _, w := range srv.dimensions {
		if w == nil {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		w.SetAutoSaveEnabled(enabled)
	}
}
func (srv *Server) SaveEnabledState() bool {
	if srv.world == nil {
		return false
	}
	return srv.world.AutoSaveEnabled()
}
func (srv *Server) SetIdleTimeout(minutes int) {
	if minutes < 0 {
		minutes = 0
	}
	srv.idleTimeout.Store(int64(minutes))
}
func (srv *Server) IdleTimeout() int { return int(srv.idleTimeout.Load()) }
func (srv *Server) Version() string  { return fmt.Sprintf("Dragonfly (%s)", version.Minecraft) }
func idleTimeoutExpired(lastActivity, now time.Time, timeout time.Duration) bool {
	return timeout > 0 && !lastActivity.IsZero() && !lastActivity.Add(timeout).After(now)
}

func (srv *Server) kickIdlePlayers() {
	minutes := srv.idleTimeout.Load()
	if minutes <= 0 {
		return
	}
	timeout := time.Duration(minutes) * time.Minute
	now := time.Now()
	for p := range srv.Players(nil) {
		if idleTimeoutExpired(p.LastActivity(), now, timeout) {
			p.Disconnect("You have been idle for too long.")
		}
	}
}
func (srv *Server) idleTimeoutLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			srv.kickIdlePlayers()
		case <-srv.idleStop:
			return
		}
	}
}
