package mcdb

import (
	"errors"
	"fmt"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/net/nbt"
)

// keyStructureTemplate prefixes the name of a structure template saved by a
// structure block, as Bedrock Dedicated Server keys them.
const keyStructureTemplate = "structuretemplate_"

// LoadStructureTemplate returns the .mcstructure NBT of the structure template
// saved under the name, or false if there is none.
func (db *DB) LoadStructureTemplate(name string) (map[string]any, bool, error) {
	db.lmu.Lock()
	defer db.lmu.Unlock()
	b, err := db.ldb.Get([]byte(keyStructureTemplate+name), nil)
	if errors.Is(err, leveldb.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("load structure template %q: %w", name, err)
	}
	var m map[string]any
	if err := nbt.UnmarshalEncoding(b, &m, nbt.LittleEndian); err != nil {
		return nil, false, fmt.Errorf("decode structure template %q: %w", name, err)
	}
	return m, true, nil
}

// SaveStructureTemplate saves the .mcstructure NBT of a structure template
// under the name.
func (db *DB) SaveStructureTemplate(name string, data map[string]any) error {
	b, err := nbt.MarshalEncoding(data, nbt.LittleEndian)
	if err != nil {
		return fmt.Errorf("encode structure template %q: %w", name, err)
	}
	db.lmu.Lock()
	defer db.lmu.Unlock()
	if err := db.ldb.Put([]byte(keyStructureTemplate+name), b, nil); err != nil {
		return fmt.Errorf("save structure template %q: %w", name, err)
	}
	return nil
}
