package mcdb

import (
	"strings"

	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/google/uuid"
)

// keyPlayerPrefix prefixes the records of players the DB holds: the key of
// the record of a player is the prefix and the UUID the server keeps its
// data by, as text (adamant:player:6d1c62f5-1e30-4a4b-9a43-6f4b8bd0a7a1).
// The DB holds a record as it is given: playerdb.WorldProvider stores the
// JSON of playerdb.
const keyPlayerPrefix = "adamant:player:"

func playerKey(id uuid.UUID) []byte { return []byte(keyPlayerPrefix + id.String()) }

// LoadPlayer returns the record of the player keyed id. If the DB holds
// none, errors.Is(err, leveldb.ErrNotFound) is true.
func (db *DB) LoadPlayer(id uuid.UUID) ([]byte, error) {
	return db.ldb.Get(playerKey(id), nil)
}

// SavePlayer saves record as the record of the player keyed id.
func (db *DB) SavePlayer(id uuid.UUID, record []byte) error {
	return db.ldb.Put(playerKey(id), record, nil)
}

// PlayerRecords calls yield with the key and the record of each player the
// DB holds a record of, until yield returns false.
func (db *DB) PlayerRecords(yield func(id uuid.UUID, record []byte) bool) error {
	it := db.ldb.NewIterator(util.BytesPrefix([]byte(keyPlayerPrefix)), nil)
	defer it.Release()
	for it.Next() {
		id, err := uuid.Parse(strings.TrimPrefix(string(it.Key()), keyPlayerPrefix))
		if err != nil {
			continue
		}
		if !yield(id, it.Value()) {
			break
		}
	}
	return it.Error()
}
