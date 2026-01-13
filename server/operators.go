package server

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// operatorsList tracks operator names loaded from a Nukkit-style ops.txt file.
//
// Lumi/Nukkit treats operators as exempt from whitelist checks. We keep this list separate from Whitelist so that the
// whitelist feature stays self-contained and operator behaviour can be composed without changing the public API.
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

	players, order := parseNameList(contents)
	o.players = players
	o.order = order
	return nil
}

func (o *operatorsList) writeLocked() error {
	dir := filepath.Dir(o.filePath)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("create ops directory: %w", err)
		}
	}

	var b strings.Builder
	for _, name := range o.order {
		b.WriteString(name)
		b.WriteString("\r\n")
	}
	if err := os.WriteFile(o.filePath, []byte(b.String()), 0644); err != nil {
		return fmt.Errorf("write ops: %w", err)
	}
	return nil
}

func parseNameList(contents []byte) (map[string]struct{}, []string) {
	players := make(map[string]struct{})

	content := strings.ReplaceAll(string(contents), "\r\n", "\n")
	lines := strings.Split(content, "\n")
	order := make([]string, 0, len(lines))
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
	return players, order
}
