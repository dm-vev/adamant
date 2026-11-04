package world

import (
	"errors"

	"github.com/df-mc/goleveldb/leveldb"
)

// LoadWorldData reads an opaque Adamant-owned namespaced payload: the one
// saved last, which may still wait in memory for the next save of the world
// (Staging), or else the provider's.
func (w *World) LoadWorldData(namespace string) ([]byte, error) {
	if w == nil {
		return nil, errors.New("world unavailable")
	}
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return nil, leveldb.ErrNotFound
	}
	if data, ok := w.providerUse.staging.data(namespace); ok {
		return data, nil
	}
	return store.LoadWorldData(namespace)
}

// SaveWorldData saves an opaque Adamant-owned namespaced payload. It is
// staged until the next save of the world, which writes it to the provider
// with the rest of the world as it is then (Staging); LoadWorldData reads
// it back meanwhile.
func (w *World) SaveWorldData(namespace string, data []byte) error {
	if w == nil {
		return errors.New("world unavailable")
	}
	if _, ok := w.conf.Provider.(worldDataProvider); !ok {
		return errors.New("world provider does not support namespaced data")
	}
	w.providerUse.staging.stageData(namespace, data)
	return nil
}

// writeWorldData writes a namespaced payload to the provider at once, as a
// save of the world does with the data it captured.
func (w *World) writeWorldData(namespace string, data []byte) error {
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return errors.New("world provider does not support namespaced data")
	}
	return store.SaveWorldData(namespace, data)
}
