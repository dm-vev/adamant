package world

import (
	"errors"

	"github.com/df-mc/goleveldb/leveldb"
)

// LoadWorldData reads an opaque Adamant-owned namespaced payload from the provider.
func (w *World) LoadWorldData(namespace string) ([]byte, error) {
	if w == nil {
		return nil, errors.New("world unavailable")
	}
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return nil, leveldb.ErrNotFound
	}
	return store.LoadWorldData(namespace)
}

// SaveWorldData writes an opaque Adamant-owned namespaced payload to the provider.
func (w *World) SaveWorldData(namespace string, data []byte) error {
	if w == nil {
		return errors.New("world unavailable")
	}
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return errors.New("world provider does not support namespaced data")
	}
	return store.SaveWorldData(namespace, data)
}
