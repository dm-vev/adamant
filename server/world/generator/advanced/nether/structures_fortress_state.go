package nether

import (
	"fmt"
	"sort"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

const (
	fortressStateFormatVersion int32 = 1
	fortressStateDataVersion   int32 = 5023
)

type fortressStartKey struct {
	x, z int32
}

type savedFortressPieceState struct {
	Index   int32 `nbt:"index"`
	Chest   byte  `nbt:"chest"`
	Spawner byte  `nbt:"spawner"`
}

type savedFortressStartState struct {
	X      int32                     `nbt:"x"`
	Z      int32                     `nbt:"z"`
	Pieces []savedFortressPieceState `nbt:"pieces"`
}

type savedFortressState struct {
	FormatVersion int32                     `nbt:"format_version"`
	DataVersion   int32                     `nbt:"java_data_version"`
	Seed          int64                     `nbt:"seed"`
	Starts        []savedFortressStartState `nbt:"starts"`
}

type fortressStartState struct {
	mu      sync.Mutex
	pieces  []structure.StructurePieceState
	pending []savedFortressPieceState
}

type fortressStateStore struct {
	mu     sync.Mutex
	starts map[fortressStartKey]*fortressStartState
}

func (s *fortressStateStore) lookup(key fortressStartKey) *fortressStartState {
	s.mu.Lock()
	state := s.starts[key]
	s.mu.Unlock()
	return state
}

func (s *fortressStateStore) forStart(key fortressStartKey, pieces []bridgePiece) *fortressStartState {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.starts[key]; state != nil {
		return state
	}
	if s.starts == nil {
		s.starts = make(map[fortressStartKey]*fortressStartState)
	}
	state := &fortressStartState{pieces: make([]structure.StructurePieceState, len(pieces))}
	s.starts[key] = state
	return state
}

func fortressPieceHasChestFlag(piece bridgePiece) bool {
	switch p := piece.(type) {
	case *netherBridgeCorridor:
		return p.chest
	case *netherBridgeCorridor2:
		return p.chest
	default:
		return false
	}
}

func fortressPieceHasSpawnerFlag(piece bridgePiece) bool {
	_, ok := piece.(*netherBridgeThrone)
	return ok
}

func validateSavedFortressPiece(key fortressStartKey, saved savedFortressPieceState, pieces []bridgePiece) error {
	if saved.Index < 0 || int64(saved.Index) >= int64(len(pieces)) {
		return fmt.Errorf("fortress state piece %d at %d,%d is outside source graph", saved.Index, key.x, key.z)
	}
	if saved.Chest > 1 || saved.Spawner > 1 || saved.Chest != 0 && saved.Spawner != 0 || saved.Chest == 0 && saved.Spawner == 0 {
		return fmt.Errorf("invalid fortress piece state at %d,%d: %+v", key.x, key.z, saved)
	}
	piece := pieces[saved.Index]
	if saved.Chest != 0 && !fortressPieceHasChestFlag(piece) {
		return fmt.Errorf("fortress chest state piece %d at %d,%d does not have a source chest flag", saved.Index, key.x, key.z)
	}
	if saved.Spawner != 0 && !fortressPieceHasSpawnerFlag(piece) {
		return fmt.Errorf("fortress spawner state piece %d at %d,%d is not a throne", saved.Index, key.x, key.z)
	}
	return nil
}

// prepare applies decoded flags only after the exact source graph is available.
// The caller holds the start lock; no input index is used for allocation.
func (s *fortressStartState) prepare(key fortressStartKey, pieces []bridgePiece) error {
	if s.pieces == nil {
		s.pieces = make([]structure.StructurePieceState, len(pieces))
	} else if len(s.pieces) != len(pieces) {
		return fmt.Errorf("fortress state piece count at %d,%d differs from source graph", key.x, key.z)
	}
	for _, saved := range s.pending {
		if err := validateSavedFortressPiece(key, saved, pieces); err != nil {
			return err
		}
	}
	for _, saved := range s.pending {
		s.pieces[saved.Index] = structure.StructurePieceState{ChestPlaced: saved.Chest != 0, SpawnerPlaced: saved.Spawner != 0}
	}
	s.pending = nil
	return nil
}

// LoadGenerationState restores fortress postProcess flags before generation.
// Invalid input leaves the previously loaded state untouched.
func (g *Nether) LoadGenerationState(data []byte) error {
	var states map[fortressStartKey]*fortressStartState
	if len(data) != 0 {
		var saved savedFortressState
		if err := nbt.UnmarshalEncoding(data, &saved, nbt.LittleEndian); err != nil {
			return fmt.Errorf("decode fortress state: %w", err)
		}
		if saved.FormatVersion != fortressStateFormatVersion || saved.DataVersion != fortressStateDataVersion {
			return fmt.Errorf("unsupported fortress state version %d/%d", saved.FormatVersion, saved.DataVersion)
		}
		if saved.Seed != g.seed {
			return fmt.Errorf("fortress state seed %d differs from world seed %d", saved.Seed, g.seed)
		}
		states = make(map[fortressStartKey]*fortressStartState, len(saved.Starts))
		for _, start := range saved.Starts {
			key := fortressStartKey{x: start.X, z: start.Z}
			if _, exists := states[key]; exists {
				return fmt.Errorf("duplicate fortress state at %d,%d", key.x, key.z)
			}
			if len(start.Pieces) == 0 {
				return fmt.Errorf("empty fortress state at %d,%d", key.x, key.z)
			}
			seen := make(map[int32]struct{}, len(start.Pieces))
			for _, piece := range start.Pieces {
				if piece.Index < 0 || piece.Chest > 1 || piece.Spawner > 1 || piece.Chest != 0 && piece.Spawner != 0 || piece.Chest == 0 && piece.Spawner == 0 {
					return fmt.Errorf("invalid fortress piece state at %d,%d: %+v", key.x, key.z, piece)
				}
				if _, exists := seen[piece.Index]; exists {
					return fmt.Errorf("duplicate fortress piece state %d at %d,%d", piece.Index, key.x, key.z)
				}
				seen[piece.Index] = struct{}{}
			}
			states[key] = &fortressStartState{pending: append([]savedFortressPieceState(nil), start.Pieces...)}
		}
	}
	g.fortressStates.mu.Lock()
	g.fortressStates.starts = states
	g.fortressStates.mu.Unlock()
	return nil
}

// SaveGenerationState snapshots one-time fortress placement flags safely while
// generation workers may be rendering unrelated starts.
func (g *Nether) SaveGenerationState() ([]byte, error) {
	saved := savedFortressState{FormatVersion: fortressStateFormatVersion, DataVersion: fortressStateDataVersion, Seed: g.seed}
	store := &g.fortressStates
	store.mu.Lock()
	for key, state := range store.starts {
		start := savedFortressStartState{X: key.x, Z: key.z}
		state.mu.Lock()
		start.Pieces = append(start.Pieces, state.pending...)
		for index, flags := range state.pieces {
			if !flags.ChestPlaced && !flags.SpawnerPlaced {
				continue
			}
			piece := savedFortressPieceState{Index: int32(index)}
			if flags.ChestPlaced {
				piece.Chest = 1
			}
			if flags.SpawnerPlaced {
				piece.Spawner = 1
			}
			start.Pieces = append(start.Pieces, piece)
		}
		state.mu.Unlock()
		if len(start.Pieces) != 0 {
			sort.Slice(start.Pieces, func(i, j int) bool { return start.Pieces[i].Index < start.Pieces[j].Index })
			saved.Starts = append(saved.Starts, start)
		}
	}
	store.mu.Unlock()
	sort.Slice(saved.Starts, func(i, j int) bool {
		a, b := saved.Starts[i], saved.Starts[j]
		if a.X != b.X {
			return a.X < b.X
		}
		return a.Z < b.Z
	})
	return nbt.MarshalEncoding(saved, nbt.LittleEndian)
}

var _ world.PersistentGenerator = (*Nether)(nil)
