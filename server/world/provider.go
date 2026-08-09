package world

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/google/uuid"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
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

type providerKey struct{ provider Provider }

type providerRef struct {
	key     providerKey
	refs    int
	shared  bool
	closed  bool
	closing chan struct{}

	trackerMu      sync.Mutex
	tracker        *PositionTracker
	trackerValid   atomic.Bool
	trackerVersion atomic.Uint64
	trackerSaved   atomic.Uint64
	writable       atomic.Bool
	providerClosed bool
}

func newProviderRef(provider Provider, writable bool) *providerRef {
	_, tracksPositions := provider.(positionTrackingProvider)
	r := &providerRef{refs: 1, tracker: NewPositionTracker()}
	r.trackerValid.Store(!tracksPositions)
	r.writable.Store(writable)
	return r
}

func (r *providerRef) positionTracker() *PositionTracker {
	return r.tracker
}

func (r *providerRef) loadPositionTracker(provider Provider) error {
	r.trackerMu.Lock()
	defer r.trackerMu.Unlock()
	return r.loadPositionTrackerLocked(provider)
}

func (r *providerRef) loadPositionTrackerLocked(provider Provider) error {
	if r.trackerValid.Load() {
		return nil
	}
	p, ok := provider.(positionTrackingProvider)
	if !ok {
		r.trackerValid.Store(true)
		return nil
	}
	data, err := p.LoadPositionTrackingData()
	if err != nil {
		return err
	}
	r.tracker.load(data)
	r.trackerValid.Store(true)
	return nil
}

func (r *providerRef) savePositionTracker(provider Provider) error {
	r.trackerMu.Lock()
	defer r.trackerMu.Unlock()
	if r.providerClosed {
		return nil
	}
	return r.savePositionTrackerLocked(provider)
}

func (r *providerRef) savePositionTrackerLocked(provider Provider) error {
	p, ok := provider.(positionTrackingProvider)
	if !ok {
		return nil
	}
	if err := r.loadPositionTrackerLocked(provider); err != nil {
		return err
	}
	version, data := r.trackerVersion.Load(), r.tracker.data()
	if err := p.SavePositionTrackingData(data); err != nil {
		return err
	}
	r.trackerSaved.Store(version)
	return nil
}

func (r *providerRef) closeProvider(provider Provider) (saveErr, closeErr error) {
	if r.shared {
		defer r.finishClose()
	}
	r.trackerMu.Lock()
	defer r.trackerMu.Unlock()
	if r.providerClosed {
		return nil, nil
	}
	if r.writable.Load() && r.trackerVersion.Load() != r.trackerSaved.Load() {
		saveErr = r.savePositionTrackerLocked(provider)
	}
	r.providerClosed = true
	closeErr = provider.Close()
	return
}

var providerRefs = struct {
	sync.Mutex
	m map[providerKey]*providerRef
}{m: make(map[providerKey]*providerRef)}

func retainProvider(provider Provider, writable bool) *providerRef {
	key, ok := providerIdentity(provider)
	if !ok {
		return newProviderRef(provider, writable)
	}
	providerRefs.Lock()
	if ref := providerRefs.m[key]; ref != nil {
		if ref.closed {
			providerRefs.Unlock()
			panic("world: provider is closed")
		}
		if ref.closing != nil {
			closing := ref.closing
			providerRefs.Unlock()
			<-closing
			panic("world: provider is closed")
		}
		ref.refs++
		if writable {
			ref.writable.Store(true)
		}
		providerRefs.Unlock()
		return ref
	}
	ref := newProviderRef(provider, writable)
	ref.key, ref.shared = key, true
	providerRefs.m[key] = ref
	providerRefs.Unlock()
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
	ref.closing = make(chan struct{})
	return true
}

func (r *providerRef) finishClose() {
	providerRefs.Lock()
	r.closed = true
	close(r.closing)
	providerRefs.Unlock()
}

func providerIdentity(provider Provider) (providerKey, bool) {
	value := reflect.ValueOf(provider)
	if !value.IsValid() {
		return providerKey{}, false
	}
	if value.Kind() != reflect.Pointer || value.IsNil() {
		return providerKey{}, false
	}
	return providerKey{provider: provider}, true
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
