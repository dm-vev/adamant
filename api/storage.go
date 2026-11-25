package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml"
)

// Storage provides safe, atomic file, JSON, and TOML configuration persistence
// scoped to a plugin data directory.
type Storage struct {
	dir string
}

// NewStorage creates a Storage rooted at dir.
func NewStorage(dir string) *Storage {
	return &Storage{dir: filepath.Clean(dir)}
}

// Dir returns the root directory of this Storage.
func (s *Storage) Dir() string {
	if s == nil {
		return ""
	}
	return s.dir
}

// Path resolves a relative filename within the storage root, rejecting path
// traversal outside the plugin directory.
func (s *Storage) Path(rel string) (string, error) {
	if s == nil || s.dir == "" {
		return "", errors.New("api storage: empty storage directory")
	}
	if rel == "" || filepath.IsAbs(rel) {
		return "", fmt.Errorf("api storage: invalid relative path %q", rel)
	}
	full := filepath.Clean(filepath.Join(s.dir, rel))
	root := filepath.Clean(s.dir)
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", fmt.Errorf("api storage: path %q escapes root directory", rel)
	}
	return full, nil
}

// Exists reports whether rel exists inside the storage directory.
func (s *Storage) Exists(rel string) bool {
	p, err := s.Path(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// ReadBytes reads raw bytes from rel inside the storage directory.
func (s *Storage) ReadBytes(rel string) ([]byte, error) {
	p, err := s.Path(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

// WriteBytes atomically writes data to rel (creating parent directories as
// needed) via a temporary file and rename.
func (s *Storage) WriteBytes(rel string, data []byte) error {
	p, err := s.Path(rel)
	if err != nil {
		return err
	}
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, p)
}

// Delete removes rel from the storage directory if present.
func (s *Storage) Delete(rel string) error {
	p, err := s.Path(rel)
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SaveJSON marshals v with indentation and atomically writes it to rel.
func SaveJSON[T any](s *Storage, rel string, v T) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return s.WriteBytes(rel, append(b, '\n'))
}

// LoadJSON reads and unmarshals JSON from rel into a T value.
func LoadJSON[T any](s *Storage, rel string) (T, error) {
	var zero T
	b, err := s.ReadBytes(rel)
	if err != nil {
		return zero, err
	}
	if err := json.Unmarshal(b, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// LoadOrDefaultJSON loads rel if present; if missing it saves def and returns def.
func LoadOrDefaultJSON[T any](s *Storage, rel string, def T) (T, error) {
	v, err := LoadJSON[T](s, rel)
	if errors.Is(err, os.ErrNotExist) {
		if saveErr := SaveJSON(s, rel, def); saveErr != nil {
			return def, saveErr
		}
		return def, nil
	}
	return v, err
}

// SaveTOML marshals v to TOML and atomically writes it to rel.
func SaveTOML[T any](s *Storage, rel string, v T) error {
	b, err := toml.Marshal(v)
	if err != nil {
		return err
	}
	return s.WriteBytes(rel, b)
}

// LoadTOML reads and unmarshals TOML from rel into a T value.
func LoadTOML[T any](s *Storage, rel string) (T, error) {
	var zero T
	b, err := s.ReadBytes(rel)
	if err != nil {
		return zero, err
	}
	if err := toml.Unmarshal(b, &zero); err != nil {
		return zero, err
	}
	return zero, nil
}

// LoadOrDefaultTOML loads rel if present; if missing it saves def and returns def.
func LoadOrDefaultTOML[T any](s *Storage, rel string, def T) (T, error) {
	v, err := LoadTOML[T](s, rel)
	if errors.Is(err, os.ErrNotExist) {
		if saveErr := SaveTOML(s, rel, def); saveErr != nil {
			return def, saveErr
		}
		return def, nil
	}
	return v, err
}
