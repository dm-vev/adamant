package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/identity"
)

// operatorsList tracks operator names persisted as a JSON array.
//
// Operators bypass whitelist enforcement and receive command permissions.

type operatorsList struct {
	mu       sync.RWMutex
	players  entries
	filePath string
}

func loadOperators(path string) (*operatorsList, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("ops path must not be empty")
	}

	ops := &operatorsList{filePath: path}
	if err := ops.reloadFromDisk(); err != nil {
		return nil, err
	}
	return ops, nil
}

// isOperator reports whether the player of the name passed is an operator
// of a server keyed by name: by its name, or as the account an entry of a
// server keyed by XUID says last had the name.
func (o *operatorsList) isOperator(name string) bool {
	if o == nil {
		return false
	}
	o.mu.RLock()
	_, ok := o.players.match(name)
	o.mu.RUnlock()
	return ok
}

// hasKey reports whether the list holds the key passed.
func (o *operatorsList) hasKey(k identity.Key) bool {
	if o == nil {
		return false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.players.has(k)
}

// view runs f with the entries of the operators, which f must not change.
func (o *operatorsList) view(f func(e *entries)) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	f(&o.players)
}

// bind binds the pending entry of the name passed to the account x, which
// joined with it, and writes the file. It returns the pending entry and
// reports whether there was one.
func (o *operatorsList) bind(x identity.Key, name string) (entry, bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	pending, ok := o.players.bind(x, name)
	if !ok {
		return entry{}, false, nil
	}
	return pending, true, o.writeLocked()
}

// add makes the player of en an operator and writes the file. It reports
// whether the player was not one; it stays one in memory if the write
// fails.
func (o *operatorsList) add(en entry) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.players.add(en) {
		return false, nil
	}
	return true, o.writeLocked()
}

// remove takes the player of the entry passed off the operators and writes
// the file. It reports whether the player was one.
func (o *operatorsList) remove(en entry) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.players.remove(en.key) {
		return false, nil
	}
	return true, o.writeLocked()
}

// keys returns the keys of the operators, in the order of the file.
func (o *operatorsList) keys() []string {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.players.keys()
}

func (o *operatorsList) reloadFromDisk() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	contents, err := os.ReadFile(o.filePath)
	if err != nil {
		if errNotExist(err) {
			o.players = entries{}
			return o.writeLocked()
		}
		return fmt.Errorf("read ops: %w", err)
	}
	players, err := decodeOps(contents)
	if err != nil {
		return fmt.Errorf("decode ops: %w", err)
	}
	o.players = players
	return nil
}

// opsAccount is an operator of ops.json keyed by Xbox account.
type opsAccount struct {
	XUID string `json:"xuid"`
	Name string `json:"name"`
}

// decodeOps reads ops.json: a JSON array of names, and of objects with the
// XUID and the name of an Xbox account.
func decodeOps(contents []byte) (entries, error) {
	var names []string
	if err := json.Unmarshal(contents, &names); err == nil {
		players := newEntries(len(names))
		for _, name := range names {
			if en, err := nameEntry(name); err == nil {
				players.add(en)
			}
		}
		return players, nil
	} else {
		var raw []json.RawMessage
		if json.Unmarshal(contents, &raw) != nil {
			return entries{}, err
		}
		players := newEntries(len(raw))
		for _, r := range raw {
			var name string
			if json.Unmarshal(r, &name) == nil {
				if en, err := nameEntry(name); err == nil {
					players.add(en)
				}
				continue
			}
			var acc opsAccount
			if err := json.Unmarshal(r, &acc); err != nil {
				return entries{}, fmt.Errorf("operator %s: %w", r, err)
			}
			en, err := xuidEntry(acc.XUID, acc.Name)
			if err != nil {
				return entries{}, fmt.Errorf("operator %s: %w", r, err)
			}
			players.add(en)
		}
		return players, nil
	}
}

func (o *operatorsList) writeLocked() error {
	// A list never read nor changed writes as JSON null, as it always has,
	// and a list of names alone as an array of strings.
	var v any
	if o.players.accounts() == 0 {
		var names []string
		if o.players.order != nil {
			names = make([]string, 0, len(o.players.order))
		}
		for _, en := range o.players.order {
			names = append(names, en.key.String())
		}
		v = names
	} else {
		all := make([]any, 0, len(o.players.order))
		for _, en := range o.players.order {
			if x, ok := en.key.XUID(); ok {
				all = append(all, opsAccount{XUID: x, Name: en.name})
			} else {
				all = append(all, en.key.String())
			}
		}
		v = all
	}
	contents, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ops: %w", err)
	}
	return writeFileAtomic(o.filePath, append(contents, '\n'), "ops")
}
