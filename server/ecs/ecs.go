package ecs

import (
	"fmt"
	"maps"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
)

// codec holds typed NBT serialization callbacks for a registered component type.
type codec[T any] struct {
	encode func(T) map[string]any
	decode func(map[string]any) (T, bool)
}

// Key is a typed handle for a component slot in a Bag.
//
// The zero value of Key is invalid; always obtain keys via NewKey.
type Key[T any] struct {
	id    uint16
	name  string
	codec *codec[T]
}

// ID returns the monotonic dense identifier assigned to k, or 0 for the
// invalid zero Key.
func (k Key[T]) ID() uint16 { return k.id }

// Name returns the unique registration name of k.
func (k Key[T]) Name() string { return k.name }

// Option configures a Key registered with NewKey.
type Option[T any] func(*keyConfig[T])

type keyConfig[T any] struct {
	codec *codec[T]
}

// WithCodec attaches NBT encode and decode callbacks to a component Key.
// Components registered without WithCodec are runtime-only and are omitted
// during Bag NBT encoding.
func WithCodec[T any](encode func(T) map[string]any, decode func(map[string]any) (T, bool)) Option[T] {
	return func(cfg *keyConfig[T]) {
		if encode != nil || decode != nil {
			cfg.codec = &codec[T]{encode: encode, decode: decode}
		}
	}
}

type keyEntry struct {
	id     uint16
	name   string
	typ    reflect.Type
	rawKey any
	clone  func(any) any
	encode func(any) map[string]any
	decode func(*Bag, map[string]any)
}

type keyRegistry struct {
	mu      sync.Mutex
	byName  atomic.Pointer[map[string]uint16]
	entries atomic.Pointer[[]keyEntry]
}

var registry = func() *keyRegistry {
	r := &keyRegistry{}
	emptyNames := make(map[string]uint16)
	r.byName.Store(&emptyNames)
	empty := make([]keyEntry, 0)
	r.entries.Store(&empty)
	return r
}()

// NewKey registers or looks up a typed component key by name.
//
// Keys receive monotonic dense uint16 IDs starting at 1. NewKey is safe for
// concurrent use. If name is empty, or if name was already registered with a
// different Go type T, NewKey panics unambiguously. Calling NewKey again with
// the same name and identical type T returns the existing Key[T] (retaining its
// original ID and codec).
func NewKey[T any](name string, opts ...Option[T]) Key[T] {
	if name == "" {
		panic("ecs: key name must not be empty")
	}
	var cfg keyConfig[T]
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	rt := reflect.TypeFor[T]()

	registry.mu.Lock()
	defer registry.mu.Unlock()

	cur := *registry.entries.Load()
	names := *registry.byName.Load()
	if existingID, ok := names[name]; ok {
		entry := cur[existingID-1]
		if entry.typ != rt {
			panic(fmt.Sprintf("ecs: key %q already registered with type %v, cannot re-register with type %v", name, entry.typ, rt))
		}
		return entry.rawKey.(Key[T])
	}
	if len(cur) >= math.MaxUint16 {
		panic("ecs: maximum key limit (65535) reached")
	}

	id := uint16(len(cur) + 1)
	k := Key[T]{
		id:    id,
		name:  name,
		codec: cfg.codec,
	}

	entry := keyEntry{
		id:     id,
		name:   name,
		typ:    rt,
		rawKey: k,
		clone: func(v any) any {
			cp := *v.(*T)
			return &cp
		},
	}
	if cfg.codec != nil {
		c := cfg.codec
		if c.encode != nil {
			entry.encode = func(v any) map[string]any {
				return c.encode(*v.(*T))
			}
		}
		if c.decode != nil {
			entry.decode = func(b *Bag, m map[string]any) {
				if val, ok := c.decode(m); ok {
					Set(b, k, val)
				}
			}
		}
	}

	next := make([]keyEntry, len(cur)+1)
	copy(next, cur)
	next[id-1] = entry
	nextNames := maps.Clone(names)
	nextNames[name] = id
	registry.entries.Store(&next)
	registry.byName.Store(&nextNames)
	return k
}

// Bag stores typed ECS components in a dense slot slice indexed by Key ID.
// The zero value is ready to use and performs zero heap allocations until a
// component is stored.
//
// Bag is not internally synchronized; callers must ensure single-goroutine or
// externally synchronized access per Bag instance.
type Bag struct {
	slots []any
}

func (b *Bag) ensureSlot(idx int) {
	if idx < len(b.slots) {
		return
	}
	if idx < cap(b.slots) {
		b.slots = b.slots[:idx+1]
		return
	}
	newCap := max(idx+1, cap(b.slots)*2)
	grown := make([]any, idx+1, newCap)
	copy(grown, b.slots)
	b.slots = grown
}

// Get returns the component stored under k in b, or the zero value and false if
// b is nil, k is uninitialized, or the component is absent.
func Get[T any](b *Bag, k Key[T]) (T, bool) {
	var zero T
	if b == nil || k.id == 0 {
		return zero, false
	}
	idx := int(k.id - 1)
	if idx >= len(b.slots) {
		return zero, false
	}
	raw := b.slots[idx]
	if raw == nil {
		return zero, false
	}
	ptr, ok := raw.(*T)
	if !ok || ptr == nil {
		return zero, false
	}
	return *ptr, true
}

// Set stores v under k in b, overwriting any existing component value in place
// without allocating after the slot's first insertion.
// Set panics if b is nil or k is uninitialized.
func Set[T any](b *Bag, k Key[T], v T) {
	if b == nil {
		panic("ecs: Set on nil Bag")
	}
	if k.id == 0 {
		panic("ecs: Set with uninitialized Key")
	}
	idx := int(k.id - 1)
	b.ensureSlot(idx)
	if ptr, ok := b.slots[idx].(*T); ok && ptr != nil {
		*ptr = v
		return
	}
	p := new(T)
	*p = v
	b.slots[idx] = p
}

// Update invokes fn on a pointer to the component stored under k in b.
// If the component is not yet present in b, it is initialized to T's zero
// value before calling fn. Update panics if b is nil or k is uninitialized.
func Update[T any](b *Bag, k Key[T], fn func(*T)) {
	if b == nil {
		panic("ecs: Update on nil Bag")
	}
	if k.id == 0 {
		panic("ecs: Update with uninitialized Key")
	}
	if fn == nil {
		return
	}
	idx := int(k.id - 1)
	b.ensureSlot(idx)
	ptr, ok := b.slots[idx].(*T)
	if !ok || ptr == nil {
		ptr = new(T)
		b.slots[idx] = ptr
	}
	fn(ptr)
}

// Delete removes the component stored under k from b, if present.
func Delete[T any](b *Bag, k Key[T]) {
	if b == nil || k.id == 0 {
		return
	}
	idx := int(k.id - 1)
	if idx < len(b.slots) {
		b.slots[idx] = nil
	}
}

// Has reports whether a component is stored under k in b.
func Has[T any](b *Bag, k Key[T]) bool {
	if b == nil || k.id == 0 {
		return false
	}
	idx := int(k.id - 1)
	return idx < len(b.slots) && b.slots[idx] != nil
}

// Clone returns an independent copy of b. An empty or nil Bag clones with zero
// allocations.
func (b *Bag) Clone() Bag {
	if b == nil || len(b.slots) == 0 {
		return Bag{}
	}
	last := -1
	for i := len(b.slots) - 1; i >= 0; i-- {
		if b.slots[i] != nil {
			last = i
			break
		}
	}
	if last < 0 {
		return Bag{}
	}
	entries := *registry.entries.Load()
	dst := make([]any, last+1)
	for i := 0; i <= last; i++ {
		if b.slots[i] == nil {
			continue
		}
		dst[i] = entries[i].clone(b.slots[i])
	}
	return Bag{slots: dst}
}

// Empty reports whether b contains zero components.
func (b *Bag) Empty() bool {
	if b == nil {
		return true
	}
	for _, s := range b.slots {
		if s != nil {
			return false
		}
	}
	return true
}

// EncodeNBT encodes all persistent components in b (those registered with
// WithCodec) into a map keyed by component name. Returns nil with zero
// allocations if b is empty or has no persistent components.
func EncodeNBT(b *Bag) map[string]any {
	if b == nil || len(b.slots) == 0 {
		return nil
	}
	entries := *registry.entries.Load()
	var out map[string]any
	for i, raw := range b.slots {
		if raw == nil || i >= len(entries) {
			continue
		}
		enc := entries[i].encode
		if enc == nil {
			continue
		}
		data := enc(raw)
		if data == nil {
			continue
		}
		if out == nil {
			out = make(map[string]any)
		}
		out[entries[i].name] = data
	}
	return out
}

// DecodeNBT decodes component NBT entries from m into b using registered key
// codecs. Unknown component names or malformed entries are ignored cleanly.
func DecodeNBT(b *Bag, m map[string]any) {
	if b == nil || len(m) == 0 {
		return
	}
	names := *registry.byName.Load()
	entries := *registry.entries.Load()
	for name, raw := range m {
		sub, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id, ok := names[name]
		if !ok || int(id) > len(entries) {
			continue
		}
		if dec := entries[id-1].decode; dec != nil {
			dec(b, sub)
		}
	}
}

// EncodeNBT encodes all persistent components in b into a map keyed by
// component name.
func (b *Bag) EncodeNBT() map[string]any {
	return EncodeNBT(b)
}

// DecodeNBT decodes persistent component entries from m into b.
func (b *Bag) DecodeNBT(m map[string]any) {
	DecodeNBT(b, m)
}
