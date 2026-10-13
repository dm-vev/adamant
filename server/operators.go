package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// operatorsList tracks operator names persisted as a JSON array.
//
// Operators bypass whitelist enforcement and receive command permissions.

type operatorsList struct {
	mu       sync.RWMutex
	players  map[string]struct{}
	order    []string
	filePath string
}

func loadOperators(path string) (*operatorsList, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("ops path must not be empty")
	}

	ops := &operatorsList{
		players:  make(map[string]struct{}),
		filePath: path,
	}
	if err := ops.reloadFromDisk(); err != nil {
		return nil, err
	}
	return ops, nil
}

func (o *operatorsList) isOperator(name string) bool {
	if o == nil {
		return false
	}

	key := normalizeName(name)
	if key == "" {
		return false
	}

	o.mu.RLock()
	_, ok := o.players[key]
	o.mu.RUnlock()
	return ok
}

func (o *operatorsList) reloadFromDisk() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	contents, err := os.ReadFile(o.filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			o.players = make(map[string]struct{})
			o.order = nil
			return o.writeLocked()
		}
		return fmt.Errorf("read ops: %w", err)
	}
	var names []string
	if err := json.Unmarshal(contents, &names); err != nil {
		return fmt.Errorf("decode ops: %w", err)
	}
	o.players, o.order = parseNameSlice(names)
	return nil
}

func (o *operatorsList) writeLocked() error {
	dir := filepath.Dir(o.filePath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("create ops directory: %w", err)
		}
	}
	contents, err := json.MarshalIndent(o.order, "", "  ")
	if err != nil {
		return fmt.Errorf("encode ops: %w", err)
	}
	if err := os.WriteFile(o.filePath, append(contents, '\n'), 0644); err != nil {
		return fmt.Errorf("write ops: %w", err)
	}
	return nil
}

func parseNameSlice(names []string) (map[string]struct{}, []string) {
	players := make(map[string]struct{}, len(names))
	order := make([]string, 0, len(names))
	for _, name := range names {
		key := normalizeName(name)
		if key == "" {
			continue
		}
		if _, exists := players[key]; exists {
			continue
		}
		players[key] = struct{}{}
		order = append(order, key)
	}
	return players, order
}
