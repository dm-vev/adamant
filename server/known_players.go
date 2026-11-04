package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/identity"
	"github.com/google/uuid"
)

// knownPlayersDebounce is how long the known players wait after a change
// before they write their file, so that a burst of logins writes it once.
const knownPlayersDebounce = 30 * time.Second

// knownPlayers are the Xbox accounts a server keyed by XUID has seen join:
// for each, the name and the UUID it last joined with, and when, as Java
// Edition keeps its usercache.json. The admin commands find an account by
// the name it last had through them. They live in known-players.json,
// beside ops.json.
type knownPlayers struct {
	mu     sync.Mutex
	path   string
	log    *slog.Logger
	byXUID map[string]knownPlayer
	// byName holds, for each name normalised, the XUID of the account that
	// last joined with it: an Xbox gamertag belongs to one account at a
	// time, so once B joins as Steve, A, who was Steve, no longer is.
	byName map[string]string

	debounce time.Duration
	now      func() time.Time
	timer    *time.Timer
	dirty    bool
	closed   bool
}

// knownPlayer is an Xbox account a server has seen, as known-players.json
// holds it.
type knownPlayer struct {
	XUID     string    `json:"xuid"`
	Name     string    `json:"name"`
	UUID     uuid.UUID `json:"uuid"`
	LastSeen time.Time `json:"lastSeen"`
}

// loadKnownPlayers reads the known players of the file at path. A missing
// file holds none; it is written once a player is seen, or at close. It
// reports whether the file existed.
func loadKnownPlayers(path string, log *slog.Logger) (*knownPlayers, bool, error) {
	k := &knownPlayers{path: path, log: log, byXUID: map[string]knownPlayer{}, byName: map[string]string{}, debounce: knownPlayersDebounce, now: time.Now}
	if k.log == nil {
		k.log = slog.Default()
	}
	data, err := os.ReadFile(path)
	if errNotExist(err) {
		return k, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("read known players: %w", err)
	}
	var players []knownPlayer
	if err := json.Unmarshal(data, &players); err != nil {
		return nil, true, fmt.Errorf("decode known players: %w", err)
	}
	for _, p := range players {
		key, err := identity.OfXUID(p.XUID)
		if err != nil {
			return nil, true, fmt.Errorf("decode known players: %w", err)
		}
		p.XUID, _ = key.XUID()
		k.putLocked(p)
	}
	return k, true, nil
}

// putLocked records p, and makes it the account of its name if no account
// joined with the name later.
func (k *knownPlayers) putLocked(p knownPlayer) {
	k.byXUID[p.XUID] = p
	n := identity.Normalize(p.Name)
	if n == "" {
		return
	}
	if other, ok := k.byName[n]; ok && other != p.XUID {
		if o := k.byXUID[other]; identity.Normalize(o.Name) == n && o.LastSeen.After(p.LastSeen) {
			return
		}
	}
	k.byName[n] = p.XUID
}

// seen records that the account of the XUID passed joined with the name
// and the UUID passed, now, and schedules a write of the file. It returns
// the name the account had before, "" if none was known.
func (k *knownPlayers) seen(xuid, name string, id uuid.UUID) (previous string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if old, ok := k.byXUID[xuid]; ok {
		previous = old.Name
		if n := identity.Normalize(old.Name); k.byName[n] == xuid {
			delete(k.byName, n)
		}
	}
	p := knownPlayer{XUID: xuid, Name: strings.TrimSpace(name), UUID: id, LastSeen: k.now().UTC().Truncate(time.Second)}
	k.byXUID[xuid] = p
	if n := identity.Normalize(p.Name); n != "" {
		k.byName[n] = xuid
	}
	k.changedLocked()
	return previous
}

// put records p as known, if no later sighting of its account is, without
// making it the latest sighting of its name over a later one, and
// schedules a write of the file.
func (k *knownPlayers) put(p knownPlayer) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if old, ok := k.byXUID[p.XUID]; ok && !p.LastSeen.After(old.LastSeen) {
		return
	}
	k.putLocked(p)
	k.changedLocked()
}

// seed records p, as the first start of a server keyed by XUID finds it in
// the saved data of the players, without scheduling a write. If not
// byName, the name of p is not made its account's: two records gave it
// to two accounts.
func (k *knownPlayers) seed(p knownPlayer, byName bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.byXUID[p.XUID] = p
	if byName {
		k.putLocked(p)
	}
	k.dirty = true
}

// write writes the file now.
func (k *knownPlayers) write() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.writeLocked()
}

// changedLocked schedules a write of the file after the debounce, unless
// one is scheduled.
func (k *knownPlayers) changedLocked() {
	k.dirty = true
	if k.timer != nil || k.closed {
		return
	}
	k.timer = time.AfterFunc(k.debounce, func() {
		if err := k.flush(); err != nil {
			k.log.Error("Write the known players: " + err.Error())
		}
	})
}

// named returns the account that last joined with the name passed.
func (k *knownPlayers) named(name string) (knownPlayer, bool) {
	if k == nil {
		return knownPlayer{}, false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	x, ok := k.byName[identity.Normalize(name)]
	if !ok {
		return knownPlayer{}, false
	}
	return k.byXUID[x], true
}

// account returns what is known of the account of the XUID passed.
func (k *knownPlayers) account(xuid string) (knownPlayer, bool) {
	if k == nil {
		return knownPlayer{}, false
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	p, ok := k.byXUID[xuid]
	return p, ok
}

// names returns the names the known accounts last had, sorted.
func (k *knownPlayers) names() []string {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]string, 0, len(k.byName))
	for _, x := range k.byName {
		out = append(out, k.byXUID[x].Name)
	}
	slices.Sort(out)
	return out
}

// flush writes the file if anything changed since it was last written.
func (k *knownPlayers) flush() error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.timer = nil
	if !k.dirty {
		return nil
	}
	return k.writeLocked()
}

// writeLocked writes the file, the accounts sorted by XUID.
func (k *knownPlayers) writeLocked() error {
	players := make([]knownPlayer, 0, len(k.byXUID))
	for _, p := range k.byXUID {
		players = append(players, p)
	}
	slices.SortFunc(players, func(a, b knownPlayer) int {
		if len(a.XUID) != len(b.XUID) {
			return len(a.XUID) - len(b.XUID)
		}
		return strings.Compare(a.XUID, b.XUID)
	})
	data, err := json.MarshalIndent(players, "", "  ")
	if err != nil {
		return fmt.Errorf("encode known players: %w", err)
	}
	if err := writeFileAtomic(k.path, append(data, '\n'), "known players"); err != nil {
		return err
	}
	k.dirty = false
	return nil
}

// close stops the scheduled write and writes the file, whether or not
// anything changed, so that it exists once a server keyed by XUID ran.
func (k *knownPlayers) close() error {
	if k == nil {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.closed = true
	if k.timer != nil {
		k.timer.Stop()
		k.timer = nil
	}
	return k.writeLocked()
}
