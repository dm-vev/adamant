package world

import (
	"math"
	"sync"

	"github.com/df-mc/dragonfly/server/block/cube"
)

const maxInactivePositionTrackingEntries = 1024

// PositionTrackingBlock is implemented by blocks whose position may be tracked by the Bedrock client.
type PositionTrackingBlock interface {
	Block
	TrackingHandle() int32
	WithTrackingHandle(handle int32) Block
}

type trackedPosition struct {
	pos    cube.Pos
	dim    int
	active bool
}

// PositionTrackingEntry is a persistent entry in the position tracking database.
type PositionTrackingEntry struct {
	Handle    int32
	Position  cube.Pos
	Dimension int
	Active    bool
}

// PositionTrackingDestroyAction notifies viewers that a tracked block no longer exists.
type PositionTrackingDestroyAction struct{ Handle int32 }

// BlockAction implements BlockAction.
func (PositionTrackingDestroyAction) BlockAction() {}

// PositionTrackingUpdateAction provides the target of a tracking handle to a viewer.
type PositionTrackingUpdateAction struct {
	Handle    int32
	Position  cube.Pos
	Dimension int
}

// BlockAction implements BlockAction.
func (PositionTrackingUpdateAction) BlockAction() {}

// PositionTracker holds Bedrock position tracking handles shared by the dimensions of a world.
type PositionTracker struct {
	mu         sync.Mutex
	next       int32
	byHandle   map[int32]trackedPosition
	byPosition map[[4]int]int32
	inactive   []int32
}

// NewPositionTracker returns an initialised PositionTracker.
func NewPositionTracker() *PositionTracker {
	return &PositionTracker{byHandle: map[int32]trackedPosition{}, byPosition: map[[4]int]int32{}}
}

func (w *World) positionTracker() *PositionTracker {
	return w.providerUse.positionTracker()
}

// PositionTrackingData is a persistent snapshot of the position tracking database.
type PositionTrackingData struct {
	Next    int32
	Entries []PositionTrackingEntry
}

func (t *PositionTracker) data() PositionTrackingData {
	t.mu.Lock()
	defer t.mu.Unlock()
	data := PositionTrackingData{Next: t.next, Entries: make([]PositionTrackingEntry, 0, len(t.byHandle))}
	for handle, entry := range t.byHandle {
		data.Entries = append(data.Entries, PositionTrackingEntry{Handle: handle, Position: entry.pos, Dimension: entry.dim, Active: entry.active})
	}
	return data
}

func (t *PositionTracker) load(data PositionTrackingData) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.next = data.Next
	if t.next < 0 {
		t.next = 0
	}
	t.byHandle = map[int32]trackedPosition{}
	t.byPosition = map[[4]int]int32{}
	t.inactive = nil
	for _, entry := range data.Entries {
		if entry.Handle <= 0 {
			continue
		}
		key := positionTrackingKey(entry.Dimension, entry.Position)
		if old, ok := t.byHandle[entry.Handle]; ok {
			oldKey := positionTrackingKey(old.dim, old.pos)
			if t.byPosition[oldKey] == entry.Handle {
				delete(t.byPosition, oldKey)
			}
			t.removeInactive(entry.Handle)
		}
		if oldHandle := t.byPosition[key]; entry.Active && oldHandle != 0 && oldHandle != entry.Handle {
			old := t.byHandle[oldHandle]
			old.active = false
			t.byHandle[oldHandle] = old
			t.inactive = append(t.inactive, oldHandle)
		}
		t.byHandle[entry.Handle] = trackedPosition{pos: entry.Position, dim: entry.Dimension, active: entry.Active}
		if entry.Active {
			t.byPosition[key] = entry.Handle
		} else {
			t.inactive = append(t.inactive, entry.Handle)
		}
		t.pruneInactive()
		if entry.Handle > t.next {
			t.next = entry.Handle
		}
	}
}

// TrackPosition activates a tracking handle for pos. Active handles at the same position are reused when handle is 0.
func (w *World) TrackPosition(pos cube.Pos, handle int32) int32 {
	dim, ok := DimensionID(w.Dimension())
	if !ok {
		return 0
	}
	t := w.positionTracker()
	t.mu.Lock()
	defer t.mu.Unlock()
	if handle < 0 {
		handle = 0
	}
	key := positionTrackingKey(dim, pos)
	if existing := t.byPosition[key]; handle == 0 && existing != 0 {
		handle = existing
	}
	if handle == 0 {
		for handle == 0 {
			if t.next == math.MaxInt32 {
				t.next = 0
			}
			t.next++
			handle = t.next
			if _, exists := t.byHandle[handle]; exists {
				handle = 0
			}
		}
	}
	if entry, exists := t.byHandle[handle]; exists {
		oldKey := positionTrackingKey(entry.dim, entry.pos)
		if t.byPosition[oldKey] == handle {
			delete(t.byPosition, oldKey)
		}
		if !entry.active {
			t.removeInactive(handle)
		}
	}
	if oldHandle := t.byPosition[key]; oldHandle != 0 && oldHandle != handle {
		old := t.byHandle[oldHandle]
		old.active = false
		t.byHandle[oldHandle] = old
		t.inactive = append(t.inactive, oldHandle)
		t.pruneInactive()
	}
	if handle > t.next {
		t.next = handle
	}
	t.byPosition[key] = handle
	t.byHandle[handle] = trackedPosition{pos: pos, dim: dim, active: true}
	return handle
}

// PositionTrackingHandleAt returns the tracking handle associated with pos.
func (w *World) PositionTrackingHandleAt(pos cube.Pos) int32 {
	dim, ok := DimensionID(w.Dimension())
	if !ok {
		return 0
	}
	t := w.positionTracker()
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.byPosition[positionTrackingKey(dim, pos)]
}

// UntrackPosition marks the tracking handle at pos as unavailable.
func (w *World) UntrackPosition(pos cube.Pos) {
	dim, ok := DimensionID(w.Dimension())
	if !ok {
		return
	}
	t := w.positionTracker()
	t.mu.Lock()
	key := positionTrackingKey(dim, pos)
	handle := t.byPosition[key]
	if entry, exists := t.byHandle[handle]; exists && entry.active {
		entry.active = false
		t.byHandle[handle] = entry
		delete(t.byPosition, key)
		t.inactive = append(t.inactive, handle)
		t.pruneInactive()
	} else {
		handle = 0
	}
	t.mu.Unlock()
	if handle == 0 {
		return
	}
	action := PositionTrackingDestroyAction{Handle: handle}
	w.viewerMu.Lock()
	viewers := make(map[Viewer]struct{}, len(w.viewers))
	for _, viewer := range w.viewers {
		viewers[viewer] = struct{}{}
	}
	w.viewerMu.Unlock()
	for viewer := range viewers {
		viewer.ViewBlockAction(pos, action)
	}
}

// TrackedPosition looks up an active position tracking handle.
func (w *World) TrackedPosition(handle int32) (cube.Pos, int, bool) {
	t := w.positionTracker()
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.byHandle[handle]
	if !ok || !entry.active {
		return cube.Pos{}, 0, false
	}
	return entry.pos, entry.dim, true
}

func positionTrackingKey(dim int, pos cube.Pos) [4]int {
	return [4]int{dim, pos[0], pos[1], pos[2]}
}

func (t *PositionTracker) removeInactive(handle int32) {
	for i, inactive := range t.inactive {
		if inactive == handle {
			t.inactive = append(t.inactive[:i], t.inactive[i+1:]...)
			return
		}
	}
}

func (t *PositionTracker) pruneInactive() {
	for len(t.inactive) > maxInactivePositionTrackingEntries {
		handle := t.inactive[0]
		t.inactive = t.inactive[1:]
		entry, ok := t.byHandle[handle]
		if !ok || entry.active {
			continue
		}
		delete(t.byHandle, handle)
		key := positionTrackingKey(entry.dim, entry.pos)
		if t.byPosition[key] == handle {
			delete(t.byPosition, key)
		}
	}
}
