package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/cmd"
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

// reload reads the file again, as /permission reload does. A file that
// cannot be read or decoded leaves the list as it was.
func (o *operatorsList) reload() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	contents, err := os.ReadFile(o.filePath)
	if errNotExist(err) {
		o.players = entries{}
		return nil
	}
	if err != nil {
		return fmt.Errorf("read ops: %w", err)
	}
	players, err := decodeOps(contents)
	if err != nil {
		return fmt.Errorf("decode ops: %w", err)
	}
	o.players = players
	return nil
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

// opsAccount is an operator of ops.json written as an object: an Xbox
// account by its XUID and the name its player last had, or a name, with a
// level of its own and bypassesPlayerLimit, as Java writes them, if set.
// An account with neither writes as it always has: "xuid", then "name".
type opsAccount struct {
	XUID                string `json:"xuid,omitempty"`
	Name                string `json:"name"`
	Level               *int   `json:"level,omitempty"`
	BypassesPlayerLimit bool   `json:"bypassesPlayerLimit,omitempty"`
}

// entry returns the operator of the object.
func (o opsAccount) entry() (entry, error) {
	var en entry
	var err error
	if o.XUID != "" {
		en, err = xuidEntry(o.XUID, o.Name)
	} else {
		en, err = nameEntry(o.Name)
	}
	if err != nil {
		return entry{}, err
	}
	if o.Level != nil {
		if *o.Level < 0 || *o.Level > int(cmd.MaxLevel) {
			return entry{}, fmt.Errorf("level %d: want 0 to %d", *o.Level, cmd.MaxLevel)
		}
		en.op.level, en.op.hasLevel = cmd.Level(*o.Level), true
	}
	en.op.bypass = o.BypassesPlayerLimit
	return en, nil
}

// opsAccountOf returns the object of an operator ops.json writes as one.
func opsAccountOf(en entry) opsAccount {
	o := opsAccount{Name: en.key.String(), BypassesPlayerLimit: en.op.bypass}
	if x, ok := en.key.XUID(); ok {
		o.XUID, o.Name = x, en.name
	}
	if en.op.hasLevel {
		l := int(en.op.level)
		o.Level = &l
	}
	return o
}

// decodeOps reads ops.json: a JSON array of names, and of objects of an
// Xbox account or a name, with a level and bypassesPlayerLimit if set.
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
			var obj opsAccount
			if err := json.Unmarshal(r, &obj); err != nil {
				return entries{}, fmt.Errorf("operator %s: %w", r, err)
			}
			en, err := obj.entry()
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
	// and a list of names alone as an array of strings. An operator with a
	// level of its own or bypassesPlayerLimit writes as an object.
	var v any
	if o.players.accounts() == 0 && !o.anySet() {
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
			if _, ok := en.key.XUID(); ok || en.op.set() {
				all = append(all, opsAccountOf(en))
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

// anySet reports whether an operator has a level of its own or
// bypassesPlayerLimit.
func (o *operatorsList) anySet() bool {
	for _, en := range o.players.order {
		if en.op.set() {
			return true
		}
	}
	return false
}

// entry returns the operator of the key passed.
func (o *operatorsList) entry(k identity.Key) (entry, bool) {
	if o == nil {
		return entry{}, false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.players.get(k)
}

// match returns the operator of the name passed on a server keyed by name,
// as isOperator finds it.
func (o *operatorsList) match(name string) (entry, bool) {
	if o == nil {
		return entry{}, false
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.players.match(name)
}

// set replaces the fields of the operator of en, or adds en with them,
// and writes the file, unless the operator has them already. It reports
// whether it changed the list; the list is left as it was if the write
// fails.
func (o *operatorsList) set(en entry) (bool, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if old, ok := o.players.get(en.key); ok && old.op == en.op {
		return false, nil
	}
	before := o.players.clone()
	o.players.put(en)
	if err := o.writeLocked(); err != nil {
		o.players = before
		return false, err
	}
	return true, nil
}

// update runs f with the entries of the list, and writes the file if f
// reports it changed them.
func (o *operatorsList) update(f func(e *entries) bool) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !f(&o.players) {
		return nil
	}
	return o.writeLocked()
}

// path returns the path of the file of the list.
func (o *operatorsList) path() string { return o.filePath }
