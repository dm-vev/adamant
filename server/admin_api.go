package server

import (
	"errors"
	"fmt"
	"time"

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

func (srv *Server) IsOperator(name string) bool {
	return srv.operators != nil && srv.operators.isOperator(name)
}
func (srv *Server) Op(name string) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	en, err := nameEntry(name)
	if err != nil {
		return false, err
	}
	added, err := srv.operators.add(en)
	if added {
		srv.refreshPlayerPermissions(en.name)
	}
	return added, err
}
func (srv *Server) Deop(name string) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	en, err := nameEntry(name)
	if err != nil {
		return false, nil
	}
	removed, err := srv.operators.remove(en)
	if removed {
		srv.refreshPlayerPermissions(en.name)
	}
	return removed, err
}

// refreshPlayerPermissions runs on the player's current owner after releasing
// the operator lock. Read the latest status there so queued changes cannot
// restore stale permissions after a subsequent op/deop or a world transfer.
func (srv *Server) refreshPlayerPermissions(key string) {
	if handle := srv.players.byKey(key); handle != nil {
		go player.Do(handle, func(_ *world.Tx, p *player.Player) { p.RefreshPermissions() })
	}
}
func (srv *Server) OperatorEntries() ([]string, error) {
	if srv.operators == nil {
		return nil, errAdminUnavailable
	}
	return srv.operators.keys(), nil
}
func (srv *Server) BanPlayer(name, reason string) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	added, err := srv.bans.addPlayer(name, reason)
	if added {
		if h, ok := srv.PlayerByName(name); ok {
			go player.Do(h, func(_ *world.Tx, p *player.Player) { p.Disconnect("You are banned from this server.") })
		}
	}
	return added, err
}
func (srv *Server) PardonPlayer(name string) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	return srv.bans.removePlayer(name)
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
	return srv.bans.entries(ip), nil
}
func (srv *Server) Banned(addr, name string) (string, bool) {
	if srv.bans == nil {
		return "", false
	}
	return srv.bans.banned(addr, name)
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
