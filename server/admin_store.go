package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
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
	mu          sync.RWMutex
	file        string
	players     map[string]banEntry
	ips         map[string]banEntry
	playerOrder []string
	ipOrder     []string
}

func loadBans(path string) (*banList, error) {
	b := &banList{file: path, players: map[string]banEntry{}, ips: map[string]banEntry{}}
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
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
	for _, e := range data.Players {
		key := normalizeName(e.Name)
		if key == "" {
			continue
		}
		e.Name = key
		if _, ok := b.players[key]; !ok {
			b.playerOrder = append(b.playerOrder, key)
		}
		b.players[key] = e
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
	dir := filepath.Dir(b.file)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("create bans directory: %w", err)
		}
	}
	data := banFile{Players: make([]banEntry, 0, len(b.playerOrder)), IPs: make([]banEntry, 0, len(b.ipOrder))}
	for _, k := range b.playerOrder {
		if e, ok := b.players[k]; ok {
			data.Players = append(data.Players, e)
		}
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
	if err = os.WriteFile(b.file, append(encoded, '\n'), 0644); err != nil {
		return fmt.Errorf("write bans: %w", err)
	}
	return nil
}
func (b *banList) addPlayer(name, reason string) (bool, error) {
	key := normalizeName(name)
	if key == "" {
		return false, errors.New("player name must not be empty")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.players[key]; ok {
		return false, nil
	}
	b.players[key] = banEntry{Name: key, Reason: reason}
	b.playerOrder = append(b.playerOrder, key)
	return true, b.writeLocked()
}
func (b *banList) removePlayer(name string) (bool, error) {
	key := normalizeName(name)
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.players[key]; !ok {
		return false, nil
	}
	delete(b.players, key)
	b.playerOrder = slices.DeleteFunc(b.playerOrder, func(v string) bool { return v == key })
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
	order := b.playerOrder
	data := b.players
	if ips {
		order = b.ipOrder
		data = b.ips
	}
	out := make([]string, 0, len(data))
	for _, k := range order {
		if _, ok := data[k]; ok {
			out = append(out, k)
		}
	}
	return out
}
func (b *banList) banned(addr, name string) (string, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if e, ok := b.players[normalizeName(name)]; ok {
		return e.Reason, true
	}
	if ip := normalizeIP(addr); ip != "" {
		if e, ok := b.ips[ip]; ok {
			return e.Reason, true
		}
	}
	return "", false
}

type adminAllower struct {
	base      Allower
	bans      *banList
	operators *operatorsList
}

func (a adminAllower) Allow(addr net.Addr, d login.IdentityData, c login.ClientData) (string, bool) {
	addrString := ""
	if addr != nil {
		addrString = addr.String()
	}
	if reason, ok := a.bans.banned(addrString, d.DisplayName); ok {
		msg := "You are banned from this server."
		if reason != "" {
			msg += " Reason: " + reason
		}
		return msg, false
	}
	if a.operators != nil && a.operators.isOperator(d.DisplayName) {
		return "", true
	}
	return a.base.Allow(addr, d, c)
}
func (a adminAllower) adminAccess() (*operatorsList, *banList) { return a.operators, a.bans }
func (a adminAllower) underlyingWhitelist() *Whitelist {
	if holder, ok := a.base.(interface{ underlyingWhitelist() *Whitelist }); ok {
		return holder.underlyingWhitelist()
	}
	return nil
}
