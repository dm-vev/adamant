package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/df-mc/dragonfly/server/internal/java26"

	"github.com/df-mc/goleveldb/leveldb"
)

const lootSequenceNamespacePrefix = "loot-random-sequences:"

type lootSequenceData struct {
	Sequences map[string]java26.State `json:"sequences"`
}

// LootRandomSequence returns the source random stream for a loot-table
// sequence. A non-zero loot-table seed creates an isolated per-roll stream;
// zero uses the sequence persisted with the world.
func (tx *Tx) LootRandomSequence(sequence string, seed int64) *java26.Random {
	if tx.closed { panic("world.Tx: use of transaction after transaction finishes is not permitted") }
	if seed != 0 {
		r := java26.NewXoroshiroRandom(seed)
		return &r
	}
	if sequence == "" { panic("world.Tx.LootRandomSequence: empty sequence ID") }
	if tx.w.lootSequences == nil { tx.w.lootSequences = make(map[string]*java26.Random) }
	if r := tx.w.lootSequences[sequence]; r != nil { return r }
	r := java26.NewRandomSequence(tx.w.Seed(), sequence)
	tx.w.lootSequences[sequence] = &r
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
		r := java26.FromState(state)
		w.lootSequences[id] = &r
	}
}

func (w *World) saveLootRandomSequences() {
	if w.conf.ReadOnly { return }
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok { return }
	saved := lootSequenceData{Sequences: make(map[string]java26.State, len(w.lootSequences))}
	for id, random := range w.lootSequences { saved.Sequences[id] = random.State() }
	data, err := json.Marshal(saved)
	if err != nil { w.conf.Log.Error("encode loot random sequences: " + err.Error()); return }
	if err := provider.SaveWorldData(w.lootRandomSequenceNamespace(), data); err != nil { w.conf.Log.Error("save loot random sequences: " + err.Error()) }
}
