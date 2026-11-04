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

// encodeGenerationState encodes the state of generator for a save of the
// world of the dimension passed, and returns the function that stores it.
// Both are nil if the generator keeps no state.
func encodeGenerationState(provider Provider, generator Generator, dim Dimension) (func() error, error) {
	persistent, ok := generator.(PersistentGenerator)
	if !ok {
		return nil, nil
	}
	store, ok := provider.(worldDataProvider)
	if !ok {
		return nil, fmt.Errorf("persistent generator requires provider world data storage")
	}
	data, err := persistent.SaveGenerationState()
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", generationStateNamespace(dim), err)
	}
	return func() error {
		if err := store.SaveWorldData(generationStateNamespace(dim), data); err != nil {
			return fmt.Errorf("store %s: %w", generationStateNamespace(dim), err)
		}
		return nil
	}, nil
}
