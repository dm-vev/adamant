package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
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

func (o *operatorsList) isOperator(name string) bool {
	if o == nil {
		return false
	}
	en, err := nameEntry(name)
	if err != nil {
		return false
	}

	o.mu.RLock()
	ok := o.players.has(en.key)
	o.mu.RUnlock()
	return ok
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
	var names []string
	if err := json.Unmarshal(contents, &names); err != nil {
		return fmt.Errorf("decode ops: %w", err)
	}
	o.players = newEntries(len(names))
	for _, name := range names {
		if en, err := nameEntry(name); err == nil {
			o.players.add(en)
		}
	}
	return nil
}

func (o *operatorsList) writeLocked() error {
	// A list never read nor changed writes as JSON null, as it always has.
	var names []string
	if o.players.order != nil {
		names = make([]string, 0, len(o.players.order))
	}
	for _, en := range o.players.order {
		names = append(names, en.key.String())
	}
	contents, err := json.MarshalIndent(names, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ops: %w", err)
	}
	return writeFileAtomic(o.filePath, append(contents, '\n'), "ops")
}
