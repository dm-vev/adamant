package structure

import (
	"container/list"
	"sync"
)

const (
	// defaultTemplateCacheMaxBytes bounds the heap retained by decoded Java
	// structure templates shared across structure starts. Templates are
	// immutable and re-decoded on demand after eviction.
	defaultTemplateCacheMaxBytes int64 = 32 << 20

	templateStructBytes = 64
	paletteHeaderBytes  = 24
	templateBlockBytes  = 64
	templateEntityBytes = 160
)

var defaultTemplateCache = newTemplateCache(defaultTemplateCacheMaxBytes)

// templateCache is a concurrency-safe, byte-bounded LRU cache for immutable
// decoded structure templates with singleflight deduplication.
type templateCache struct {
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	recent   list.List
	entries  map[string]*templateCacheEntry
	flights  map[string]*templateFlight
}

type templateCacheEntry struct {
	name     string
	template *Template
	bytes    int64
	element  *list.Element
}

type templateFlight struct {
	done     chan struct{}
	template *Template
	err      error
	failure  any
}

func newTemplateCache(maxBytes int64) *templateCache {
	return &templateCache{
		maxBytes: maxBytes,
		entries:  make(map[string]*templateCacheEntry),
		flights:  make(map[string]*templateFlight),
	}
}

func (c *templateCache) load(name string, decode func() (*Template, error)) (*Template, error) {
	c.mu.Lock()
	if entry, ok := c.entries[name]; ok {
		c.recent.MoveToFront(entry.element)
		t := entry.template
		c.mu.Unlock()
		return t, nil
	}
	if flight, ok := c.flights[name]; ok {
		c.mu.Unlock()
		<-flight.done
		if flight.failure != nil {
			panic(flight.failure)
		}
		return flight.template, flight.err
	}
	if c.flights == nil {
		c.flights = make(map[string]*templateFlight)
	}
	flight := &templateFlight{done: make(chan struct{})}
	c.flights[name] = flight
	c.mu.Unlock()

	func() {
		defer func() { flight.failure = recover() }()
		flight.template, flight.err = decode()
	}()

	c.mu.Lock()
	delete(c.flights, name)
	if flight.failure == nil && flight.err == nil && flight.template != nil {
		c.storeLocked(name, flight.template)
	}
	close(flight.done)
	c.mu.Unlock()

	if flight.failure != nil {
		panic(flight.failure)
	}
	return flight.template, flight.err
}

func (c *templateCache) storeLocked(name string, t *Template) {
	size := estimateTemplateBytes(t)
	if c.maxBytes > 0 && size > c.maxBytes {
		return
	}
	if c.entries == nil {
		c.entries = make(map[string]*templateCacheEntry)
	}
	if existing, ok := c.entries[name]; ok {
		c.bytes -= existing.bytes
		existing.template = t
		existing.bytes = size
		c.bytes += size
		c.recent.MoveToFront(existing.element)
	} else {
		entry := &templateCacheEntry{name: name, template: t, bytes: size}
		entry.element = c.recent.PushFront(entry)
		c.entries[name] = entry
		c.bytes += size
	}
	c.evictLocked()
}

func (c *templateCache) evictLocked() {
	if c.maxBytes <= 0 {
		return
	}
	for c.bytes > c.maxBytes {
		tail := c.recent.Back()
		if tail == nil {
			return
		}
		entry := tail.Value.(*templateCacheEntry)
		c.recent.Remove(tail)
		delete(c.entries, entry.name)
		c.bytes -= entry.bytes
	}
}

func estimateTemplateBytes(t *Template) int64 {
	if t == nil {
		return 0
	}
	n := int64(templateStructBytes) + int64(cap(t.Palettes))*paletteHeaderBytes + int64(cap(t.Entities))*templateEntityBytes
	for _, blocks := range t.Palettes {
		n += int64(cap(blocks)) * templateBlockBytes
		for i := range blocks {
			if blocks[i].NBT != nil {
				n += estimateNBTBytes(blocks[i].NBT)
			}
		}
	}
	for i := range t.Entities {
		if t.Entities[i].NBT != nil {
			n += estimateNBTBytes(t.Entities[i].NBT)
		}
		n += int64(cap(t.Entities[i].Passengers)) * templateEntityBytes
	}
	return n
}

func estimateNBTBytes(v any) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		return 16 + int64(len(x))
	case []byte:
		return 24 + int64(cap(x))
	case []int32:
		return 24 + int64(cap(x))*4
	case []int64:
		return 24 + int64(cap(x))*8
	case []float32:
		return 24 + int64(cap(x))*4
	case []float64:
		return 24 + int64(cap(x))*8
	case []string:
		n := int64(24) + int64(cap(x))*16
		for _, s := range x {
			n += int64(len(s))
		}
		return n
	case []any:
		n := int64(24) + int64(cap(x))*16
		for _, elem := range x {
			n += estimateNBTBytes(elem)
		}
		return n
	case []map[string]any:
		n := int64(24) + int64(cap(x))*8
		for _, elem := range x {
			n += estimateNBTBytes(elem)
		}
		return n
	case map[string]any:
		n := int64(48) + int64(len(x))*48
		for k, elem := range x {
			n += int64(len(k)) + estimateNBTBytes(elem)
		}
		return n
	default:
		return 16
	}
}
