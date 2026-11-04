package server

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/identity"
	"github.com/pelletier/go-toml"
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
	players  entries
	filePath string
	enabled  bool
	// enforce kicks the players online the whitelist does not hold when it
	// is turned on or reloaded.
	enforce bool
	kickMsg string
	format  whitelistFormat
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

// SetEnforce sets whether turning the whitelist on, or reloading it, kicks
// the players online it does not hold who are not operators, as Java's
// enforce-whitelist.
func (w *Whitelist) SetEnforce(enforce bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.enforce = enforce
	w.mu.Unlock()
}

// Enforced reports whether the whitelist is on and enforced on the players
// online.
func (w *Whitelist) Enforced() bool {
	if w == nil {
		return false
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.enabled && w.enforce
}

// kickMessage returns the message the whitelist refuses players with.
func (w *Whitelist) kickMessage() string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.kickMsg
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
func (w *Whitelist) Allow(_ net.Addr, id client.Identity, _ client.Profile) (string, bool) {
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

	name := strings.TrimSpace(id.Name)
	if name == "" {
		w.mu.RUnlock()
		return kickMsg, false
	}

	_, ok := w.players.match(name)
	w.mu.RUnlock()
	if !ok {
		return kickMsg, false
	}
	return "", true
}

// allowAccount holds the account of the key passed to the whitelist, as a
// server keyed by XUID does: if enabled, it must be on it.
func (w *Whitelist) allowAccount(k identity.Key) (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if !w.enabled || w.players.has(k) {
		return "", true
	}
	return w.kickMsg, false
}

// add puts en on the whitelist and writes the file, unless it holds its
// key. On a failed write, the whitelist is left as it was.
func (w *Whitelist) add(en entry) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.players.has(en.key) {
		return false, nil
	}
	before := w.players.clone()
	w.players.add(en)
	if err := w.writeLocked(); err != nil {
		w.players = before
		return false, err
	}
	return true, nil
}

// remove takes the entry of the key passed off the whitelist and writes the
// file. On a failed write, the whitelist is left as it was.
func (w *Whitelist) remove(k identity.Key) (bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.players.has(k) {
		return false, nil
	}
	before := w.players.clone()
	w.players.remove(k)
	if err := w.writeLocked(); err != nil {
		w.players = before
		return false, err
	}
	return true, nil
}

// view runs f with the entries of the whitelist, which f must not change.
func (w *Whitelist) view(f func(e *entries)) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	f(&w.players)
}

// bind binds the pending entry of the name passed to the account x, which
// joined with it, and writes the file. It returns the pending entry and
// reports whether there was one.
func (w *Whitelist) bind(x identity.Key, name string) (entry, bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	pending, ok := w.players.bind(x, name)
	if !ok {
		return entry{}, false, nil
	}
	return pending, true, w.writeLocked()
}

// Add inserts the provided name into the whitelist. The returned bool indicates if the name was newly added.
func (w *Whitelist) Add(name string) (bool, error) {
	if w == nil {
		return false, ErrWhitelistUnavailable
	}
	en, err := nameEntry(name)
	if err != nil {
		return false, ErrWhitelistInvalidName
	}
	return w.add(en)
}

// Remove deletes the provided name from the whitelist. The returned bool indicates if the name was present before the
// call.
func (w *Whitelist) Remove(name string) (bool, error) {
	if w == nil {
		return false, ErrWhitelistUnavailable
	}
	en, err := nameEntry(name)
	if err != nil {
		return false, ErrWhitelistInvalidName
	}
	return w.remove(en.key)
}

// Players returns the list of players stored in the whitelist in the order they appear on disk.
func (w *Whitelist) Players() []string {
	if w == nil {
		return nil
	}
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.players.keys()
}

func (w *Whitelist) reloadFromDisk() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reloadLocked()
}

func (w *Whitelist) reloadLocked() error {
	contents, err := os.ReadFile(w.filePath)
	if err != nil {
		if errNotExist(err) {
			w.players = entries{}
			return w.writeLocked()
		}
		return fmt.Errorf("read whitelist: %w", err)
	}

	players, err := parseWhitelistFile(w.format, contents)
	if err != nil {
		return err
	}
	w.players = players
	return nil
}

func (w *Whitelist) writeLocked() error {
	encoded, err := encodeWhitelistFile(w.format, w.players)
	if err != nil {
		return err
	}
	return writeFileAtomic(w.filePath, encoded, "whitelist")
}

func parseWhitelistFile(format whitelistFormat, contents []byte) (entries, error) {
	var names []string
	switch format {
	case whitelistFormatTOML:
		data := whitelistFile{}
		if len(contents) != 0 {
			if err := toml.Unmarshal(contents, &data); err != nil {
				return entries{}, fmt.Errorf("decode whitelist: %w", err)
			}
		}
		names = data.Players
	case whitelistFormatList:
		content := strings.ReplaceAll(string(contents), "\r\n", "\n")
		names = strings.Split(content, "\n")
	default:
		return entries{}, errors.New("unsupported whitelist file format")
	}
	players := newEntries(len(names))
	for _, name := range names {
		if en, ok := parseEntryText(name); ok {
			players.add(en)
		}
	}
	return players, nil
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

func encodeWhitelistFile(format whitelistFormat, players entries) ([]byte, error) {
	var names []string
	if players.order != nil {
		names = make([]string, 0, len(players.order))
	}
	for _, en := range players.order {
		names = append(names, entryText(en))
	}
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

// normalizeName returns the form the lists compare names in.
func normalizeName(name string) string {
	return identity.Normalize(name)
}

var _ Allower = (*Whitelist)(nil)

// update runs f with the entries of the list, and writes the file if f
// reports it changed them.
func (w *Whitelist) update(f func(e *entries) bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !f(&w.players) {
		return nil
	}
	return w.writeLocked()
}

// path returns the path of the file of the list.
func (w *Whitelist) path() string { return w.filePath }
