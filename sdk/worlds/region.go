package worlds

import (
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/dm-vev/adamant/sdk/event"
	"github.com/dm-vev/adamant/sdk/plugin"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Box is an inclusive integer cuboid region defined by Min and Max corners.
type Box struct {
	Min cube.Pos
	Max cube.Pos
}

// NewBox constructs a normalized Box with Min <= Max on every axis.
func NewBox(a, b cube.Pos) Box {
	return Box{
		Min: cube.Pos{min(a[0], b[0]), min(a[1], b[1]), min(a[2], b[2])},
		Max: cube.Pos{max(a[0], b[0]), max(a[1], b[1]), max(a[2], b[2])},
	}
}

// ContainsBlock reports whether block position p lies inside b.
func (b Box) ContainsBlock(p cube.Pos) bool {
	return p[0] >= b.Min[0] && p[0] <= b.Max[0] &&
		p[1] >= b.Min[1] && p[1] <= b.Max[1] &&
		p[2] >= b.Min[2] && p[2] <= b.Max[2]
}

// ContainsVec reports whether continuous point v lies inside b's block bounds
// [Min, Max+1).
func (b Box) ContainsVec(v mgl64.Vec3) bool {
	return v[0] >= float64(b.Min[0]) && v[0] < float64(b.Max[0]+1) &&
		v[1] >= float64(b.Min[1]) && v[1] < float64(b.Max[1]+1) &&
		v[2] >= float64(b.Min[2]) && v[2] < float64(b.Max[2]+1)
}

// Intersects reports whether b and other share at least one block position.
func (b Box) Intersects(other Box) bool {
	return b.Min[0] <= other.Max[0] && b.Max[0] >= other.Min[0] &&
		b.Min[1] <= other.Max[1] && b.Max[1] >= other.Min[1] &&
		b.Min[2] <= other.Max[2] && b.Max[2] >= other.Min[2]
}

// Region is a named 3D cuboid zone with optional priority, tags, and metadata
// (useful for spawn protection, PvP arenas, claims, and trigger zones).
type Region struct {
	Name     string
	Box      Box
	Priority int
	Tags     []string
	Meta     map[string]any
}

// HasTag reports whether r carries tag.
func (r *Region) HasTag(tag string) bool {
	return r != nil && slices.Contains(r.Tags, tag)
}

// RegionIndex maintains named Regions indexed by overlapping chunk coordinates
// for fast O(1) chunk-bucket point and cuboid queries. Safe for concurrent use.
type RegionIndex struct {
	mu      sync.RWMutex
	byName  map[string]*Region
	buckets map[world.ChunkPos][]*Region
}

// NewRegionIndex creates an empty RegionIndex.
func NewRegionIndex() *RegionIndex {
	return &RegionIndex{
		byName:  make(map[string]*Region),
		buckets: make(map[world.ChunkPos][]*Region),
	}
}

// Add inserts or replaces r in the index.
func (idx *RegionIndex) Add(r Region) *Region {
	if idx == nil || r.Name == "" {
		return nil
	}
	cp := &Region{
		Name:     r.Name,
		Box:      NewBox(r.Box.Min, r.Box.Max),
		Priority: r.Priority,
		Tags:     slices.Clone(r.Tags),
		Meta:     maps.Clone(r.Meta),
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if old, ok := idx.byName[cp.Name]; ok {
		idx.removeLocked(old)
	}
	idx.byName[cp.Name] = cp
	minCX, maxCX := int32(cp.Box.Min[0]>>4), int32(cp.Box.Max[0]>>4)
	minCZ, maxCZ := int32(cp.Box.Min[2]>>4), int32(cp.Box.Max[2]>>4)
	for cx := minCX; cx <= maxCX; cx++ {
		for cz := minCZ; cz <= maxCZ; cz++ {
			pos := world.ChunkPos{cx, cz}
			idx.buckets[pos] = append(idx.buckets[pos], cp)
		}
	}
	return cp
}

// Remove deletes the region named name from the index.
func (idx *RegionIndex) Remove(name string) bool {
	if idx == nil || name == "" {
		return false
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	r, ok := idx.byName[name]
	if !ok {
		return false
	}
	idx.removeLocked(r)
	delete(idx.byName, name)
	return true
}

func (idx *RegionIndex) removeLocked(r *Region) {
	minCX, maxCX := int32(r.Box.Min[0]>>4), int32(r.Box.Max[0]>>4)
	minCZ, maxCZ := int32(r.Box.Min[2]>>4), int32(r.Box.Max[2]>>4)
	for cx := minCX; cx <= maxCX; cx++ {
		for cz := minCZ; cz <= maxCZ; cz++ {
			pos := world.ChunkPos{cx, cz}
			list := idx.buckets[pos]
			for i, item := range list {
				if item == r {
					list = slices.Delete(list, i, i+1)
					break
				}
			}
			if len(list) == 0 {
				delete(idx.buckets, pos)
			} else {
				idx.buckets[pos] = list
			}
		}
	}
}

// Get returns the region registered under name, if any.
func (idx *RegionIndex) Get(name string) (*Region, bool) {
	if idx == nil {
		return nil, false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	r, ok := idx.byName[name]
	return r, ok
}

// AtBlock returns all regions containing block pos, sorted by Priority descending
// then Name ascending.
func (idx *RegionIndex) AtBlock(pos cube.Pos) []*Region {
	if idx == nil {
		return nil
	}
	cp := world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}
	idx.mu.RLock()
	candidates := idx.buckets[cp]
	var out []*Region
	for _, r := range candidates {
		if r.Box.ContainsBlock(pos) {
			out = append(out, r)
		}
	}
	idx.mu.RUnlock()
	sortRegions(out)
	return out
}

// At returns all regions containing continuous position pos, sorted by Priority
// descending then Name ascending.
func (idx *RegionIndex) At(pos mgl64.Vec3) []*Region {
	if idx == nil {
		return nil
	}
	bp := cube.PosFromVec3(pos)
	cp := world.ChunkPos{int32(bp[0] >> 4), int32(bp[2] >> 4)}
	idx.mu.RLock()
	candidates := idx.buckets[cp]
	var out []*Region
	for _, r := range candidates {
		if r.Box.ContainsVec(pos) {
			out = append(out, r)
		}
	}
	idx.mu.RUnlock()
	sortRegions(out)
	return out
}

// Intersecting returns all regions intersecting box b, sorted by Priority
// descending then Name ascending.
func (idx *RegionIndex) Intersecting(b Box) []*Region {
	if idx == nil {
		return nil
	}
	b = NewBox(b.Min, b.Max)
	minCX, maxCX := int32(b.Min[0]>>4), int32(b.Max[0]>>4)
	minCZ, maxCZ := int32(b.Min[2]>>4), int32(b.Max[2]>>4)
	idx.mu.RLock()
	seen := make(map[*Region]struct{})
	var out []*Region
	for cx := minCX; cx <= maxCX; cx++ {
		for cz := minCZ; cz <= maxCZ; cz++ {
			for _, r := range idx.buckets[world.ChunkPos{cx, cz}] {
				if _, dup := seen[r]; dup {
					continue
				}
				seen[r] = struct{}{}
				if r.Box.Intersects(b) {
					out = append(out, r)
				}
			}
		}
	}
	idx.mu.RUnlock()
	sortRegions(out)
	return out
}

// All returns all registered regions sorted by Priority descending then Name.
func (idx *RegionIndex) All() []*Region {
	if idx == nil {
		return nil
	}
	idx.mu.RLock()
	out := make([]*Region, 0, len(idx.byName))
	for _, r := range idx.byName {
		out = append(out, r)
	}
	idx.mu.RUnlock()
	sortRegions(out)
	return out
}

// TrackWorld subscribes to MoveEvent and TeleportEvent on w and invokes
// onEnter / onLeave whenever a player crosses a region boundary. Automatically
// unsubscribes on ctx.OnDisable when ctx is non-nil.
func (idx *RegionIndex) TrackWorld(
	ctx *plugin.Context,
	w *world.World,
	onEnter func(p *player.Player, r *Region, tx *world.Tx),
	onLeave func(p *player.Player, r *Region, tx *world.Tx),
) func() {
	if idx == nil || w == nil {
		return func() {}
	}
	diff := func(p *player.Player, from, to mgl64.Vec3, tx *world.Tx) {
		prev := idx.At(from)
		next := idx.At(to)
		if len(prev) == 0 && len(next) == 0 {
			return
		}
		if onLeave != nil {
			for _, r := range prev {
				if !slices.Contains(next, r) {
					onLeave(p, r, tx)
				}
			}
		}
		if onEnter != nil {
			for _, r := range next {
				if !slices.Contains(prev, r) {
					onEnter(p, r, tx)
				}
			}
		}
	}
	s1 := event.OnWorld[player.MoveEvent](ctx, w, event.Monitor, func(ev *player.MoveEvent) {
		if !ev.Cancelled() {
			diff(ev.Player, ev.From, ev.To, ev.Tx)
		}
	})
	s2 := event.OnWorld[player.TeleportEvent](ctx, w, event.Monitor, func(ev *player.TeleportEvent) {
		if !ev.Cancelled() {
			diff(ev.Player, ev.From, ev.To, ev.Tx)
		}
	})
	return func() {
		s1.Unsubscribe()
		s2.Unsubscribe()
	}
}

func sortRegions(rs []*Region) {
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Priority != rs[j].Priority {
			return rs[i].Priority > rs[j].Priority
		}
		return rs[i].Name < rs[j].Name
	})
}
