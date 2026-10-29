// Command redstone-structure-readback reads saved audit structure compounds
// from a stopped native BDS database. It opens LevelDB read-only and emits
// JSON lines; it never starts a server or modifies structures.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/df-mc/goleveldb/leveldb/util"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	path := flag.String("db", "", "stopped native BDS database directory")
	prefix := flag.String("prefix", "structuretemplate_mystructure:audit_", "structure key prefix")
	wal := flag.Bool("immutable-audit-wal", false, "verify and include immutable audit snapshots from native write-ahead journals")
	flag.Parse()
	if *path == "" || !strings.HasPrefix(*prefix, "structuretemplate_") || flag.NArg() != 0 {
		return fmt.Errorf("-db and a structuretemplate_ prefix are required")
	}
	strict := opt.StrictAll
	if *wal {
		if !strings.HasPrefix(*prefix, "structuretemplate_mystructure:audit_") {
			return fmt.Errorf("immutable journal readback requires an audit snapshot prefix")
		}
		// The dependency rejects a valid native first WAL batch when its
		// sequence precedes the manifest's last sequence. The explicit audit
		// mode verifies every physical/logical WAL record independently below.
		strict &^= opt.StrictJournal
	}
	db, err := leveldb.OpenFile(*path, &opt.Options{ReadOnly: true, Compression: opt.SnappyCompression, Strict: strict})
	if err != nil {
		return err
	}
	defer db.Close()
	iterator := db.NewIterator(util.BytesPrefix([]byte(*prefix)), nil)
	defer iterator.Release()
	records := map[string][]byte{}
	for iterator.Next() {
		records[string(iterator.Key())] = bytes.Clone(iterator.Value())
	}
	if err := iterator.Error(); err != nil {
		return err
	}
	if *wal {
		before := len(records)
		batches, err := readAuditJournals(*path, *prefix, records)
		if err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "verified %d WAL batches; %d journal-only immutable snapshots; %d total snapshots\n", batches, len(records)-before, len(records))
	}
	names := make([]string, 0, len(records))
	for key := range records {
		names = append(names, key)
	}
	sort.Strings(names)
	output := json.NewEncoder(os.Stdout)
	for _, key := range names {
		reader := bytes.NewReader(records[key])
		var structure map[string]any
		if err := nbt.NewDecoderWithEncoding(reader, nbt.LittleEndian).Decode(&structure); err != nil {
			return fmt.Errorf("decode %s: %w", key, err)
		}
		if reader.Len() != 0 {
			return fmt.Errorf("trailing bytes in %s", key)
		}
		if err := output.Encode(map[string]any{"name": strings.TrimPrefix(key, "structuretemplate_"), "structure": structure, "state_types": stateTagTypes(structure)}); err != nil {
			return err
		}
	}
	return nil
}

func stateTagTypes(structure map[string]any) []map[string]string {
	contents, _ := structure["structure"].(map[string]any)
	palette, _ := contents["palette"].(map[string]any)
	defaults, _ := palette["default"].(map[string]any)
	blocks, _ := defaults["block_palette"].([]any)
	result := make([]map[string]string, 0, len(blocks))
	for _, raw := range blocks {
		block, _ := raw.(map[string]any)
		states, _ := block["states"].(map[string]any)
		types := make(map[string]string, len(states))
		for name, value := range states {
			types[name] = fmt.Sprintf("%T", value)
		}
		result = append(result, types)
	}
	return result
}
