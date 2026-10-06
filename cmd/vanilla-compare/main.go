package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
)

type item struct {
	Identifier     string `json:"identifier"`
	RuntimeID      int16  `json:"runtime_id"`
	ComponentBased bool   `json:"component_based"`
	Version        int32  `json:"version"`
}
type snapshot struct {
	ProtocolVersion int     `json:"protocol_version"`
	LibraryVersion  string  `json:"library_game_version"`
	WorldSeed       int64   `json:"world_seed"`
	Dimension       int32   `json:"dimension"`
	WorldGameMode   int32   `json:"world_game_mode"`
	Difficulty      int32   `json:"difficulty"`
	Items           []item  `json:"items"`
	Biomes          []biome `json:"biomes"`
}
type itemDifference struct {
	Identifier string   `json:"identifier"`
	Reference  *item    `json:"reference,omitempty"`
	Candidate  *item    `json:"candidate,omitempty"`
	Changed    []string `json:"changed,omitempty"`
}
type report struct {
	Scope               string `json:"scope"`
	Equal               bool   `json:"equal"`
	ReferenceItemCount  int    `json:"reference_item_count"`
	CandidateItemCount  int    `json:"candidate_item_count"`
	ReferenceBiomeCount int    `json:"reference_biome_count"`
	CandidateBiomeCount int    `json:"candidate_biome_count"`
	Differences         struct {
		ProtocolVersion bool              `json:"protocol_version"`
		WorldSeed       bool              `json:"world_seed"`
		Dimension       bool              `json:"dimension"`
		WorldGameMode   bool              `json:"world_game_mode"`
		Difficulty      bool              `json:"difficulty"`
		Items           []itemDifference  `json:"items"`
		Biomes          []biomeDifference `json:"biomes"`
	} `json:"differences"`
}

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

func run(args []string, out, errOut io.Writer) int {
	fs := flag.NewFlagSet("vanilla-compare", flag.ContinueOnError)
	fs.SetOutput(errOut)
	reference := fs.String("reference", "", "reference snapshot JSON path")
	candidate := fs.String("candidate", "", "candidate snapshot JSON path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *reference == "" || *candidate == "" || fs.NArg() != 0 {
		fmt.Fprintln(errOut, "usage: vanilla-compare -reference path -candidate path")
		return 2
	}
	a, err := load(*reference)
	if err != nil {
		fmt.Fprintf(errOut, "reference: %v\n", err)
		return 2
	}
	b, err := load(*candidate)
	if err != nil {
		fmt.Fprintf(errOut, "candidate: %v\n", err)
		return 2
	}
	r := compare(a, b)
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		fmt.Fprintf(errOut, "output: %v\n", err)
		return 2
	}
	if !r.Equal {
		return 1
	}
	return 0
}

func load(path string) (snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot{}, err
	}
	var decoded struct {
		snapshot
		ProtocolVersion *int32 `json:"protocol_version"`
		WorldSeed       *int64 `json:"world_seed"`
		Dimension       *int32 `json:"dimension"`
		WorldGameMode   *int32 `json:"world_game_mode"`
		Difficulty      *int32 `json:"difficulty"`
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&decoded); err != nil {
		return snapshot{}, fmt.Errorf("invalid snapshot: %w", err)
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return snapshot{}, fmt.Errorf("invalid snapshot: trailing JSON value")
		}
		return snapshot{}, fmt.Errorf("invalid snapshot: trailing data: %w", err)
	}
	if decoded.ProtocolVersion == nil || decoded.WorldSeed == nil || decoded.Dimension == nil || decoded.WorldGameMode == nil || decoded.Difficulty == nil {
		return snapshot{}, fmt.Errorf("invalid snapshot: protocol and world settings are required")
	}
	if *decoded.ProtocolVersion <= 0 {
		return snapshot{}, fmt.Errorf("invalid snapshot: protocol version must be positive")
	}
	s := decoded.snapshot
	s.ProtocolVersion = int(*decoded.ProtocolVersion)
	s.WorldSeed = *decoded.WorldSeed
	s.Dimension = *decoded.Dimension
	s.WorldGameMode = *decoded.WorldGameMode
	s.Difficulty = *decoded.Difficulty
	if s.Items == nil {
		return snapshot{}, fmt.Errorf("invalid snapshot: items is required")
	}
	ids, runtimes := make(map[string]struct{}, len(s.Items)), make(map[int16]string, len(s.Items))
	for _, it := range s.Items {
		if it.Identifier == "" {
			return snapshot{}, fmt.Errorf("invalid snapshot: empty item identifier")
		}
		if _, ok := ids[it.Identifier]; ok {
			return snapshot{}, fmt.Errorf("invalid snapshot: duplicate item identifier %q", it.Identifier)
		}
		if prev, ok := runtimes[it.RuntimeID]; ok {
			return snapshot{}, fmt.Errorf("invalid snapshot: duplicate runtime_id %d for %q and %q", it.RuntimeID, prev, it.Identifier)
		}
		ids[it.Identifier] = struct{}{}
		runtimes[it.RuntimeID] = it.Identifier
	}
	if err := validateBiomes(s.Biomes); err != nil {
		return snapshot{}, err
	}
	return s, nil
}

func compare(a, b snapshot) report {
	r := report{
		Scope:              "initial registry/world-settings only; not behavioral parity",
		ReferenceItemCount: len(a.Items), CandidateItemCount: len(b.Items),
		ReferenceBiomeCount: len(a.Biomes), CandidateBiomeCount: len(b.Biomes),
	}
	d := &r.Differences
	d.ProtocolVersion = a.ProtocolVersion != b.ProtocolVersion
	d.WorldSeed = a.WorldSeed != b.WorldSeed
	d.Dimension = a.Dimension != b.Dimension
	d.WorldGameMode = a.WorldGameMode != b.WorldGameMode
	d.Difficulty = a.Difficulty != b.Difficulty
	am, bm := make(map[string]*item, len(a.Items)), make(map[string]*item, len(b.Items))
	for i := range a.Items {
		am[a.Items[i].Identifier] = &a.Items[i]
	}
	for i := range b.Items {
		bm[b.Items[i].Identifier] = &b.Items[i]
	}
	keys := make([]string, 0, len(am)+len(bm))
	for k := range am {
		keys = append(keys, k)
	}
	for k := range bm {
		if _, ok := am[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		x, xok := am[k]
		y, yok := bm[k]
		if !xok {
			d.Items = append(d.Items, itemDifference{Identifier: k, Candidate: y})
			continue
		}
		if !yok {
			d.Items = append(d.Items, itemDifference{Identifier: k, Reference: x})
			continue
		}
		if *x == *y {
			continue
		}
		changed := make([]string, 0, 3)
		if x.RuntimeID != y.RuntimeID {
			changed = append(changed, "runtime_id")
		}
		if x.ComponentBased != y.ComponentBased {
			changed = append(changed, "component_based")
		}
		if x.Version != y.Version {
			changed = append(changed, "version")
		}
		d.Items = append(d.Items, itemDifference{Identifier: k, Reference: x, Candidate: y, Changed: changed})
	}
	d.Biomes = compareBiomes(a.Biomes, b.Biomes)
	r.Equal = !d.ProtocolVersion && !d.WorldSeed && !d.Dimension && !d.WorldGameMode && !d.Difficulty && len(d.Items) == 0 && len(d.Biomes) == 0
	return r
}
