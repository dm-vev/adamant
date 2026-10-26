package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/df-mc/dragonfly/server/internal/vanilla"

	"github.com/df-mc/goleveldb/leveldb"
)

const lootSequenceNamespacePrefix = "loot-random-sequences:"

type lootSequenceData struct {
	Sequences map[string]vanilla.State `json:"sequences"`
}

// LootRandomSequence returns the source random stream for a loot-table
// sequence. A non-zero loot-table seed creates an isolated per-roll stream;
// zero uses the sequence persisted with the world.
func (tx *Tx) LootRandomSequence(sequence string, seed int64) *vanilla.Random {
	if tx.closed {
		panic("world.Tx: use of transaction after transaction finishes is not permitted")
	}
	if seed != 0 {
		r := vanilla.NewLegacyRandom(seed)
		return &r
	}
	if sequence == "" {
		panic("world.Tx.LootRandomSequence: empty sequence ID")
	}
	tx.lockShared()
	defer tx.unlockShared()
	if r := tx.region; r != nil {
		// Regions roll at once: each rolls from a stream of its own for the
		// sequence, seeded from the world's.
		if random := r.lootSequences[sequence]; random != nil {
			return random
		}
		world := tx.w.lootSequence(sequence)
		random := vanilla.NewLegacyRandom(world.NextLong())
		if r.lootSequences == nil {
			r.lootSequences = make(map[string]*vanilla.Random)
		}
		r.lootSequences[sequence] = &random
		return &random
	}
	return tx.w.lootSequence(sequence)
}

// lootSequence returns the stream of the world for a loot-table sequence.
func (w *World) lootSequence(sequence string) *vanilla.Random {
	if w.lootSequences == nil {
		w.lootSequences = make(map[string]*vanilla.Random)
	}
	if r := w.lootSequences[sequence]; r != nil {
		return r
	}
	r := vanilla.NewRandomSequence(w.Seed(), sequence)
	w.lootSequences[sequence] = &r
	return &r
}

func (w *World) lootRandomSequenceNamespace() string {
	return fmt.Sprintf("%s%s", lootSequenceNamespacePrefix, dimensionStorageKey(w.Dimension()))
}

func (w *World) loadLootRandomSequences() {
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok { return }
	data, err := provider.LoadWorldData(w.lootRandomSequenceNamespace())
	if errors.Is(err, leveldb.ErrNotFound) { return }
	if err != nil { w.conf.Log.Error("load loot random sequences: " + err.Error()); return }
	var saved lootSequenceData
	if err := json.Unmarshal(data, &saved); err != nil { w.conf.Log.Error("decode loot random sequences: " + err.Error()); return }
	for id, state := range saved.Sequences {
		r := vanilla.FromState(state)
		w.lootSequences[id] = &r
	}
}

func (w *World) saveLootRandomSequences() {
	if w.conf.ReadOnly { return }
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok { return }
	saved := lootSequenceData{Sequences: make(map[string]vanilla.State, len(w.lootSequences))}
	for id, random := range w.lootSequences { saved.Sequences[id] = random.State() }
	data, err := json.Marshal(saved)
	if err != nil { w.conf.Log.Error("encode loot random sequences: " + err.Error()); return }
	if err := provider.SaveWorldData(w.lootRandomSequenceNamespace(), data); err != nil { w.conf.Log.Error("save loot random sequences: " + err.Error()) }
}
