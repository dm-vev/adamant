package playerdb

import (
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/google/uuid"
	"os"
)

// Provider is a player data provider that uses a LevelDB database to store data. The data passed on
// will first be converted to make sure it can be marshaled into JSON. This JSON (in bytes) will then
// be stored in the database under a key that is the byte representation of the player's UUID.
type Provider struct {
	db *leveldb.DB
}

// NewProvider creates a new player data provider that saves and loads data using
// a LevelDB database.
func NewProvider(path string) (*Provider, error) {
	if err := os.MkdirAll(path, 0777); err != nil {
		return nil, err
	}
	db, err := leveldb.OpenFile(path, &opt.Options{Compression: opt.SnappyCompression})
	if err != nil {
		return nil, err
	}
	return &Provider{db: db}, nil
}

// OpenReadOnly opens the data a Provider saved under the path passed, to
// read it only: Save fails. It fails if there is none. A server keeping
// its players in the world database reads the players folder so, as the
// fallback of a WorldProvider.
func OpenReadOnly(path string) (*Provider, error) {
	db, err := leveldb.OpenFile(path, &opt.Options{Compression: opt.SnappyCompression, ReadOnly: true, ErrorIfMissing: true})
	if err != nil {
		return nil, err
	}
	return &Provider{db: db}, nil
}

// Save ...
func (p *Provider) Save(id uuid.UUID, d player.Config, w *world.World) error {
	b, err := Encode(d, w)
	if err != nil {
		return err
	}
	return p.db.Put(id[:], b, nil)
}

// Load ...
func (p *Provider) Load(id uuid.UUID, world func(world.Dimension) *world.World) (player.Config, *world.World, error) {
	b, err := p.db.Get(id[:], nil)
	if err != nil {
		return player.Config{}, nil, err
	}
	return Decode(b, id, world)
}

// Players calls yield with the UUID, the name and the XUID of each record
// the provider holds, reading only those fields, until yield returns false.
// A record that does not decode is skipped.
func (p *Provider) Players(yield func(id uuid.UUID, name, xuid string) bool) error {
	it := p.db.NewIterator(nil, nil)
	defer it.Release()
	for it.Next() {
		key := it.Key()
		if len(key) != len(uuid.UUID{}) {
			continue
		}
		name, xuid, err := Identity(it.Value())
		if err != nil {
			continue
		}
		if !yield(uuid.UUID(key), name, xuid) {
			break
		}
	}
	return it.Error()
}

var _ player.Lister = (*Provider)(nil)

// Close ...
func (p *Provider) Close() error {
	return p.db.Close()
}
