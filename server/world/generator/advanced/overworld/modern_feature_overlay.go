package overworld

import (
	"sync"

	"github.com/df-mc/dragonfly/server/world"
)

// posIndex maps the positions of a region to indices. It pages them by
// section of 16x16x16 blocks: a region writes some twenty thousand positions
// in under a hundred sections, and a map by position spends most of its reads
// hashing.
type posIndex struct {
	pages map[uint64]*indexPage
	// last caches the page of the section read last, nil if it has none.
	lastKey   uint64
	last      *indexPage
	lastValid bool
}

// indexPage holds the index plus one of each position of a section, zero for
// a position without one.
type indexPage [4096]uint32

// freeIndexPages keeps released pages for reuse. It outlives garbage
// collections, which empty a sync.Pool many times a second while chunks
// generate.
var freeIndexPages struct {
	sync.Mutex
	pages []*indexPage
}

// maxFreeIndexPages bounds the pages kept for reuse to 64 MiB, about what
// fifteen workers decorating at once hold.
const maxFreeIndexPages = 4096

func newIndexPage() *indexPage {
	freeIndexPages.Lock()
	defer freeIndexPages.Unlock()
	if n := len(freeIndexPages.pages); n > 0 {
		page := freeIndexPages.pages[n-1]
		freeIndexPages.pages = freeIndexPages.pages[:n-1]
		return page
	}
	return new(indexPage)
}

func releaseIndexPage(page *indexPage) {
	*page = indexPage{}
	freeIndexPages.Lock()
	defer freeIndexPages.Unlock()
	if len(freeIndexPages.pages) < maxFreeIndexPages {
		freeIndexPages.pages = append(freeIndexPages.pages, page)
	}
}

// indexSectionKey packs the section of p. The 22 bits of the section x and z
// cover the world border.
func indexSectionKey(p featurePos) uint64 {
	return uint64(uint32(p.x>>4)&0x3fffff) | uint64(uint32(p.z>>4)&0x3fffff)<<22 | uint64(uint32(p.y>>4)&0xfffff)<<44
}

func indexInPage(p featurePos) int {
	return int(p.x&15) | int(p.z&15)<<4 | int(p.y&15)<<8
}

func (x *posIndex) page(key uint64) *indexPage {
	if x.lastValid && x.lastKey == key {
		return x.last
	}
	page := x.pages[key]
	x.lastKey, x.last, x.lastValid = key, page, true
	return page
}

// get returns the index plus one of p, zero if p has none.
func (x *posIndex) get(p featurePos) uint32 {
	if page := x.page(indexSectionKey(p)); page != nil {
		return page[indexInPage(p)]
	}
	return 0
}

// slot returns the index plus one of p for writing.
func (x *posIndex) slot(p featurePos) *uint32 {
	key := indexSectionKey(p)
	page := x.page(key)
	if page == nil {
		page = newIndexPage()
		if x.pages == nil {
			x.pages = make(map[uint64]*indexPage)
		}
		x.pages[key] = page
		x.lastKey, x.last, x.lastValid = key, page, true
	}
	return &page[indexInPage(p)]
}

// all yields every position with an index and its index plus one.
func (x *posIndex) all(yield func(featurePos, uint32) bool) {
	for key, page := range x.pages {
		sx := int32(uint32(key)<<10) >> 10 << 4
		sz := int32(uint32(key>>22)<<10) >> 10 << 4
		sy := int32(uint32(key>>44)<<12) >> 12 << 4
		for i, index := range page {
			if index == 0 {
				continue
			}
			if !yield(featurePos{sx + int32(i&15), sy + int32(i>>8), sz + int32(i>>4&15)}, index) {
				return
			}
		}
	}
}

// release returns the pages of x for reuse, leaving it empty.
func (x *posIndex) release() {
	for _, page := range x.pages {
		releaseIndexPage(page)
	}
	*x = posIndex{}
}

// featureOverlay holds the states written to a region.
type featureOverlay struct {
	index  posIndex
	states []world.NativeState
}

// freeOverlayStates keeps the state slices of released overlays for reuse.
var freeOverlayStates = sync.Pool{New: func() any { return new([]world.NativeState) }}

// get returns the state written at p, if any.
func (o *featureOverlay) get(p featurePos) (world.NativeState, bool) {
	if i := o.index.get(p); i != 0 {
		return o.states[i-1], true
	}
	return world.NativeState{}, false
}

// set writes state at p.
func (o *featureOverlay) set(p featurePos, state world.NativeState) {
	i := o.index.slot(p)
	if *i != 0 {
		o.states[*i-1] = state
		return
	}
	if o.states == nil {
		o.states = (*freeOverlayStates.Get().(*[]world.NativeState))[:0]
	}
	o.states = append(o.states, state)
	*i = uint32(len(o.states))
}

// len returns the number of positions written.
func (o *featureOverlay) len() int {
	return len(o.states)
}

// all yields every position written and its state.
func (o *featureOverlay) all(yield func(featurePos, world.NativeState) bool) {
	for p, i := range o.index.all {
		if !yield(p, o.states[i-1]) {
			return
		}
	}
}

// release returns the memory of the overlay for reuse, leaving it empty.
func (o *featureOverlay) release() {
	o.index.release()
	if o.states != nil {
		states := o.states[:0]
		freeOverlayStates.Put(&states)
	}
	*o = featureOverlay{}
}
