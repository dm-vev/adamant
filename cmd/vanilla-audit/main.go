package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"sort"

	_ "github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	_ "github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type audit struct {
	ProtocolVersion   int32              `json:"protocol_version"`
	GameVersion       string             `json:"game_version"`
	ParityVerified    bool               `json:"parity_verified"`
	RegistryCoverage  registryCoverage   `json:"registry_coverage"`
	EntityIdentifiers []string           `json:"entity_identifiers"`
	Enchantments      []enchantmentEntry `json:"enchantments"`
}

type registryCoverage struct {
	TotalBlockStates       int            `json:"total_block_states"`
	ImplementedBlockStates int            `json:"implemented_block_states"`
	UnknownBlockStates     int            `json:"unknown_block_states"`
	MissingBlocks          []missingBlock `json:"missing_blocks"`
}

type missingBlock struct {
	Identifier string         `json:"identifier"`
	StateCount int            `json:"missing_state_count"`
	States     []missingState `json:"states"`
}

type missingState struct {
	Properties map[string]any `json:"properties"`
	sortKey    string         `json:"-"`
}

type enchantmentEntry struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "vanilla-audit:", err)
		os.Exit(1)
	}
}

func run(out io.Writer) error {
	registry := world.DefaultBlockRegistry
	registry.Finalize()

	blocks := registry.Blocks()
	missingByName := make(map[string][]missingState)
	for _, b := range blocks {
		typ := reflect.TypeOf(b)
		if typ.PkgPath() != "github.com/df-mc/dragonfly/server/world" || typ.Name() != "unknownBlock" {
			continue
		}
		name, properties := b.EncodeBlock()
		stateKey, err := json.Marshal(properties)
		if err != nil {
			return fmt.Errorf("encode properties for unknown block state %q: %w", name, err)
		}
		missingByName[name] = append(missingByName[name], missingState{
			Properties: properties,
			sortKey:    string(stateKey),
		})
	}

	missing := make([]missingBlock, 0, len(missingByName))
	unknownCount := 0
	for identifier, states := range missingByName {
		unknownCount += len(states)
		sort.Slice(states, func(i, j int) bool {
			return states[i].sortKey < states[j].sortKey
		})
		missing = append(missing, missingBlock{Identifier: identifier, StateCount: len(states), States: states})
	}
	sort.Slice(missing, func(i, j int) bool { return missing[i].Identifier < missing[j].Identifier })

	entities := entity.DefaultRegistry.Types()
	entityIdentifiers := make([]string, 0, len(entities))
	for _, entityType := range entities {
		entityIdentifiers = append(entityIdentifiers, entityType.EncodeEntity())
	}
	slices.Sort(entityIdentifiers)

	enchantments := item.Enchantments()
	enchantmentEntries := make([]enchantmentEntry, 0, len(enchantments))
	for _, enchantment := range enchantments {
		id, ok := item.EnchantmentID(enchantment)
		if !ok {
			return fmt.Errorf("missing ID for registered enchantment %q", enchantment.Name())
		}
		enchantmentEntries = append(enchantmentEntries, enchantmentEntry{ID: id, Name: enchantment.Name()})
	}

	result := audit{
		ProtocolVersion: protocol.CurrentProtocol,
		GameVersion:     protocol.CurrentVersion,
		ParityVerified:  false,
		RegistryCoverage: registryCoverage{
			TotalBlockStates:       len(blocks),
			ImplementedBlockStates: len(blocks) - unknownCount,
			UnknownBlockStates:     unknownCount,
			MissingBlocks:          missing,
		},
		EntityIdentifiers: entityIdentifiers,
		Enchantments:      enchantmentEntries,
	}
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("encode audit JSON: %w", err)
	}
	return nil
}
