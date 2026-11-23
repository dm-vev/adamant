package event

import (
	"cmp"
	"maps"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"unsafe"
)

// Priority orders event listeners from earliest (PriorityLowest) to latest
// (PriorityMonitor).
type Priority int8

const (
	PriorityLowest Priority = iota
	PriorityLow
	PriorityNormal
	PriorityHigh
	PriorityHighest
	// PriorityMonitor observes the final outcome of an event after all
	// lower-priority listeners have run and is always invoked even when a
	// cancellable event has been cancelled.
	PriorityMonitor
)

// Cancellable may be embedded into an event struct so listeners can cancel the
// action the event represents.
type Cancellable struct {
	cancelled bool
}

// Cancel marks the event as cancelled.
func (c *Cancellable) Cancel() {
	if c != nil {
		c.cancelled = true
	}
}

// Cancelled reports whether Cancel has been called on the event.
func (c *Cancellable) Cancelled() bool {
	return c != nil && c.cancelled
}

type cancellable interface {
	Cancelled() bool
}

type efaceHeader struct {
	typ  uintptr
	data unsafe.Pointer
}

// efaceType returns the runtime type descriptor word for a non-nil interface
// value without invoking reflect.
func efaceType(v any) uintptr {
	return (*efaceHeader)(unsafe.Pointer(&v)).typ
}

var (
	typeMu     sync.Mutex
	typeIDs    atomic.Pointer[map[uintptr]uint32]
	typeByRefl = make(map[reflect.Type]uint32)
	nextTypeID uint32
)

func init() {
	empty := make(map[uintptr]uint32)
	typeIDs.Store(&empty)
}

// lookupTypeID resolves the dense event type ID for E without reflection.
func lookupTypeID[E any]() (uint32, bool) {
	tp := efaceType((*E)(nil))
	id, ok := (*typeIDs.Load())[tp]
	return id, ok
}

// registerTypeID returns the dense event type ID for E, calling
// reflect.TypeFor[E]() at most once per distinct event type E.
func registerTypeID[E any]() uint32 {
	if id, ok := lookupTypeID[E](); ok {
		return id
	}
	tp := efaceType((*E)(nil))
	rt := reflect.TypeFor[E]()

	typeMu.Lock()
	defer typeMu.Unlock()

	cur := *typeIDs.Load()
	if id, ok := cur[tp]; ok {
		return id
	}
	if id, ok := typeByRefl[rt]; ok {
		next := maps.Clone(cur)
		next[tp] = id
		typeIDs.Store(&next)
		return id
	}
	id := nextTypeID
	nextTypeID++
	typeByRefl[rt] = id
	next := maps.Clone(cur)
	next[tp] = id
	typeIDs.Store(&next)
	return id
}

type listenerEntry struct {
	id       uint64
	priority Priority
	fn       any
	alive    atomic.Bool
}

type listenerSlot struct {
	listeners atomic.Pointer[[]*listenerEntry]
}

// Bus dispatches typed events to registered listeners in priority order.
// The zero value of Bus is ready to use and is safe for concurrent use.
type Bus struct {
	mu            sync.Mutex
	nextSeq       uint64
	listenerCount atomic.Int64
	slots         atomic.Pointer[[]*listenerSlot]
}

// NewBus returns a new initialized event Bus.
func NewBus() *Bus {
	return &Bus{}
}

// Subscription represents a registered listener on a Bus. Calling Unsubscribe
// detaches the listener safely and idempotently.
type Subscription struct {
	bus    *Bus
	typeID uint32
	entry  *listenerEntry
}

// Unsubscribe detaches the listener from its Bus. It is safe to call multiple
// times, concurrently, or on a zero-value Subscription.
func (s Subscription) Unsubscribe() {
	if s.bus == nil || s.entry == nil {
		return
	}
	if !s.entry.alive.CompareAndSwap(true, false) {
		return
	}
	s.bus.removeListener(s.typeID, s.entry)
}

func (b *Bus) removeListener(id uint32, target *listenerEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	slotsPtr := b.slots.Load()
	if slotsPtr == nil || int(id) >= len(*slotsPtr) {
		b.listenerCount.Add(-1)
		return
	}
	slot := (*slotsPtr)[id]
	if slot == nil {
		b.listenerCount.Add(-1)
		return
	}
	curPtr := slot.listeners.Load()
	if curPtr == nil || len(*curPtr) == 0 {
		b.listenerCount.Add(-1)
		return
	}
	next := make([]*listenerEntry, 0, len(*curPtr)-1)
	for _, entry := range *curPtr {
		if entry != target && entry.alive.Load() {
			next = append(next, entry)
		}
	}
	slot.listeners.Store(&next)
	b.listenerCount.Add(-1)
}

// On registers fn to listen for events of type *E at priority p on bus.
// Returns a Subscription that may be used to detach the listener.
func On[E any](bus *Bus, p Priority, fn func(*E)) Subscription {
	if bus == nil || fn == nil {
		return Subscription{}
	}
	id := registerTypeID[E]()
	entry := &listenerEntry{
		priority: p,
		fn:       fn,
	}
	entry.alive.Store(true)

	bus.mu.Lock()
	bus.nextSeq++
	entry.id = bus.nextSeq

	slotsPtr := bus.slots.Load()
	var slots []*listenerSlot
	if slotsPtr != nil {
		slots = *slotsPtr
	}
	var slot *listenerSlot
	needPublishSlots := false
	if int(id) >= len(slots) || slots[id] == nil {
		grown := make([]*listenerSlot, max(len(slots), int(id)+1))
		copy(grown, slots)
		slot = &listenerSlot{}
		grown[id] = slot
		slots = grown
		needPublishSlots = true
	} else {
		slot = slots[id]
	}
	var cur []*listenerEntry
	if curPtr := slot.listeners.Load(); curPtr != nil {
		cur = *curPtr
	}
	next := make([]*listenerEntry, len(cur)+1)
	copy(next, cur)
	next[len(cur)] = entry
	slices.SortFunc(next, func(a, b *listenerEntry) int {
		if a.priority != b.priority {
			return cmp.Compare(a.priority, b.priority)
		}
		return cmp.Compare(a.id, b.id)
	})
	slot.listeners.Store(&next)
	if needPublishSlots {
		bus.slots.Store(&slots)
	}
	bus.listenerCount.Add(1)
	bus.mu.Unlock()

	return Subscription{bus: bus, typeID: id, entry: entry}
}

// HasListeners reports whether bus currently has at least one listener
// registered for events of type *E. It performs zero reflection and zero
// heap allocations.
func HasListeners[E any](bus *Bus) bool {
	if bus == nil || bus.listenerCount.Load() == 0 {
		return false
	}
	id, ok := lookupTypeID[E]()
	if !ok {
		return false
	}
	slotsPtr := bus.slots.Load()
	if slotsPtr == nil || int(id) >= len(*slotsPtr) {
		return false
	}
	slot := (*slotsPtr)[id]
	if slot == nil {
		return false
	}
	listPtr := slot.listeners.Load()
	return listPtr != nil && len(*listPtr) > 0
}

// Emit dispatches ev to all listeners registered for *E on bus in ascending
// priority order (PriorityLowest through PriorityHighest, then PriorityMonitor).
//
// If *E implements interface{ Cancelled() bool }, once Cancelled() returns true
// remaining non-monitor listeners (Priority < PriorityMonitor) are skipped,
// while PriorityMonitor listeners are always invoked to observe the final
// outcome. Emit returns !ev.Cancelled() for cancellable events, or true for
// non-cancellable events (and false if ev is nil).
//
// Emit performs zero reflection and zero heap allocations in steady state.
func Emit[E any](bus *Bus, ev *E) bool {
	if ev == nil {
		return false
	}
	c, isCancellable := any(ev).(cancellable)
	if bus == nil || bus.listenerCount.Load() == 0 {
		return !isCancellable || !c.Cancelled()
	}
	id, ok := lookupTypeID[E]()
	if !ok {
		return !isCancellable || !c.Cancelled()
	}
	slotsPtr := bus.slots.Load()
	if slotsPtr == nil || int(id) >= len(*slotsPtr) {
		return !isCancellable || !c.Cancelled()
	}
	slot := (*slotsPtr)[id]
	if slot == nil {
		return !isCancellable || !c.Cancelled()
	}
	listPtr := slot.listeners.Load()
	if listPtr == nil || len(*listPtr) == 0 {
		return !isCancellable || !c.Cancelled()
	}
	for _, entry := range *listPtr {
		if !entry.alive.Load() {
			continue
		}
		if isCancellable && entry.priority < PriorityMonitor && c.Cancelled() {
			continue
		}
		entry.fn.(func(*E))(ev)
	}
	return !isCancellable || !c.Cancelled()
}
