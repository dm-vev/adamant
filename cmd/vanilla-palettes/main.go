package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

const subChunkTag byte = '/'

type blockState struct {
	Name    string         `json:"name" nbt:"name"`
	States  map[string]any `json:"states" nbt:"states"`
	Version int32          `json:"version" nbt:"version"`
}

type report struct {
	SubChunks   int           `json:"subchunk_count"`
	Versions    map[int32]int `json:"palette_version_counts"`
	BlockStates []blockState  `json:"block_states"`
}

func main() {
	path := flag.String("db", "", "Bedrock LevelDB directory (the world's db directory)")
	nbtPath := flag.String("nbt", "", "also write original palettes as NetworkLittleEndian NBT to a new file")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "-db is required")
		os.Exit(2)
	}
	if err := run(*path, *nbtPath, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(path, nbtPath string, out io.Writer) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat LevelDB directory %q: %w", path, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("LevelDB path %q is not a directory", path)
	}
	db, err := leveldb.OpenFile(path, &opt.Options{ReadOnly: true, Compression: opt.SnappyCompression, BlockSize: 16 * opt.KiB})
	if err != nil {
		return fmt.Errorf("open read-only LevelDB %q: %w", path, err)
	}
	defer db.Close()

	r := report{Versions: make(map[int32]int)}
	unique := make(map[string]blockState)
	it := db.NewIterator(nil, nil)
	for it.Next() {
		key := it.Key()
		dim, ok := subChunkDimension(key)
		if !ok {
			continue
		}
		value := it.Value()
		if len(value) == 0 {
			it.Release()
			return fmt.Errorf("subchunk key %x (%s): empty value", key, dim)
		}
		if value[0] != 9 {
			it.Release()
			return fmt.Errorf("subchunk key %x (%s): unsupported version %d", key, dim, value[0])
		}
		states, err := decodeSubChunk9(value)
		if err != nil {
			it.Release()
			return fmt.Errorf("subchunk key %x (%s): %w", key, dim, err)
		}
		r.SubChunks++
		for _, state := range states {
			r.Versions[state.Version]++
			id, err := json.Marshal(state)
			if err != nil {
				it.Release()
				return fmt.Errorf("encode state identity: %w", err)
			}
			unique[string(id)] = state
		}
	}
	if err := it.Error(); err != nil {
		it.Release()
		return fmt.Errorf("iterate LevelDB: %w", err)
	}
	it.Release()
	keys := make([]string, 0, len(unique))
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	r.BlockStates = make([]blockState, len(keys))
	for i, key := range keys {
		r.BlockStates[i] = unique[key]
	}
	if nbtPath != "" {
		if err := writePaletteNBT(nbtPath, r.BlockStates); err != nil {
			return err
		}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("write JSON: %w", err)
	}
	return nil
}

func writePaletteNBT(path string, states []blockState) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".vanilla-palettes-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	buffer := bufio.NewWriter(file)
	encoder := nbt.NewEncoderWithEncoding(buffer, nbt.NetworkLittleEndian)
	for i := range states {
		if err := encoder.Encode(&states[i]); err != nil {
			return err
		}
	}
	if err := buffer.Flush(); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Link(file.Name(), path)
}

// subChunkDimension validates Bedrock's chunk coordinates, optional dimension ID,
// subchunk tag, and trailing Y byte. Dimension IDs are stored only for non-overworld keys.
func subChunkDimension(key []byte) (string, bool) {
	if len(key) != 10 && len(key) != 14 || key[len(key)-2] != subChunkTag {
		return "", false
	}
	if len(key) == 10 {
		return "overworld", true
	}
	switch binary.LittleEndian.Uint32(key[8:12]) {
	case 1:
		return "nether", true
	case 2:
		return "end", true
	default:
		return "", false
	}
}

func decodeSubChunk9(data []byte) ([]blockState, error) {
	r := bytes.NewReader(data)
	version, err := r.ReadByte()
	if err != nil || version != 9 {
		return nil, fmt.Errorf("expected subchunk version 9")
	}
	storageCount, err := r.ReadByte()
	if err != nil {
		return nil, fmt.Errorf("read storage count: %w", err)
	}
	if _, err = r.ReadByte(); err != nil {
		return nil, fmt.Errorf("read signed subchunk Y header: %w", err)
	}
	var states []blockState
	for storage := 0; storage < int(storageCount); storage++ {
		bitsByte, err := r.ReadByte()
		if err != nil {
			return nil, fmt.Errorf("storage %d read palette bit header: %w", storage, err)
		}
		bits := bitsByte >> 1
		if bitsByte&1 != 0 {
			return nil, fmt.Errorf("storage %d has unsupported runtime palette flag", storage)
		}
		switch bits {
		case 0, 1, 2, 3, 4, 5, 6, 8, 16:
		default:
			return nil, fmt.Errorf("storage %d has invalid palette bit width %d", storage, bits)
		}
		wordsPerWord := 0
		if bits != 0 {
			wordsPerWord = 32 / int(bits)
		}
		wordCount := 0
		if wordsPerWord != 0 {
			wordCount = (4096 + wordsPerWord - 1) / wordsPerWord
		}
		if wordCount > r.Len()/4 {
			return nil, fmt.Errorf("storage %d packed words truncated: need %d words, have %d bytes", storage, wordCount, r.Len())
		}
		if _, err := r.Seek(int64(wordCount*4), io.SeekCurrent); err != nil {
			return nil, fmt.Errorf("storage %d skip packed words: %w", storage, err)
		}
		count := uint32(1)
		if bits != 0 {
			if err := binary.Read(r, binary.LittleEndian, &count); err != nil {
				return nil, fmt.Errorf("storage %d read palette count: %w", storage, err)
			}
		}
		if count == 0 {
			return nil, fmt.Errorf("storage %d has zero palette entries", storage)
		}
		if bits != 0 && uint64(count) > uint64(1)<<bits {
			return nil, fmt.Errorf("storage %d palette count %d exceeds %d-bit capacity", storage, count, bits)
		}
		for entry := uint32(0); entry < count; entry++ {
			var value map[string]any
			if err := nbt.NewDecoderWithEncoding(r, nbt.LittleEndian).Decode(&value); err != nil {
				return nil, fmt.Errorf("storage %d palette entry %d/%d NBT: %w", storage, entry+1, count, err)
			}
			name, ok := value["name"].(string)
			if !ok || name == "" {
				return nil, fmt.Errorf("storage %d palette entry %d has missing/invalid name", storage, entry)
			}
			version, ok := value["version"].(int32)
			if !ok {
				return nil, fmt.Errorf("storage %d palette entry %d has missing/invalid int32 version", storage, entry)
			}
			props, ok := value["states"].(map[string]any)
			if !ok {
				return nil, fmt.Errorf("storage %d palette entry %d has missing/invalid states compound", storage, entry)
			}
			states = append(states, blockState{Name: name, States: props, Version: version})
		}
	}
	if r.Len() != 0 {
		return nil, fmt.Errorf("%d trailing bytes after palette data", r.Len())
	}
	return states, nil
}
