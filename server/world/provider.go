package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
	"io"
	"reflect"
	"sync"
)

// Provider represents a value that may provide world data to a World value. It usually does the reading and
// writing of the world data so that the World may use it.
type Provider interface {
	io.Closer
	// Settings loads the settings for a World and returns them.
	Settings() *Settings
	// SaveSettings saves the settings of a World.
	SaveSettings(*Settings)

	// LoadPlayerSpawnPosition loads the player spawn point if found, otherwise an error will be returned.
	LoadPlayerSpawnPosition(uuid uuid.UUID) (pos cube.Pos, exists bool, err error)
	// SavePlayerSpawnPosition saves the player spawn point. In vanilla, this can be done with beds in the overworld
	// or respawn anchors in the nether.
	SavePlayerSpawnPosition(uuid uuid.UUID, pos cube.Pos) error
	// LoadColumn reads a world.Column from the DB at a position and dimension
	// in the DB. If no column at that position exists, errors.Is(err,
	// leveldb.ErrNotFound) equals true.
	LoadColumn(pos ChunkPos, dim Dimension) (*chunk.Column, error)
	// StoreColumn stores a world.Column at a position and dimension in the DB.
	// An error is returned if storing was unsuccessful.
	StoreColumn(pos ChunkPos, dim Dimension, col *chunk.Column) error
}

type positionTrackingProvider interface {
	LoadPositionTrackingData() (PositionTrackingData, error)
	SavePositionTrackingData(PositionTrackingData) error
}

type providerKey struct {
	typ   reflect.Type
	ptr   uintptr
	value any
}

type providerRef struct {
	key         providerKey
	refs        int
	shared      bool
	trackerOnce sync.Once
	tracker     *PositionTracker
	trackerErr  error
}

func (r *providerRef) positionTracker(provider Provider) (*PositionTracker, error) {
	r.trackerOnce.Do(func() {
		r.tracker = NewPositionTracker()
		if p, ok := provider.(positionTrackingProvider); ok {
			var data PositionTrackingData
			data, r.trackerErr = p.LoadPositionTrackingData()
			if r.trackerErr == nil {
				r.tracker.load(data)
			}
		}
	})
	return r.tracker, r.trackerErr
}

var providerRefs = struct {
	sync.Mutex
	m map[providerKey]*providerRef
}{m: make(map[providerKey]*providerRef)}

func retainProvider(provider Provider) *providerRef {
	key, ok := providerIdentity(provider)
	if !ok {
		return &providerRef{refs: 1}
	}
	providerRefs.Lock()
	defer providerRefs.Unlock()
	if ref := providerRefs.m[key]; ref != nil {
		ref.refs++
		return ref
	}
	ref := &providerRef{key: key, refs: 1, shared: true}
	providerRefs.m[key] = ref
	return ref
}

func releaseProvider(ref *providerRef) bool {
	if !ref.shared {
		return true
	}
	providerRefs.Lock()
	defer providerRefs.Unlock()
	ref.refs--
	if ref.refs != 0 {
		return false
	}
	delete(providerRefs.m, ref.key)
	return true
}

func providerIdentity(provider Provider) (providerKey, bool) {
	typ := reflect.TypeOf(provider)
	if typ == nil {
		return providerKey{}, false
	}
	if typ.Comparable() {
		return providerKey{typ: typ, value: provider}, true
	}
	value := reflect.ValueOf(provider)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return providerKey{typ: typ, ptr: value.Pointer()}, true
	default:
		return providerKey{}, false
	}
}

// Compile time check to make sure NopProvider implements Provider.
var _ Provider = (*NopProvider)(nil)

// NopProvider implements a Provider that does not perform any disk I/O. It generates values on the run and
// dynamically, instead of reading and writing data, and otherwise returns empty values. A Settings struct can be passed
// to initialise a world with specific settings. Since Settings is a pointer, using the same NopProvider for multiple
// worlds means those worlds will share the same settings.
type NopProvider struct {
	Set *Settings
}

func (n NopProvider) Settings() *Settings {
	if n.Set == nil {
		return defaultSettings()
	}
	return n.Set
}
func (NopProvider) SaveSettings(*Settings) {}
func (NopProvider) LoadColumn(ChunkPos, Dimension) (*chunk.Column, error) {
	return nil, leveldb.ErrNotFound
}
func (NopProvider) StoreColumn(ChunkPos, Dimension, *chunk.Column) error { return nil }
func (NopProvider) LoadPlayerSpawnPosition(uuid.UUID) (cube.Pos, bool, error) {
	return cube.Pos{}, false, nil
}
func (NopProvider) SavePlayerSpawnPosition(uuid.UUID, cube.Pos) error { return nil }
func (NopProvider) Close() error                                      { return nil }
