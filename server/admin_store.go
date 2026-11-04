package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/identity"
)

type banEntry struct {
	// XUID is that of the Xbox account of a ban a server keyed by XUID
	// wrote, Name then the name the player last had.
	XUID   string `json:"xuid,omitempty"`
	Name   string `json:"name,omitempty"`
	IP     string `json:"ip,omitempty"`
	Reason string `json:"reason,omitempty"`
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
}

func loadBans(path string) (*banList, error) {
	b := &banList{file: path, ips: map[string]banEntry{}}
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
		en.reason, en.ip = e.Reason, e.IP
		b.players.put(en)
	}
	for _, e := range data.IPs {
		key := normalizeIP(e.IP)
		if key == "" {
			continue
		}
		e.IP = key
		if _, ok := b.ips[key]; !ok {
			b.ipOrder = append(b.ipOrder, key)
		}
		b.ips[key] = e
	}
	return b, nil
}

func normalizeIP(value string) string {
	value = strings.TrimSpace(value)
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if ip := net.ParseIP(strings.Trim(value, "[]")); ip != nil {
		return ip.String()
	}
	return ""
}

func (b *banList) writeLocked() error {
	data := banFile{Players: make([]banEntry, 0, len(b.players.order)), IPs: make([]banEntry, 0, len(b.ipOrder))}
	for _, en := range b.players.order {
		e := banEntry{Name: en.key.String(), IP: en.ip, Reason: en.reason}
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

// add bans the player of en for the reason passed and writes the file,
// unless the player is banned.
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

// bannedAccount reports whether the account of the key passed, or the
// address passed, is banned, as a server keyed by XUID checks a player,
// and the reason.
func (b *banList) bannedAccount(addr string, k identity.Key) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if e, ok := b.players.get(k); ok {
		return e.reason, true
	}
	return b.bannedIPLocked(addr)
}
func (b *banList) addIP(ip, reason string) (bool, error) {
	key := normalizeIP(ip)
	if key == "" {
		return false, fmt.Errorf("invalid IP address %q", ip)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.ips[key]; ok {
		return false, nil
	}
	b.ips[key] = banEntry{IP: key, Reason: reason}
	b.ipOrder = append(b.ipOrder, key)
	return true, b.writeLocked()
}
func (b *banList) removeIP(ip string) (bool, error) {
	key := normalizeIP(ip)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.ips[key]; !ok {
		return false, nil
	}
	delete(b.ips, key)
	b.ipOrder = slices.DeleteFunc(b.ipOrder, func(v string) bool { return v == key })
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
// reason.
func (b *banList) banned(addr, name string) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if e, ok := b.players.match(name); ok {
		return e.reason, true
	}
	return b.bannedIPLocked(addr)
}

// bannedIPLocked reports whether the address passed is banned, and the
// reason.
func (b *banList) bannedIPLocked(addr string) (string, bool) {
	if ip := normalizeIP(addr); ip != "" {
		if e, ok := b.ips[ip]; ok {
			return e.Reason, true
		}
	}
	return "", false
}
