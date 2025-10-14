package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/pelletier/go-toml"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

var (
	// ErrWhitelistUnavailable is returned when the whitelist is not configured.
	ErrWhitelistUnavailable = errors.New("whitelist is not configured")
	// ErrWhitelistInvalidName is returned when an invalid player name is provided to a whitelist operation.
	ErrWhitelistInvalidName = errors.New("invalid player name")
)

// Whitelist controls which players are allowed to join the server. Entries are persisted in a TOML file.
type Whitelist struct {
	mu       sync.RWMutex
	players  map[string]string
	filePath string
	enabled  bool
}

type whitelistFile struct {
	Players []string `toml:"players"`
}

// LoadWhitelist loads the whitelist stored in the file at the provided path. If the file does not exist yet, it will
// be created with an empty player list.
func LoadWhitelist(path string) (*Whitelist, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("whitelist path must not be empty")
	}
	w := &Whitelist{
		players:  make(map[string]string),
		filePath: path,
	}
	if err := w.reloadFromDisk(); err != nil {
		return nil, err
	}
	return w, nil
}

// Enabled reports if the whitelist is currently enforced.
func (w *Whitelist) Enabled() bool {
	if w == nil {
		return false
	}
	return w.enabled
}

// SetEnabled updates whether the whitelist is enforced.
func (w *Whitelist) SetEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.enabled = enabled
}

// Allow implements the Allower interface, allowing players to join only if the whitelist is enabled and contains their
// name.
func (w *Whitelist) Allow(_ net.Addr, d login.IdentityData, _ login.ClientData) (string, bool) {
	if w == nil || !w.enabled {
		return "", true
	}
	name := strings.TrimSpace(d.DisplayName)
	if name == "" {
		return "You are not whitelisted on this server.", false
	}

	w.mu.RLock()
	_, ok := w.players[normalizeName(name)]
	w.mu.RUnlock()
	if !ok {
		return "You are not whitelisted on this server.", false
	}
	return "", true
}

// Add inserts the provided name into the whitelist. The returned bool indicates if the name was newly added.
func (w *Whitelist) Add(name string) (bool, error) {
	if w == nil {
		return false, ErrWhitelistUnavailable
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, ErrWhitelistInvalidName
	}
	key := normalizeName(trimmed)

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, exists := w.players[key]; exists {
		return false, nil
	}
	w.players[key] = trimmed
	if err := w.writeLocked(); err != nil {
		delete(w.players, key)
		return false, err
	}
	return true, nil
}

// Remove deletes the provided name from the whitelist. The returned bool indicates if the name was present before the
// call.
func (w *Whitelist) Remove(name string) (bool, error) {
	if w == nil {
		return false, ErrWhitelistUnavailable
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false, ErrWhitelistInvalidName
	}
	key := normalizeName(trimmed)

	w.mu.Lock()
	defer w.mu.Unlock()

	original, exists := w.players[key]
	if !exists {
		return false, nil
	}
	delete(w.players, key)
	if err := w.writeLocked(); err != nil {
		w.players[key] = original
		return false, err
	}
	return true, nil
}

// Players returns the list of players stored in the whitelist in a case-insensitive sorted order.
func (w *Whitelist) Players() []string {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()

	names := make([]string, 0, len(w.players))
	for _, name := range w.players {
		names = append(names, name)
	}
	sortNames(names)
	return names
}

func (w *Whitelist) reloadFromDisk() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reloadLocked()
}

func (w *Whitelist) reloadLocked() error {
	data := whitelistFile{}
	contents, err := os.ReadFile(w.filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			w.players = make(map[string]string)
			return w.writeLocked()
		}
		return fmt.Errorf("read whitelist: %w", err)
	}
	if len(contents) != 0 {
		if err := toml.Unmarshal(contents, &data); err != nil {
			return fmt.Errorf("decode whitelist: %w", err)
		}
	}
	w.players = make(map[string]string, len(data.Players))
	for _, name := range data.Players {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}
		w.players[normalizeName(trimmed)] = trimmed
	}
	return nil
}

func (w *Whitelist) writeLocked() error {
	dir := filepath.Dir(w.filePath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("create whitelist directory: %w", err)
		}
	}
	data := whitelistFile{Players: w.normalisedPlayersLocked()}
	encoded, err := toml.Marshal(data)
	if err != nil {
		return fmt.Errorf("encode whitelist: %w", err)
	}
	if err := os.WriteFile(w.filePath, encoded, 0644); err != nil {
		return fmt.Errorf("write whitelist: %w", err)
	}
	return nil
}

func (w *Whitelist) normalisedPlayersLocked() []string {
	names := make([]string, 0, len(w.players))
	for _, name := range w.players {
		names = append(names, name)
	}
	sortNames(names)
	return names
}

func sortNames(names []string) {
	slices.SortFunc(names, func(a, b string) int {
		lowerA, lowerB := strings.ToLower(a), strings.ToLower(b)
		if lowerA == lowerB {
			return strings.Compare(a, b)
		}
		return strings.Compare(lowerA, lowerB)
	})
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var _ Allower = (*Whitelist)(nil)
