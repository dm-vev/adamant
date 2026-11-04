package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/identity"
)

type banEntry struct {
	// XUID is that of the Xbox account of a ban a server keyed by XUID
	// wrote, Name then the name the player last had.
	XUID   string `json:"xuid,omitempty"`
	Name   string `json:"name,omitempty"`
	IP     string `json:"ip,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Created, Source and Expires are Java's: when the ban was made, by
	// whom, and until when, left out if unknown or for ever.
	Created banTime `json:"created,omitzero"`
	Source  string  `json:"source,omitempty"`
	Expires banTime `json:"expires,omitzero"`
}

// fields returns the fields of the ban of the entry.
func (e banEntry) fields() banFields {
	return banFields{created: e.Created.Time, source: e.Source, expires: e.Expires.Time}
}

// withFields returns e with the fields of the ban passed.
func (e banEntry) withFields(f banFields) banEntry {
	e.Created, e.Source, e.Expires = banTime{f.created}, f.source, banTime{f.expires}
	return e
}

type banFile struct {
	Players []banEntry `json:"players"`
	IPs     []banEntry `json:"ips"`
}

type banList struct {
	mu      sync.RWMutex
	file    string
	players entries
	ips     map[string]banEntry
	ipOrder []string
	// now is the clock the bans expire by.
	now func() time.Time
	// messages are the texts banned players are refused with.
	messages BanMessages
	// expired, if not nil, is told of the bans that expired, which were
	// taken off the list; it must not wait.
	expired func(players []entry, ips []banEntry)
	// maxDuration is the longest a ban for a time lasts, zero for no
	// limit.
	maxDuration time.Duration
	// v6Prefix is how many leading bits of an IPv6 address a new ban of it
	// covers.
	v6Prefix int
	// prefixes are the keys of the bans of networks, which a login scans.
	prefixes []string
}

func loadBans(path string) (*banList, error) {
	b := &banList{file: path, ips: map[string]banEntry{}, now: time.Now, messages: DefaultBanMessages(), v6Prefix: DefaultIPv6Prefix}
	contents, err := os.ReadFile(path)
	if errNotExist(err) {
		if err = b.writeLocked(); err != nil {
			return nil, err
		}
		return b, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read bans: %w", err)
	}
	var data banFile
	if err = json.Unmarshal(contents, &data); err != nil {
		return nil, fmt.Errorf("decode bans: %w", err)
	}
	b.players = newEntries(len(data.Players))
	for _, e := range data.Players {
		var en entry
		if e.XUID != "" {
			if en, err = xuidEntry(e.XUID, e.Name); err != nil {
				return nil, fmt.Errorf("decode bans: player %q: %w", e.Name, err)
			}
		} else if en, err = nameEntry(e.Name); err != nil {
			continue
		}
		// A ban of a name written twice keeps its place and the fields of
		// the last.
		en.reason, en.ip, en.ban = e.Reason, e.IP, e.fields()
		b.players.put(en)
	}
	for _, e := range data.IPs {
		key := fileKey(e.IP)
		if key == "" {
			continue
		}
		e.IP = key
		if _, ok := b.ips[key]; !ok {
			b.ipOrder = append(b.ipOrder, key)
		}
		b.ips[key] = e
	}
	b.rebuildPrefixesLocked()
	return b, nil
}


func (b *banList) writeLocked() error {
	data := banFile{Players: make([]banEntry, 0, len(b.players.order)), IPs: make([]banEntry, 0, len(b.ipOrder))}
	for _, en := range b.players.order {
		e := banEntry{Name: en.key.String(), IP: en.ip, Reason: en.reason}.withFields(en.ban)
		if x, ok := en.key.XUID(); ok {
			e.XUID, e.Name = x, en.name
		}
		data.Players = append(data.Players, e)
	}
	for _, k := range b.ipOrder {
		if e, ok := b.ips[k]; ok {
			data.IPs = append(data.IPs, e)
		}
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(b.file, append(encoded, '\n'), "bans")
}
func (b *banList) addPlayer(name, reason string) (bool, error) {
	en, err := nameEntry(name)
	if err != nil {
		return false, err
	}
	return b.add(en, reason)
}

// add bans the player of en for the reason passed, with the fields of en,
// and writes the file, unless the player is banned.
func (b *banList) add(en entry, reason string) (bool, error) {
	en.reason = reason
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.players.add(en) {
		return false, nil
	}
	return true, b.writeLocked()
}
func (b *banList) removePlayer(name string) (bool, error) {
	en, err := nameEntry(name)
	if err != nil {
		return false, nil
	}
	return b.remove(en.key)
}

// ban bans the player of en for the reason passed, with the fields of en,
// over any ban of the player as reban says, and writes the file if it
// changed.
func (b *banList) ban(en entry, reason string) (BanChange, error) {
	if err := en.ban.checkExpiry(b.maxDuration); err != nil {
		return BanUnchanged, err
	}
	en.reason = reason
	b.mu.Lock()
	defer b.mu.Unlock()
	old, ok := b.players.get(en.key)
	if !ok {
		b.players.add(en)
		return BanAdded, b.writeLocked()
	}
	next, change := reban(old, en.ban, reason)
	if change == BanUnchanged {
		return change, nil
	}
	b.players.put(next)
	return change, b.writeLocked()
}

// banAddress bans the address or network of the key passed, as banKey
// returns it, as ban bans a player.
func (b *banList) banAddress(key, reason string, f banFields) (BanChange, error) {
	if err := f.checkExpiry(b.maxDuration); err != nil {
		return BanUnchanged, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	e, ok := b.ips[key]
	if !ok {
		b.ips[key] = banEntry{IP: key, Reason: reason}.withFields(f)
		b.ipOrder = append(b.ipOrder, key)
		b.rebuildPrefixesLocked()
		return BanAdded, b.writeLocked()
	}
	next, change := reban(entry{reason: e.Reason, ban: e.fields()}, f, reason)
	if change == BanUnchanged {
		return change, nil
	}
	b.ips[key] = banEntry{IP: key, Reason: next.reason}.withFields(next.ban)
	return change, b.writeLocked()
}

// infos returns the bans of players, or of addresses if ips, as /banlist
// shows them, the players shown by show.
func (b *banList) infos(ips bool, show func(entry) string) []BanInfo {
	b.mu.RLock()
	defer b.mu.RUnlock()
	var out []BanInfo
	if !ips {
		for _, en := range b.players.order {
			out = append(out, b.info(show(en), false, en.reason, en.ban))
		}
		return out
	}
	for _, k := range b.ipOrder {
		if e, ok := b.ips[k]; ok {
			out = append(out, b.info(e.IP, true, e.Reason, e.fields()))
		}
	}
	return out
}

// infoOf returns the ban of the player of the key passed, shown by show,
// or, if ip is not empty, of the address or network of that key, and
// reports whether there is one.
func (b *banList) infoOf(k identity.Key, ip string, show func(entry) string) (BanInfo, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if ip != "" {
		e, ok := b.ips[ip]
		if !ok {
			return BanInfo{}, false
		}
		return b.info(e.IP, true, e.Reason, e.fields()), true
	}
	en, ok := b.players.get(k)
	if !ok {
		return BanInfo{}, false
	}
	return b.info(show(en), false, en.reason, en.ban), true
}

// info returns a ban as /banlist shows it.
func (b *banList) info(target string, ip bool, reason string, f banFields) BanInfo {
	until, source := "", f.source
	if !f.expires.IsZero() {
		until = f.expires.Local().Format(b.messages.TimeFormat)
	}
	if source == "" {
		source = "(Unknown)"
	}
	return BanInfo{Target: target, IP: ip, Reason: reason, Source: source, Created: f.created, Expires: f.expires, Until: until}
}

// remove pardons the player of the key passed and writes the file.
func (b *banList) remove(k identity.Key) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.players.remove(k) {
		return false, nil
	}
	return true, b.writeLocked()
}

// view runs f with the bans of players, which f must not change.
func (b *banList) view(f func(e *entries)) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	f(&b.players)
}

// bind binds the pending ban of the name passed to the account x, which
// joined with it, and writes the file. It returns the pending ban and
// reports whether there was one.
func (b *banList) bind(x identity.Key, name string) (entry, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	pending, ok := b.players.bind(x, name)
	if !ok {
		return entry{}, false, nil
	}
	return pending, true, b.writeLocked()
}

// banHit is a ban a player is refused by: its reason, its fields, and
// whether it is of the address.
type banHit struct {
	reason string
	fields banFields
	ip     bool
}

// message returns the text the player of the ban is refused with.
func (b *banList) message(h banHit) string {
	return b.messages.format(h.reason, h.fields, h.ip)
}

// bannedAccount reports whether the account of the key passed, or the
// address passed, is banned, as a server keyed by XUID checks a player,
// and the ban. A ban that expired applies no more, and is taken off.
func (b *banList) bannedAccount(addr string, k identity.Key) (banHit, bool) {
	return b.check(func() (banHit, bool, bool) {
		if e, ok := b.players.get(k); ok {
			return banHit{reason: e.reason, fields: e.ban}, true, e.ban.expired(b.now())
		}
		return b.bannedIPLocked(addr)
	})
}

// check runs find under the read lock: a ban that it finds and that
// expired applies no more, and the expired bans are taken off once the
// lock is released.
func (b *banList) check(find func() (banHit, bool, bool)) (banHit, bool) {
	b.mu.RLock()
	h, ok, expired := find()
	b.mu.RUnlock()
	if ok && expired {
		b.prune()
		return banHit{}, false
	}
	return h, ok
}

// prune takes the bans that expired off the list, writes the file if any
// did, and tells expired of them.
func (b *banList) prune() {
	now := b.now()
	var players []entry
	var ips []banEntry
	b.mu.Lock()
	for _, en := range slices.Clone(b.players.order) {
		if en.ban.expired(now) {
			b.players.remove(en.key)
			players = append(players, en)
		}
	}
	for _, k := range slices.Clone(b.ipOrder) {
		if e, ok := b.ips[k]; ok && e.fields().expired(now) {
			delete(b.ips, k)
			b.ipOrder = slices.DeleteFunc(b.ipOrder, func(v string) bool { return v == k })
			ips = append(ips, e)
		}
	}
	b.rebuildPrefixesLocked()
	var err error
	if len(players)+len(ips) > 0 {
		err = b.writeLocked()
	}
	b.mu.Unlock()
	if err != nil {
		slog.Default().Error("Write the bans without those that expired: " + err.Error())
	}
	if f := b.expired; f != nil && len(players)+len(ips) > 0 {
		f(players, ips)
	}
}
func (b *banList) addIP(ip, reason string) (bool, error) {
	key, ok := banKey(ip, 128)
	if !ok {
		return false, fmt.Errorf("invalid IP address %q", ip)
	}
	return b.addAddress(key, reason, banFields{})
}

// addAddress bans the address or network of the key passed, as banKey
// returns it, for the reason passed, with the fields passed, and writes the
// file, unless it is banned.
func (b *banList) addAddress(key, reason string, f banFields) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.ips[key]; ok {
		return false, nil
	}
	b.ips[key] = banEntry{IP: key, Reason: reason}.withFields(f)
	b.ipOrder = append(b.ipOrder, key)
	b.rebuildPrefixesLocked()
	return true, b.writeLocked()
}

// removeIP pardons the ban of the address or the network passed: the ban
// of it as typed, or else the ban a new ban of it would make, as of the
// network of an IPv6 address.
func (b *banList) removeIP(ip string) (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	key := fileKey(ip)
	if _, ok := b.ips[key]; !ok {
		key, _ = banKey(ip, b.v6Prefix)
	}
	if _, ok := b.ips[key]; !ok || key == "" {
		return false, nil
	}
	delete(b.ips, key)
	b.ipOrder = slices.DeleteFunc(b.ipOrder, func(v string) bool { return v == key })
	b.rebuildPrefixesLocked()
	return true, b.writeLocked()
}
func (b *banList) entries(ips bool) []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if !ips {
		return b.players.keys()
	}
	out := make([]string, 0, len(b.ips))
	for _, k := range b.ipOrder {
		if _, ok := b.ips[k]; ok {
			out = append(out, k)
		}
	}
	return out
}

// banned reports whether the player of the name passed, or the address
// passed, is banned, as a server keyed by name checks a player, and the
// ban. A ban that expired applies no more, and is taken off.
func (b *banList) banned(addr, name string) (banHit, bool) {
	return b.check(func() (banHit, bool, bool) {
		if e, ok := b.players.match(name); ok {
			return banHit{reason: e.reason, fields: e.ban}, true, e.ban.expired(b.now())
		}
		return b.bannedIPLocked(addr)
	})
}

// bannedIPLocked reports whether the address passed is banned, the ban,
// and whether it expired.
//
// An address is banned by a ban of it alone, or of a network that holds
// it, unless it is a loopback address, which only a ban of it alone bans.
func (b *banList) bannedIPLocked(addr string) (banHit, bool, bool) {
	a, ok := parseAddr(addr)
	if !ok {
		return banHit{}, false, false
	}
	hit := func(e banEntry) (banHit, bool, bool) {
		return banHit{reason: e.Reason, fields: e.fields(), ip: true}, true, e.fields().expired(b.now())
	}
	if e, ok := b.ips[a.String()]; ok {
		return hit(e)
	}
	for _, k := range b.prefixes {
		if keyMatches(k, a) {
			return hit(b.ips[k])
		}
	}
	return banHit{}, false, false
}

// update runs f with the entries of the list, and writes the file if f
// reports it changed them.
func (b *banList) update(f func(e *entries) bool) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !f(&b.players) {
		return nil
	}
	return b.writeLocked()
}

// path returns the path of the file of the list.
func (b *banList) path() string { return b.file }
