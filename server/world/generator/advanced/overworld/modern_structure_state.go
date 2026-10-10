package overworld

import (
	"fmt"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

// Source graphs remain immutable and may be evicted independently of their
// postProcess flags. A start lock serializes its pieces across chunk workers.
type modernStructureStateStore struct {
	mu     sync.Mutex
	starts map[modernStructureStartKey]*modernStructureStartState
}

type modernStructureStartState struct {
	mu      sync.Mutex
	pieces  []javastructure.StructurePieceState
	pending []savedStructurePieceState
}

func (s *modernStructureStateStore) lookup(key modernStructureStartKey) *modernStructureStartState {
	s.mu.Lock()
	state := s.starts[key]
	s.mu.Unlock()
	return state
}

func (s *modernStructureStateStore) forStart(start modernStructureStart) *modernStructureStartState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.starts[start.key]; state != nil {
		return state
	}
	if s.starts == nil {
		s.starts = make(map[modernStructureStartKey]*modernStructureStartState)
	}
	state := &modernStructureStartState{pieces: make([]javastructure.StructurePieceState, len(start.pieces))}
	s.starts[start.key] = state
	return state
}

// prepare applies decoded flags only after the matching source graph exists.
// The caller holds the start lock.
func (s *modernStructureStartState) prepare(start modernStructureStart) error {
	if len(s.pieces) == 0 {
		s.pieces = make([]javastructure.StructurePieceState, len(start.pieces))
	}
	if len(s.pieces) != len(start.pieces) {
		return fmt.Errorf("Java structure state piece count at %s %d,%d differs from source graph", start.key.set, start.key.x, start.key.z)
	}
	for _, piece := range s.pending {
		if int(piece.Index) >= len(s.pieces) {
			return fmt.Errorf("Java structure state piece %d at %s %d,%d is outside source graph", piece.Index, start.key.set, start.key.x, start.key.z)
		}
		s.pieces[piece.Index] = javastructure.StructurePieceState{
			ChestPlaced: piece.Chest != 0, SpawnerPlaced: piece.Spawner != 0,
			GroundHeightSet: piece.GroundHeightSet != 0, GroundHeight: piece.GroundHeight,
			LootPlaced: piece.LootPlaced, EntitiesSpawned: piece.EntitiesSpawned,
		}
	}
	s.pending = nil
	return nil
}

func hasMutableStructurePieceState(pieces []modernStructurePiece) bool {
	for _, piece := range pieces {
		switch p := piece.(type) {
		case minePiece:
			if p.kind == "corridor" && p.spider {
				return true
			}
		case strongholdPiece:
			if p.kind == "chest" || p.kind == "portal" {
				return true
			}
		case modernJungleTemple, modernSwampHutPiece:
			return true
		}
	}
	return false
}

const structureStateFormatVersion int32 = 1
const structureStateJavaDataVersion int32 = 5023

type savedStructurePieceState struct {
	Index           int32 `nbt:"index"`
	Chest           byte  `nbt:"chest"`
	Spawner         byte  `nbt:"spawner"`
	GroundHeightSet byte  `nbt:"ground_height_set"`
	GroundHeight    int32 `nbt:"ground_height"`
	LootPlaced      byte  `nbt:"loot_placed"`
	EntitiesSpawned byte  `nbt:"entities_spawned"`
}

type savedStructureStartState struct {
	Set    string                     `nbt:"set"`
	X      int32                      `nbt:"x"`
	Z      int32                      `nbt:"z"`
	Pieces []savedStructurePieceState `nbt:"pieces"`
}

type savedStructureState struct {
	FormatVersion int32                      `nbt:"format_version"`
	DataVersion   int32                      `nbt:"java_data_version"`
	Seed          int64                      `nbt:"seed"`
	Starts        []savedStructureStartState `nbt:"starts"`
}

// LoadGenerationState restores Java structure-start flags before generation.
// Graphs are rebuilt from the seed; their state is not tied to the graph cache.
func (g *Overworld) LoadGenerationState(data []byte) error {
	var states map[modernStructureStartKey]*modernStructureStartState
	if len(data) != 0 {
		var saved savedStructureState
		if err := nbt.UnmarshalEncoding(data, &saved, nbt.LittleEndian); err != nil {
			return fmt.Errorf("decode Java structure state: %w", err)
		}
		if saved.FormatVersion != structureStateFormatVersion || saved.DataVersion != structureStateJavaDataVersion {
			return fmt.Errorf("unsupported Java structure state version %d/%d", saved.FormatVersion, saved.DataVersion)
		}
		if saved.Seed != g.seed {
			return fmt.Errorf("Java structure state seed %d differs from world seed %d", saved.Seed, g.seed)
		}
		states = make(map[modernStructureStartKey]*modernStructureStartState, len(saved.Starts))
		for _, start := range saved.Starts {
			key := modernStructureStartKey{set: start.Set, x: start.X, z: start.Z}
			if _, exists := states[key]; exists {
				return fmt.Errorf("duplicate Java structure state at %s %d,%d", key.set, key.x, key.z)
			}
			for _, piece := range start.Pieces {
				if piece.Index < 0 || piece.Chest > 1 || piece.Spawner > 1 || piece.GroundHeightSet > 1 || piece.LootPlaced > 15 || piece.EntitiesSpawned > 3 {
					return fmt.Errorf("invalid Java structure piece state at %s %d,%d: %+v", key.set, key.x, key.z, piece)
				}
			}
			state := &modernStructureStartState{pending: start.Pieces}
			states[key] = state
		}
	}
	s := &g.modernStructures.pieceStates
	s.mu.Lock()
	s.starts = states
	s.mu.Unlock()
	return nil
}

// SaveGenerationState snapshots one-time Java placement flags independently of
// the bounded immutable start cache. It is safe while generation is running.
func (g *Overworld) SaveGenerationState() ([]byte, error) {
	saved := savedStructureState{FormatVersion: structureStateFormatVersion, DataVersion: structureStateJavaDataVersion, Seed: g.seed}
	s := &g.modernStructures.pieceStates
	s.mu.Lock()
	for key, state := range s.starts {
		start := savedStructureStartState{Set: key.set, X: key.x, Z: key.z}
		state.mu.Lock()
		start.Pieces = append(start.Pieces, state.pending...)
		for index, flags := range state.pieces {
			if !flags.ChestPlaced && !flags.SpawnerPlaced && !flags.GroundHeightSet && flags.LootPlaced == 0 && flags.EntitiesSpawned == 0 {
				continue
			}
			piece := savedStructurePieceState{Index: int32(index)}
			if flags.ChestPlaced {
				piece.Chest = 1
			}
			if flags.SpawnerPlaced {
				piece.Spawner = 1
			}
			if flags.GroundHeightSet {
				piece.GroundHeightSet = 1
				piece.GroundHeight = flags.GroundHeight
			}
			piece.LootPlaced, piece.EntitiesSpawned = flags.LootPlaced, flags.EntitiesSpawned
			start.Pieces = append(start.Pieces, piece)
		}
		state.mu.Unlock()
		if len(start.Pieces) != 0 {
			saved.Starts = append(saved.Starts, start)
		}
	}
	s.mu.Unlock()
	sort.Slice(saved.Starts, func(i, j int) bool {
		a, b := saved.Starts[i], saved.Starts[j]
		if a.Set != b.Set {
			return a.Set < b.Set
		}
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Z < b.Z
	})
	return nbt.MarshalEncoding(saved, nbt.LittleEndian)
}
