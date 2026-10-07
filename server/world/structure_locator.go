package world

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/dragonfly/server/block/cube"
)

// StructureLocation identifies a generated Java structure start and its
// structure-placement locate position. Position is not necessarily the start
// chunk centre: structure placements may define a locate offset.
type StructureLocation struct {
	ID        string
	StartChunk ChunkPos
	Position  cube.Pos
}

// StructureReference identifies a structure start already returned to a
// player-facing locate request. It is persisted separately from native chunk
// references because BDS and Java structure-reference records are not the same.
type StructureReference struct {
	DimensionID int
	StructureID string
	StartChunk  ChunkPos
}


// StructureLocator is implemented by Java world generators. isReferenced is
// called for each candidate start so a locate request can skip starts already
// referenced by earlier map/locate results.
type StructureLocator interface {
	FindNearestMapFeature(structureTag string, origin cube.Pos, searchRadius int, isReferenced func(StructureLocation) bool) (StructureLocation, bool, error)
}

type StructureAtFinder interface {
	FindStructureAt(structureTag string, position cube.Pos) (StructureLocation, bool, error)
}

// FindNearestMapFeature returns an unreferenced Java structure start when
// skipReferenced is true. A successful lookup does not consume the result;
// callers must call RecordStructureReference only after the operation that
// uses the location has been durably committed.
func (w *World) FindNearestMapFeature(structureTag string, origin cube.Pos, searchRadius int, skipReferenced bool) (StructureLocation, bool, error) {
	if w == nil {
		return StructureLocation{}, false, errors.New("find nearest map feature: nil world")
	}
	locator, ok := w.conf.Generator.(StructureLocator)
	if !ok {
		return StructureLocation{}, false, fmt.Errorf("find nearest map feature: generator %T does not support structure lookup", w.conf.Generator)
	}
	if searchRadius < 0 {
		return StructureLocation{}, false, fmt.Errorf("find nearest map feature: negative search radius %d", searchRadius)
	}
	if !skipReferenced {
		return locator.FindNearestMapFeature(structureTag, origin, searchRadius, nil)
	}
	w.structureReferenceQueryMu.Lock()
	defer w.structureReferenceQueryMu.Unlock()
	if err := w.loadStructureReferences(); err != nil {
		return StructureLocation{}, false, err
	}
	dimensionID, ok := DimensionID(w.conf.Dim)
	if !ok {
		return StructureLocation{}, false, fmt.Errorf("find nearest map feature: unregistered dimension %q", w.conf.Dim)
	}
	isReferenced := func(location StructureLocation) bool {
		reference := StructureReference{DimensionID: dimensionID, StructureID: location.ID, StartChunk: location.StartChunk}
		w.structureReferenceMu.Lock()
		_, exists := w.structureReferences[reference]
		w.structureReferenceMu.Unlock()
		return exists
	}
	return locator.FindNearestMapFeature(structureTag, origin, searchRadius, isReferenced)
}

// RecordStructureReference marks a structure start as consumed by a
// successfully committed locate/map operation. Persisting the record before
// updating the in-memory index ensures a provider failure does not consume it.
func (w *World) RecordStructureReference(location StructureLocation) error {
	if w == nil {
		return errors.New("record structure reference: nil world")
	}
	dimensionID, ok := DimensionID(w.conf.Dim)
	if !ok {
		return fmt.Errorf("record structure reference: unregistered dimension %q", w.conf.Dim)
	}
	if location.ID == "" {
		return errors.New("record structure reference: empty structure ID")
	}
	if err := w.loadStructureReferences(); err != nil {
		return err
	}
	reference := StructureReference{DimensionID: dimensionID, StructureID: location.ID, StartChunk: location.StartChunk}
	w.structureReferenceQueryMu.Lock()
	defer w.structureReferenceQueryMu.Unlock()
	w.structureReferenceMu.Lock()
	defer w.structureReferenceMu.Unlock()
	if _, exists := w.structureReferences[reference]; exists {
		return nil
	}
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return fmt.Errorf("record structure reference: provider %T cannot persist Java structure references", w.conf.Provider)
	}
	updated := make(map[StructureReference]struct{}, len(w.structureReferences)+1)
	for existing := range w.structureReferences {
		updated[existing] = struct{}{}
	}
	updated[reference] = struct{}{}
	data, err := encodeStructureReferences(updated)
	if err != nil {
		return err
	}
	if err := store.SaveWorldData(structureReferenceNamespace(dimensionID), data); err != nil {
		return fmt.Errorf("persist structure reference: %w", err)
	}
	w.structureReferences = updated
	return nil
}

// RecordStructureAt claims an existing structure start containing position.
// Exploration maps call this before locating their destination when their
// skip-known-structures option is enabled.
func (w *World) RecordStructureAt(structureTag string, position cube.Pos) (bool, error) {
	if w == nil {
		return false, errors.New("record structure at: nil world")
	}
	locator, ok := w.conf.Generator.(StructureAtFinder)
	if !ok {
		return false, fmt.Errorf("record structure at: generator %T does not support structure lookup", w.conf.Generator)
	}
	location, found, err := locator.FindStructureAt(structureTag, position)
	if err != nil || !found {
		return found, err
	}
	if err := w.RecordStructureReference(location); err != nil {
		return false, err
	}
	return true, nil
}

func (w *World) loadStructureReferences() error {
	w.structureReferenceMu.Lock()
	defer w.structureReferenceMu.Unlock()
	if w.structureReferencesLoaded {
		return w.structureReferencesErr
	}
	w.structureReferencesLoaded = true
	w.structureReferences = make(map[StructureReference]struct{})
	dimensionID, ok := DimensionID(w.conf.Dim)
	if !ok {
		w.structureReferencesErr = fmt.Errorf("load structure references: unregistered dimension %q", w.conf.Dim)
		return w.structureReferencesErr
	}
	store, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		w.structureReferencesErr = fmt.Errorf("provider %T cannot load Java structure references", w.conf.Provider)
		return w.structureReferencesErr
	}
	data, err := store.LoadWorldData(structureReferenceNamespace(dimensionID))
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil
	}
	if err != nil {
		w.structureReferencesErr = fmt.Errorf("load structure references: %w", err)
		return w.structureReferencesErr
	}
	references, err := decodeStructureReferences(data)
	if err != nil {
		w.structureReferencesErr = fmt.Errorf("decode structure references: %w", err)
		return w.structureReferencesErr
	}
	for _, reference := range references {
		if reference.DimensionID != dimensionID {
			w.structureReferencesErr = fmt.Errorf("Java structure reference has dimension %d in dimension %d namespace", reference.DimensionID, dimensionID)
			return w.structureReferencesErr
		}
		w.structureReferences[reference] = struct{}{}
	}
	return nil
}

const structureReferenceDataNamespace = "java_structure_references"

func structureReferenceNamespace(dimensionID int) string {
	return fmt.Sprintf("%s/%d", structureReferenceDataNamespace, dimensionID)
}

func encodeStructureReferences(references map[StructureReference]struct{}) ([]byte, error) {
	ordered := make([]StructureReference, 0, len(references))
	for reference := range references {
		if len(reference.StructureID) > 65535 {
			return nil, fmt.Errorf("Java structure ID too long: %d", len(reference.StructureID))
		}
		ordered = append(ordered, reference)
	}
	sort.Slice(ordered, func(i, j int) bool {
		a, b := ordered[i], ordered[j]
		if a.DimensionID != b.DimensionID {
			return a.DimensionID < b.DimensionID
		}
		if a.StructureID != b.StructureID {
			return a.StructureID < b.StructureID
		}
		if a.StartChunk[0] != b.StartChunk[0] {
			return a.StartChunk[0] < b.StartChunk[0]
		}
		return a.StartChunk[1] < b.StartChunk[1]
	})
	size := 12
	for _, reference := range ordered {
		size += 14 + len(reference.StructureID)
	}
	data := make([]byte, size)
	copy(data, "JSTRREF1")
	binary.LittleEndian.PutUint32(data[8:], uint32(len(ordered)))
	at := 12
	for _, reference := range ordered {
		binary.LittleEndian.PutUint32(data[at:], uint32(reference.DimensionID))
		binary.LittleEndian.PutUint16(data[at+4:], uint16(len(reference.StructureID)))
		copy(data[at+6:], reference.StructureID)
		chunkAt := at + 6 + len(reference.StructureID)
		binary.LittleEndian.PutUint32(data[chunkAt:], uint32(reference.StartChunk[0]))
		binary.LittleEndian.PutUint32(data[chunkAt+4:], uint32(reference.StartChunk[1]))
		at = chunkAt + 8
	}
	return data, nil
}

func decodeStructureReferences(data []byte) ([]StructureReference, error) {
	if len(data) < 12 || string(data[:8]) != "JSTRREF1" {
		return nil, errors.New("invalid Java structure reference data header")
	}
	count := int(binary.LittleEndian.Uint32(data[8:]))
	if count > (len(data)-12)/14 {
		return nil, fmt.Errorf("invalid Java structure reference count %d", count)
	}
	references := make([]StructureReference, 0, count)
	at := 12
	for i := 0; i < count; i++ {
		if len(data)-at < 6 {
			return nil, fmt.Errorf("truncated Java structure reference %d", i)
		}
		dimension := int(int32(binary.LittleEndian.Uint32(data[at:])))
		length := int(binary.LittleEndian.Uint16(data[at+4:]))
		at += 6
		if len(data)-at < length+8 {
			return nil, fmt.Errorf("truncated Java structure reference %d payload", i)
		}
		id := string(data[at : at+length])
		at += length
		references = append(references, StructureReference{
			DimensionID: dimension,
			StructureID: id,
			StartChunk: ChunkPos{
				int32(binary.LittleEndian.Uint32(data[at:])),
				int32(binary.LittleEndian.Uint32(data[at+4:])),
			},
		})
		at += 8
	}
	if at != len(data) {
		return nil, fmt.Errorf("unexpected trailing bytes in Java structure reference data: %d", len(data)-at)
	}
	return references, nil
}
