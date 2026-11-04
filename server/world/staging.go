package world

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
)

// Staging holds the writes the worlds of one provider make to the data
// saved beside their columns between two captures: namespaced world data
// (World.SaveWorldData: forced chunks, game rules, command storage and the
// like), the structure references the generator records, and maps
// (World.StoreMapData). As a column unloaded is staged until the next
// capture (chunkSaver.stage), these wait in memory, so that the provider
// holds them as they were at a capture rather than as they were when
// written. The worlds read through it: a read finds a write staged, or
// taken by a capture and not yet written, before what the provider holds.
//
// The next capture of any world of the provider takes the writes staged,
// and its Checkpoint writes them before the data it captured. A write that
// fails is staged again, unless a newer one of the same data was staged
// since. The reservations of map IDs (MapDataProvider.AllocateMapID and
// JavaMapID) are not staged: an ID reserved on disk is never handed out
// twice, even if the map it was reserved for is lost in a crash, while a
// reservation staged and lost could hand out an ID the record of a player
// who quit already holds. The spawn points of players (SetPlayerSpawn) are
// written at once too: a provider that keeps none, as NopProvider, reads
// none back, and a player's record holds its spawn point besides. The
// position tracking data needs no staging: its tracker lives in memory and
// is written by the captures.
type Staging struct {
	mu  sync.Mutex
	seq uint64
	cur *stagedWrites
	// taken holds the writes taken by captures and not yet written.
	taken []*stagedWrites

	// writeMu serialises the writes of the writes taken. written holds the
	// seq of the last write of each key while writes taken overlap, so that
	// an older one never replaces a newer.
	writeMu sync.Mutex
	written map[string]uint64
}

// stagedWrites is a set of writes staged between two captures, numbered by
// seq in the order taken.
type stagedWrites struct {
	seq  uint64
	data map[string][]byte
	maps map[int64]MapData
}

func (sw *stagedWrites) empty() bool {
	return sw == nil || len(sw.data)+len(sw.maps) == 0
}

// current returns the writes being staged. The caller holds mu.
func (s *Staging) current() *stagedWrites {
	if s.cur == nil {
		s.cur = &stagedWrites{data: map[string][]byte{}, maps: map[int64]MapData{}}
	}
	return s.cur
}

// stageData stages data as the namespaced world data of namespace.
func (s *Staging) stageData(namespace string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current().data[namespace] = append([]byte{}, data...)
}

// stageMap stages data as the map of its ID.
func (s *Staging) stageMap(data MapData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current().maps[data.ID] = cloneMapData(data)
}

// lookup calls find with the writes staged, then those taken from the
// newest to the oldest, until it reports true.
func (s *Staging) lookup(find func(sw *stagedWrites) bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil && find(s.cur) {
		return
	}
	for i := len(s.taken) - 1; i >= 0; i-- {
		if find(s.taken[i]) {
			return
		}
	}
}

// data returns the namespaced world data staged for namespace.
func (s *Staging) data(namespace string) (data []byte, ok bool) {
	s.lookup(func(sw *stagedWrites) bool {
		var b []byte
		if b, ok = sw.data[namespace]; ok {
			data = append([]byte{}, b...)
		}
		return ok
	})
	return data, ok
}

// mapData returns the map staged with the ID passed, as a provider reads
// back the map it stored: without the changes of its last update, which
// only its viewers are sent.
func (s *Staging) mapData(id int64) (data MapData, ok bool) {
	s.lookup(func(sw *stagedWrites) bool {
		var m MapData
		if m, ok = sw.maps[id]; ok {
			data = cloneMapData(m)
		}
		return ok
	})
	data.ColorsChanged, data.ForceFullTexture, data.PixelPatch, data.TrackedObjects = false, false, nil, nil
	return data, ok
}

// take takes the writes staged, for a capture to write, or nil if none.
// They stay readable until written.
func (s *Staging) take() *stagedWrites {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur.empty() {
		return nil
	}
	sw := s.cur
	s.cur = nil
	s.seq++
	sw.seq = s.seq
	s.taken = append(s.taken, sw)
	return sw
}

// write writes sw, taken by take, through provider and returns the errors
// it fails with. A write that fails is staged again, unless a newer write of
// the same data was staged since.
func (s *Staging) write(provider Provider, sw *stagedWrites) error {
	if sw == nil {
		return nil
	}
	s.writeMu.Lock()
	var errs, failed []error
	retry := &stagedWrites{data: map[string][]byte{}, maps: map[int64]MapData{}}
	newer := func(key string) bool {
		if s.written == nil {
			s.written = map[string]uint64{}
		}
		if s.written[key] > sw.seq {
			return true
		}
		s.written[key] = sw.seq
		return false
	}
	store, stores := provider.(worldDataProvider)
	for namespace, data := range sw.data {
		if newer("data:" + namespace) {
			continue
		}
		if !stores {
			errs = append(errs, fmt.Errorf("save world data %s: provider %T stores none", namespace, provider))
			continue
		}
		if err := store.SaveWorldData(namespace, data); err != nil {
			failed = append(failed, fmt.Errorf("save world data %s: %w", namespace, err))
			retry.data[namespace] = data
		}
	}
	maps, storesMaps := provider.(MapDataProvider)
	for id, data := range sw.maps {
		if newer("map:" + strconv.FormatInt(id, 10)) {
			continue
		}
		if !storesMaps {
			errs = append(errs, fmt.Errorf("save map %d: %w", id, ErrMapDataUnavailable))
			continue
		}
		if err := maps.StoreMap(data); err != nil {
			failed = append(failed, fmt.Errorf("save map %d: %w", id, err))
			retry.maps[id] = data
		}
	}
	s.writeMu.Unlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if i := slices.Index(s.taken, sw); i >= 0 {
		s.taken = slices.Delete(s.taken, i, i+1)
	}
	if len(s.taken) == 0 {
		s.writeMu.Lock()
		s.written = nil
		s.writeMu.Unlock()
	}
	if !retry.empty() {
		cur := s.current()
		for namespace, data := range retry.data {
			if _, ok := cur.data[namespace]; !ok {
				cur.data[namespace] = data
			}
		}
		for id, data := range retry.maps {
			if _, ok := cur.maps[id]; !ok {
				cur.maps[id] = data
			}
		}
	}
	return errors.Join(append(errs, failed...)...)
}

// cloneMapData returns a copy of data that shares no buffer with it.
func cloneMapData(data MapData) MapData {
	data.Colors = slices.Clone(data.Colors)
	data.NativeColors = slices.Clone(data.NativeColors)
	data.Decorations = slices.Clone(data.Decorations)
	data.TrackedObjects = slices.Clone(data.TrackedObjects)
	return data
}
