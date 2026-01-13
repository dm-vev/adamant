package server

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
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

const defaultWhitelistKickMessage = "§cServer is white-listed"

type whitelistFormat uint8

const (
	whitelistFormatList whitelistFormat = iota
	whitelistFormatTOML
)

// Whitelist controls which players are allowed to join the server.
type Whitelist struct {
	mu       sync.RWMutex
	players  map[string]struct{}
	order    []string
	filePath string
	enabled  bool
	kickMsg  string
	format   whitelistFormat
}

type whitelistFile struct {
	Players []string `toml:"players"`
}

// LoadWhitelist loads the whitelist stored in the file at the provided path.
// If the file does not exist yet, it is created with an empty player list.
func LoadWhitelist(path string) (*Whitelist, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("whitelist path must not be empty")
	}
	format := whitelistFormatFromPath(path)
	w := &Whitelist{
		players:  make(map[string]struct{}),
		filePath: path,
		format:   format,
	}
	w.SetKickMessage("")
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
	w.mu.RLock()
	enabled := w.enabled
	w.mu.RUnlock()
	return enabled
}

// SetEnabled updates whether the whitelist is enforced.
func (w *Whitelist) SetEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.enabled = enabled
	w.mu.Unlock()
}

// SetKickMessage updates the message sent to players that are rejected by the whitelist.
// If the provided string is empty, a safe default is used.
func (w *Whitelist) SetKickMessage(msg string) {
	if w == nil {
		return
	}

	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = defaultWhitelistKickMessage
	}

	w.mu.Lock()
	w.kickMsg = msg
	w.mu.Unlock()
}

// Reload refreshes the whitelist contents from disk.
func (w *Whitelist) Reload() error {
	if w == nil {
		return ErrWhitelistUnavailable
	}
	return w.reloadFromDisk()
}

// Allow implements the Allower interface, allowing players to join only if the whitelist is enabled and contains their
// name.
func (w *Whitelist) Allow(_ net.Addr, d login.IdentityData, _ login.ClientData) (string, bool) {
	if w == nil {
		return "", true
	}

	w.mu.RLock()
	enabled := w.enabled
	kickMsg := w.kickMsg
	if !enabled {
		w.mu.RUnlock()
		return "", true
	}

	name := strings.TrimSpace(d.DisplayName)
	if name == "" {
		w.mu.RUnlock()
		return kickMsg, false
	}

	_, ok := w.players[normalizeName(name)]
	w.mu.RUnlock()
	if !ok {
		return kickMsg, false
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
	w.players[key] = struct{}{}
	w.order = append(w.order, key)
	if err := w.writeLocked(); err != nil {
		delete(w.players, key)
		w.order = w.order[:len(w.order)-1]
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

	if _, exists := w.players[key]; !exists {
		return false, nil
	}
	originalOrder := append([]string(nil), w.order...)
	delete(w.players, key)
	w.removeOrderLocked(key)
	if err := w.writeLocked(); err != nil {
		w.players[key] = struct{}{}
		w.order = originalOrder
		return false, err
	}
	return true, nil
}

func (w *Whitelist) removeOrderLocked(name string) {
	for i, entry := range w.order {
		if entry == name {
			copy(w.order[i:], w.order[i+1:])
			w.order = w.order[:len(w.order)-1]
			return
		}
	}
}

// Players returns the list of players stored in the whitelist in the order they appear on disk.
func (w *Whitelist) Players() []string {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return append([]string(nil), w.order...)
}

func (w *Whitelist) reloadFromDisk() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reloadLocked()
}

func (w *Whitelist) reloadLocked() error {
	contents, err := os.ReadFile(w.filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			w.players = make(map[string]struct{})
			w.order = nil
			return w.writeLocked()
		}
		return fmt.Errorf("read whitelist: %w", err)
	}

	players, order, err := parseWhitelistFile(w.format, contents)
	if err != nil {
		return err
	}
	w.players = players
	w.order = order
	return nil
}

func (w *Whitelist) writeLocked() error {
	dir := filepath.Dir(w.filePath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("create whitelist directory: %w", err)
		}
	}
	encoded, err := encodeWhitelistFile(w.format, w.order)
	if err != nil {
		return err
	}
	if err := os.WriteFile(w.filePath, encoded, 0644); err != nil {
		return fmt.Errorf("write whitelist: %w", err)
	}
	return nil
}

func parseWhitelistFile(format whitelistFormat, contents []byte) (map[string]struct{}, []string, error) {
	players := make(map[string]struct{})
	var order []string

	switch format {
	case whitelistFormatTOML:
		data := whitelistFile{}
		if len(contents) != 0 {
			if err := toml.Unmarshal(contents, &data); err != nil {
				return nil, nil, fmt.Errorf("decode whitelist: %w", err)
			}
		}
		order = make([]string, 0, len(data.Players))
		for _, name := range data.Players {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			key := normalizeName(trimmed)
			if _, exists := players[key]; exists {
				continue
			}
			players[key] = struct{}{}
			order = append(order, key)
		}
		return players, order, nil
	case whitelistFormatList:
		content := strings.ReplaceAll(string(contents), "\r\n", "\n")
		lines := strings.Split(content, "\n")
		order = make([]string, 0, len(lines))
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			key := normalizeName(trimmed)
			if _, exists := players[key]; exists {
				continue
			}
			players[key] = struct{}{}
			order = append(order, key)
		}
		return players, order, nil
	default:
		return nil, nil, errors.New("unsupported whitelist file format")
	}
}

func whitelistFormatFromPath(path string) whitelistFormat {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	switch ext {
	case "toml":
		return whitelistFormatTOML
	case "txt", "list", "enum":
		return whitelistFormatList
	default:
		// When the extension is unknown, default to the line-based list format because it is resilient and easy to
		// inspect/edit by hand.
		return whitelistFormatList
	}
}

func encodeWhitelistFile(format whitelistFormat, names []string) ([]byte, error) {
	switch format {
	case whitelistFormatTOML:
		encoded, err := toml.Marshal(whitelistFile{Players: names})
		if err != nil {
			return nil, fmt.Errorf("encode whitelist: %w", err)
		}
		return encoded, nil
	case whitelistFormatList:
		var b strings.Builder
		for _, name := range names {
			b.WriteString(name)
			b.WriteString("\r\n")
		}
		return []byte(b.String()), nil
	default:
		return nil, errors.New("unsupported whitelist file format")
	}
}

func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

var _ Allower = (*Whitelist)(nil)
