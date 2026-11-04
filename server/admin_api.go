package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/version"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
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

// PlayerIsOperator reports whether the player online of the UUID passed is
// an operator: by its account on a server keyed by XUID, by its name on one
// keyed by name. A UUID of no player online, such as that of an entity of a
// plugin named after an operator, is no operator's. It is the operator check
// of the commands, the game master blocks and the abilities a client is
// shown.
func (srv *Server) PlayerIsOperator(id uuid.UUID) bool {
	p, ok := srv.players.player(id)
	return ok && srv.operatorOf(p.id)
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
// BanPlayer bans the player of the name passed, or "xuid:<XUID>", as
// ResolvePlayer resolves it, for the reason passed, for ever, recording no
// time or source, as the server always banned. A player online of the ban
// is kicked with the text of [Bans]. Ban records when, by whom and until
// when.
func (srv *Server) BanPlayer(name, reason string) (bool, error) {
	return srv.banPlayer(name, reason, banFields{})
}

// Ban bans the player of the name passed, or "xuid:<XUID>", as
// ResolvePlayer resolves it, as the options say, recording when the ban
// was made, by whom (o.By, or "API"), and until when (o.Expires or o.For,
// or for ever). A ban for a time lasts a minute at least and [Bans]
// MaxDuration at most. A name no account is known by on a server keyed by
// XUID is held until an account joins with it, and the ban keeps its
// expiry. Over a ban of the player, a ban for ever makes a ban for a time
// for ever, and a ban for a time replaces the expiry; BanChange says what
// changed. A player online of a new ban is kicked with the text of
// [Bans].
func (srv *Server) Ban(name string, o BanOptions) (BanChange, error) {
	if srv.bans == nil {
		return BanUnchanged, errAdminUnavailable
	}
	en, _, err := srv.addition(name)
	if err != nil {
		return BanUnchanged, err
	}
	en.ban = o.fields(srv.bansNow())
	change, err := srv.bans.ban(en, o.Reason)
	if change == BanAdded {
		srv.kickBanned(en, o.Reason)
	}
	return change, err
}

// kickBanned kicks the player online of the entry of a new ban, if any,
// with the text of [Bans].
func (srv *Server) kickBanned(en entry, reason string) {
	if h, ok := srv.playerOf(en); ok {
		msg := srv.bans.message(banHit{reason: reason, fields: en.ban})
		go player.Do(h, func(_ *world.Tx, p *player.Player) { p.Disconnect(msg) })
	}
}

// banPlayer bans the player of the name passed for the reason passed, with
// the fields passed, and kicks it if online.
func (srv *Server) banPlayer(name, reason string, f banFields) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	en, _, err := srv.addition(name)
	if err != nil {
		return false, err
	}
	en.ban = f
	added, err := srv.bans.add(en, reason)
	if added {
		srv.kickBanned(en, reason)
	}
	return added, err
}

// bansNow returns the time of the clock the bans expire by.
func (srv *Server) bansNow() time.Time {
	if srv.bans == nil || srv.bans.now == nil {
		return time.Now()
	}
	return srv.bans.now()
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
// BanIP bans the address passed for the reason passed, for ever,
// recording no time or source, as the server always banned. The players
// online from the address are kicked with the text of [Bans]. BanAddress
// records when, by whom and until when.
func (srv *Server) BanIP(ip, reason string) (bool, error) {
	return srv.banAddress(ip, reason, banFields{})
}

// BanAddress bans the address passed as the options say, recording when
// the ban was made, by whom and until when, over any ban of it, as Ban
// does. The players online from the address are kicked with the text of
// [Bans].
func (srv *Server) BanAddress(ip string, o BanOptions) (BanChange, error) {
	if srv.bans == nil {
		return BanUnchanged, errAdminUnavailable
	}
	f := o.fields(srv.bansNow())
	change, err := srv.bans.banAddress(ip, o.Reason, f)
	if change == BanAdded {
		srv.kickAddress(ip, o.Reason, f)
	}
	return change, err
}

// banAddress bans an address for the reason passed, with the fields
// passed, and kicks the players online from it.
func (srv *Server) banAddress(ip, reason string, f banFields) (bool, error) {
	if srv.bans == nil {
		return false, errAdminUnavailable
	}
	added, err := srv.bans.addAddress(ip, reason, f)
	if added {
		srv.kickAddress(ip, reason, f)
	}
	return added, err
}

// kickAddress kicks the players online from the address of a new ban,
// from a goroutine of its own, with the text of [Bans].
func (srv *Server) kickAddress(ip, reason string, f banFields) {
	target := normalizeIP(ip)
	msg := srv.bans.message(banHit{reason: reason, fields: f, ip: true})
	go func() {
		for p := range srv.Players(nil) {
			if p.Addr() != nil && normalizeIP(p.Addr().String()) == target {
				p.Disconnect(msg)
			}
		}
	}()
}

// BanList returns the bans of players, or of addresses if ips, as /banlist
// shows them, after taking off those that expired.
func (srv *Server) BanList(ips bool) ([]BanInfo, error) {
	if srv.bans == nil {
		return nil, errAdminUnavailable
	}
	srv.bans.prune()
	return srv.bans.infos(ips, func(en entry) string { return Target{Key: en.key, Name: en.name}.String() }), nil
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
	var hit banHit
	var ok bool
	if srv.IdentityKey() != identity.XUID {
		hit, ok = srv.bans.banned(addr, name)
	} else {
		t, _ := srv.ResolvePlayer(name)
		hit, ok = srv.bans.bannedAccount(addr, t.Key)
	}
	return hit.reason, ok
}

// SaveAll saves a checkpoint of the server, as Java's save-all saves
// everything: every dimension and the players in them, captured at one
// instant, and waits until they were written, the worlds first, then the
// players, then level.dat. It saves even while saving is off. It waits for
// the owners of the worlds: call it off any world owner only, such as from a
// goroutine of the console. From a transaction, use SaveAllFrom.
func (srv *Server) SaveAll() error {
	return srv.checkpoints.checkpoint(context.Background(), "save-all")
}

// SaveAllFrom saves a checkpoint like SaveAll for code running in tx, such
// as a command, and returns at once: the checkpoint is saved from a goroutine
// of its own, as tx's world cannot be captured while tx runs and the owner of
// another may be waiting for it. done, if not nil, runs in a transaction of
// tx's world once the checkpoint was written, with the error of SaveAll. If
// tx is nil, done runs with a nil transaction on that goroutine.
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

// SaveEnabled turns the periodic checkpoints of the server on or off, as
// save-on and save-off do. /save-all and closing the server save a
// checkpoint either way.
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
	// The bans that expired are taken off within the hour, even if no one
	// banned joins or lists the bans.
	prune := time.NewTicker(time.Hour)
	defer prune.Stop()
	for {
		select {
		case <-prune.C:
			if srv.bans != nil {
				srv.bans.prune()
			}
		case <-ticker.C:
			srv.kickIdlePlayers()
		case <-srv.idleStop:
			return
		}
	}
}
