package server

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

var errAdminUnavailable = errors.New("admin persistence unavailable")

func (srv *Server) IsOperator(name string) bool {
	return srv.operators != nil && srv.operators.isOperator(name)
}
func (srv *Server) Op(name string) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	key := normalizeName(name)
	if key == "" {
		return false, errors.New("player name must not be empty")
	}
	o := srv.operators
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.players[key]; ok {
		return false, nil
	}
	o.players[key] = struct{}{}
	o.order = append(o.order, key)
	return true, o.writeLocked()
}
func (srv *Server) Deop(name string) (bool, error) {
	if srv.operators == nil {
		return false, errAdminUnavailable
	}
	key := normalizeName(name)
	o := srv.operators
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.players[key]; !ok {
		return false, nil
	}
	delete(o.players, key)
	o.order = slices.DeleteFunc(o.order, func(v string) bool { return v == key })
	return true, o.writeLocked()
}
func (srv *Server) OperatorEntries() ([]string, error) {
	if srv.operators == nil {
		return nil, errAdminUnavailable
	}
	o := srv.operators
	o.mu.RLock()
	defer o.mu.RUnlock()
	return slices.Clone(o.order), nil
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
func (srv *Server) Version() string  { return fmt.Sprintf("Dragonfly (%s)", protocol.CurrentVersion) }
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
