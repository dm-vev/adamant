package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/df-mc/goleveldb/leveldb/opt"
	"github.com/dm-vev/adamant/net/nbt"
)

const sentinel = "ADAMANT_BLOCK_CATALOG "

type blockState struct {
	Name    string         `nbt:"name"`
	States  map[string]any `nbt:"states"`
	Version int32          `nbt:"version"`
}
type savedStructure struct {
	ID     string
	States int
}
type catalog struct {
	ExpectedTypes, CompletedTypes, RejectedCombinations int
	States                                              []blockState
	Defaults                                            map[string]map[string]any
	Structures                                          []savedStructure
	WorldStates                                         map[int]bool
}
type record struct {
	Event                string                     `json:"event"`
	Schema               int                        `json:"schema"`
	ExpectedTypes        int                        `json:"expectedTypes"`
	CompletedTypes       int                        `json:"completedTypes"`
	IncompleteTypes      int                        `json:"incompleteTypes"`
	Type                 string                     `json:"type"`
	ResolvedType         string                     `json:"resolvedType"`
	Properties           int                        `json:"properties"`
	DefaultType          string                     `json:"defaultType"`
	DefaultStates        map[string]json.RawMessage `json:"defaultStates"`
	States               json.RawMessage            `json:"states"`
	Generated            int                        `json:"generated"`
	Rejected             int                        `json:"rejected"`
	RejectedCombinations int                        `json:"rejectedCombinations"`
	Structures           int                        `json:"structures"`
	WorldStates          int                        `json:"worldStates"`
	ID                   string                     `json:"id"`
	Index                int                        `json:"index"`
	Message              string                     `json:"message"`
}

type structureFile struct {
	FormatVersion int32   `nbt:"format_version"`
	Size          []int32 `nbt:"size"`
	WorldOrigin   []int32 `nbt:"structure_world_origin"`
	Structure     struct {
		BlockIndices []any `nbt:"block_indices"`
		Entities     []any `nbt:"entities"`
		Palette      struct {
			Default struct {
				Blocks            []blockState   `nbt:"block_palette"`
				BlockPositionData map[string]any `nbt:"block_position_data"`
			} `nbt:"default"`
		} `nbt:"palette"`
	} `nbt:"structure"`
}

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "vanilla-block-catalog:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("vanilla-block-catalog", flag.ContinueOnError)
	logPath := flags.String("log", "", "complete BDS catalog console log")
	dbPath := flags.String("db", "", "stopped BDS world's LevelDB directory")
	worldPath := flags.String("world-states", "", "original world palettes exported by vanilla-palettes -nbt")
	version := flags.Int64("version", math.MinInt64, "independently observed BDS palette version")
	output := flags.String("output", "", "new NetworkLittleEndian NBT file; never overwritten")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *logPath == "" || *dbPath == "" || *worldPath == "" || *output == "" || flags.NArg() != 0 || *version < math.MinInt32 || *version > math.MaxInt32 {
		return errors.New("-log, -db, -world-states, -version INT32 and -output are required")
	}
	input, err := os.Open(*logPath)
	if err != nil {
		return err
	}
	defer input.Close()
	parsed, err := parseCatalog(input)
	if err != nil {
		return err
	}
	worldFile, err := os.Open(*worldPath)
	if err != nil {
		return err
	}
	defer worldFile.Close()
	worldStates, err := readWorldStates(worldFile)
	if err != nil {
		return err
	}
	db, err := leveldb.OpenFile(*dbPath, &opt.Options{ReadOnly: true, Compression: opt.SnappyCompression})
	if err != nil {
		return err
	}
	defer db.Close()
	states, err := canonicalStates(parsed, worldStates, int32(*version), func(id string) ([]byte, error) {
		return db.Get([]byte("structuretemplate_"+id), nil)
	})
	if err != nil {
		return err
	}
	encoded, err := encodeStates(states)
	if err != nil {
		return err
	}
	if err := writeNewFileAtomic(*output, encoded); err != nil {
		return err
	}
	hash := sha256.Sum256(encoded)
	summary := struct {
		BlockTypes   int    `json:"block_types"`
		BlockStates  int    `json:"block_states"`
		ScriptStates int    `json:"script_states"`
		WorldStates  int    `json:"world_only_states"`
		Rejected     int    `json:"rejected_combinations"`
		Version      int32  `json:"palette_version"`
		Output       string `json:"output"`
		SHA256       string `json:"sha256"`
	}{len(parsed.Defaults), len(states), len(parsed.States), len(parsed.WorldStates), parsed.RejectedCombinations, int32(*version), *output, hex.EncodeToString(hash[:])}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(summary)
}

func parseCatalog(input io.Reader) (catalog, error) {
	result := catalog{Defaults: make(map[string]map[string]any), WorldStates: make(map[int]bool)}
	seenTypes, seenStates := make(map[string]bool), make(map[string]bool)
	started, complete, worldComplete := false, false, false
	activeType := ""
	activeStates, activeProperties, structureSlots, expectedWorld := 0, 0, 0, 0
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		index := strings.Index(text, sentinel)
		if index < 0 {
			continue
		}
		var r record
		if err := json.Unmarshal([]byte(strings.TrimSpace(text[index+len(sentinel):])), &r); err != nil {
			return catalog{}, fmt.Errorf("line %d: %w", line, err)
		}
		fail := func() (catalog, error) {
			return catalog{}, fmt.Errorf("line %d: inconsistent %q record", line, r.Event)
		}
		count := 0
		if r.Event != "state" && len(r.States) != 0 {
			if err := json.Unmarshal(r.States, &count); err != nil || count < 0 {
				return fail()
			}
		}
		switch r.Event {
		case "start":
			if started || r.Schema != 2 || r.ExpectedTypes <= 0 {
				return fail()
			}
			started = true
			result.ExpectedTypes = r.ExpectedTypes
		case "error", "type-error":
			return catalog{}, fmt.Errorf("line %d: exporter error: %s", line, r.Message)
		case "type-start":
			if !started || complete || activeType != "" || r.Type == "" || seenTypes[r.Type] || r.DefaultType == "" || r.DefaultStates == nil || len(r.DefaultStates) != r.Properties {
				return fail()
			}
			props, err := decodeProperties(r.DefaultStates)
			if err != nil {
				return catalog{}, err
			}
			if old, ok := result.Defaults[r.DefaultType]; ok && (!propertiesMatch(old, props) || len(old) != len(props)) {
				return fail()
			}
			result.Defaults[r.DefaultType] = props
			seenTypes[r.Type] = true
			activeType = r.Type
			activeStates = 0
			activeProperties = r.Properties
		case "state":
			if !started || complete || r.Type != activeType || activeType == "" || r.ResolvedType == "" {
				return fail()
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(r.States, &raw); err != nil || raw == nil || len(raw) != activeProperties {
				return fail()
			}
			props, err := decodeProperties(raw)
			if err != nil {
				return catalog{}, err
			}
			key, err := stateKey(r.ResolvedType, props)
			if err != nil {
				return catalog{}, err
			}
			if seenStates[key] {
				return fail()
			}
			seenStates[key] = true
			result.States = append(result.States, blockState{Name: r.ResolvedType, States: props})
			activeStates++
		case "type-complete":
			if !started || complete || activeType == "" || r.Type != activeType || count != activeStates || r.Rejected < 0 || r.Generated < r.Rejected || r.Generated-r.Rejected < count {
				return fail()
			}
			result.CompletedTypes++
			result.RejectedCombinations += r.Rejected
			activeType = ""
		case "structure":
			if !started || complete || r.ID != fmt.Sprintf("adamant_catalog:blocks_%d", len(result.Structures)) || count < 1 || count > 4096 {
				return fail()
			}
			result.Structures = append(result.Structures, savedStructure{r.ID, count})
			structureSlots += count
		case "complete":
			if !started || complete || activeType != "" || result.CompletedTypes != result.ExpectedTypes || r.ExpectedTypes != result.ExpectedTypes || r.CompletedTypes != result.CompletedTypes || r.IncompleteTypes != 0 || count != len(result.States) || count != structureSlots || r.Structures != len(result.Structures) || r.RejectedCombinations != result.RejectedCombinations || r.WorldStates < 0 || r.WorldStates > count {
				return fail()
			}
			complete = true
			expectedWorld = r.WorldStates
		case "world-state":
			if !complete || worldComplete || r.Index < 0 || r.Index >= len(result.States) || result.WorldStates[r.Index] || r.Type != result.States[r.Index].Name {
				return fail()
			}
			result.WorldStates[r.Index] = true
		case "world-complete":
			if !complete || worldComplete || count != expectedWorld || count != len(result.WorldStates) {
				return fail()
			}
			worldComplete = true
		default:
			return fail()
		}
	}
	if err := scanner.Err(); err != nil {
		return catalog{}, err
	}
	if !complete || !worldComplete {
		return catalog{}, errors.New("catalog or world capture is incomplete")
	}
	return result, nil
}

func decodeProperties(raw map[string]json.RawMessage) (map[string]any, error) {
	props := make(map[string]any, len(raw))
	for name, value := range raw {
		if name == "" {
			return nil, errors.New("empty property name")
		}
		v, err := decodeProperty(value)
		if err != nil {
			return nil, fmt.Errorf("property %q: %w", name, err)
		}
		props[name] = v
	}
	return props, nil
}
func decodeProperty(raw json.RawMessage) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	switch v := value.(type) {
	case bool:
		if v {
			return uint8(1), nil
		}
		return uint8(0), nil
	case string:
		return v, nil
	case json.Number:
		i, err := v.Int64()
		if err != nil || i < math.MinInt32 || i > math.MaxInt32 {
			return nil, fmt.Errorf("not an int32: %s", v)
		}
		return int32(i), nil
	default:
		return nil, fmt.Errorf("unsupported state property type %T", value)
	}
}

func readWorldStates(reader io.Reader) (map[string][]blockState, error) {
	result := make(map[string][]blockState)
	buffer := bufio.NewReader(reader)
	decoder := nbt.NewDecoderWithEncoding(buffer, nbt.NetworkLittleEndian)
	for {
		if _, err := buffer.Peek(1); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		var state blockState
		err := decoder.Decode(&state)
		if err != nil {
			return nil, fmt.Errorf("decode original world palette: %w", err)
		}
		result[state.Name] = append(result[state.Name], state)
	}
	return result, nil
}

func canonicalStates(c catalog, worldStates map[string][]blockState, version int32, load func(string) ([]byte, error)) ([]blockState, error) {
	type entry struct {
		state        blockState
		key          string
		defaultState bool
	}
	entries := make([]entry, 0, len(c.States))
	unique, defaults := make(map[string]bool), make(map[string]bool)
	sourceIndex := 0
	for _, meta := range c.Structures {
		data, err := load(meta.ID)
		if err != nil {
			return nil, fmt.Errorf("load %s: %w", meta.ID, err)
		}
		var structure structureFile
		reader := bytes.NewReader(data)
		if err := nbt.NewDecoderWithEncoding(reader, nbt.LittleEndian).Decode(&structure); err != nil {
			return nil, fmt.Errorf("decode %s: %w", meta.ID, err)
		}
		if reader.Len() != 0 || structure.FormatVersion != 2 || len(structure.Size) != 3 || structure.Size[0] != 16 || structure.Size[1] != 16 || structure.Size[2] != 16 || len(structure.Structure.BlockIndices) < 1 || len(structure.Structure.BlockIndices) > 2 {
			return nil, fmt.Errorf("invalid structure %s: format=%d size=%v layers=%d trailing=%d", meta.ID, structure.FormatVersion, structure.Size, len(structure.Structure.BlockIndices), reader.Len())
		}
		for _, layer := range structure.Structure.BlockIndices {
			values := reflect.ValueOf(layer)
			if values.Kind() != reflect.Array || values.Len() != 4096 || values.Type().Elem().Kind() != reflect.Int32 {
				return nil, fmt.Errorf("invalid structure block-index array: %s", meta.ID)
			}
		}
		indices := reflect.ValueOf(structure.Structure.BlockIndices[0])
		palette := structure.Structure.Palette.Default.Blocks
		for slot := 0; slot < meta.States; slot++ {
			if sourceIndex >= len(c.States) {
				return nil, errors.New("more saved slots than source states")
			}
			source := &c.States[sourceIndex]
			var state *blockState
			if c.WorldStates[sourceIndex] {
				candidates := worldStates[source.Name]
				for i := range candidates {
					if propertiesMatch(candidates[i].States, source.States) {
						state = &candidates[i]
						break
					}
				}
				if state == nil {
					return nil, fmt.Errorf("world palette did not preserve source state %d (%s)", sourceIndex, source.Name)
				}
			} else {
				x, y, z := slot&15, (slot>>8)&15, (slot>>4)&15
				rid := indices.Index(x*256 + y*16 + z).Int()
				if rid < 0 || int(rid) >= len(palette) {
					return nil, fmt.Errorf("invalid palette index %d at source state %d", rid, sourceIndex)
				}
				state = &palette[rid]
			}
			if state.Name != source.Name || state.Version != version || !propertiesMatch(state.States, source.States) {
				return nil, fmt.Errorf("persisted/source state mismatch at %d (%s)", sourceIndex, source.Name)
			}
			sourceIndex++
			key, err := stateKey(state.Name, state.States)
			if err != nil {
				return nil, err
			}
			if unique[key] {
				continue
			}
			unique[key] = true
			initial, ok := c.Defaults[state.Name]
			if !ok {
				return nil, fmt.Errorf("missing default for %s", state.Name)
			}
			isDefault := propertiesMatch(state.States, initial)
			if isDefault {
				defaults[state.Name] = true
			}
			entries = append(entries, entry{*state, key, isDefault})
		}
	}
	if sourceIndex != len(c.States) || len(defaults) != len(c.Defaults) {
		return nil, errors.New("persisted catalog is missing states or block defaults")
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := &entries[i], &entries[j]
		if a.state.Name != b.state.Name {
			return a.state.Name < b.state.Name
		}
		if a.defaultState != b.defaultState {
			return a.defaultState
		}
		return a.key < b.key
	})
	states := make([]blockState, len(entries))
	for i := range entries {
		states[i] = entries[i].state
	}
	return states, nil
}

// Script API exposes compatibility properties (for example old_log_type), which
// are not persisted. Every actual NBT property must still agree, including its type.
func propertiesMatch(actual, script map[string]any) bool {
	if actual == nil || script == nil {
		return false
	}
	for name, value := range actual {
		switch value.(type) {
		case uint8, int32, string:
		default:
			return false
		}
		if script[name] != value {
			return false
		}
	}
	return true
}
func stateKey(name string, states map[string]any) (string, error) {
	data, err := json.Marshal(states)
	if err != nil {
		return "", err
	}
	return name + "\x00" + string(data), nil
}
func encodeStates(states []blockState) ([]byte, error) {
	var output bytes.Buffer
	encoder := nbt.NewEncoderWithEncoding(&output, nbt.NetworkLittleEndian)
	for i := range states {
		if err := encoder.Encode(&states[i]); err != nil {
			return nil, err
		}
	}
	return output.Bytes(), nil
}
func writeNewFileAtomic(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".vanilla-block-catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Link(file.Name(), path)
}
