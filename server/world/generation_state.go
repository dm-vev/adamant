package world

import (
	"errors"
	"fmt"

	"github.com/df-mc/goleveldb/leveldb"
)

func generationStateNamespace(dim Dimension) string {
	return "generation:" + dimensionStorageKey(dim)
}

func loadGenerationState(provider Provider, generator Generator, dim Dimension) error {
	persistent, ok := generator.(PersistentGenerator)
	if !ok {
		return nil
	}
	store, ok := provider.(worldDataProvider)
	if !ok {
		return fmt.Errorf("persistent generator requires provider world data storage")
	}
	data, err := store.LoadWorldData(generationStateNamespace(dim))
	if errors.Is(err, leveldb.ErrNotFound) {
		return persistent.LoadGenerationState(nil)
	}
	if err != nil {
		return fmt.Errorf("load %s: %w", generationStateNamespace(dim), err)
	}
	return persistent.LoadGenerationState(data)
}

func saveGenerationState(provider Provider, generator Generator, dim Dimension) error {
	persistent, ok := generator.(PersistentGenerator)
	if !ok {
		return nil
	}
	store, ok := provider.(worldDataProvider)
	if !ok {
		return fmt.Errorf("persistent generator requires provider world data storage")
	}
	data, err := persistent.SaveGenerationState()
	if err != nil {
		return fmt.Errorf("encode %s: %w", generationStateNamespace(dim), err)
	}
	if err := store.SaveWorldData(generationStateNamespace(dim), data); err != nil {
		return fmt.Errorf("store %s: %w", generationStateNamespace(dim), err)
	}
	return nil
}
