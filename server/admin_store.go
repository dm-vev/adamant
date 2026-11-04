package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
)

type banEntry struct {
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
		en, err := nameEntry(e.Name)
		if err != nil {
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
		data.Players = append(data.Players, banEntry{Name: en.key.String(), IP: en.ip, Reason: en.reason})
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
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.players.remove(en.key) {
		return false, nil
	}
	return true, b.writeLocked()
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
func (b *banList) banned(addr, name string) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if en, err := nameEntry(name); err == nil {
		if e, ok := b.players.get(en.key); ok {
			return e.reason, true
		}
	}
	if ip := normalizeIP(addr); ip != "" {
		if e, ok := b.ips[ip]; ok {
			return e.Reason, true
		}
	}
	return "", false
}
