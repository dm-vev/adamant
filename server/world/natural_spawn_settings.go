package world

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/dm-vev/adamant/server/internal/gamedata"
)

// NaturalSpawnEntry is one source-defined weighted spawn row from a biome.
type NaturalSpawnEntry struct {
	ID       string
	Category string
	Weight   int
	MinCount int
	MaxCount int
}

// NaturalSpawnCost is a MobSpawnSettings.MobSpawnCost: the charge a mob of
// a type carries for the spawn potential and the energy its spawning may
// add.
type NaturalSpawnCost struct {
	Charge, EnergyBudget float64
}

var (
	spawnSettingsOnce sync.Once
	spawnSettings     map[string][]NaturalSpawnEntry
	spawnCosts        map[string]map[string]NaturalSpawnCost
	spawnCostTypes    map[string]struct{}
	biomeFloats       map[string]map[string]float32
	spawnSettingsErr  error
)

// HasJavaSpawnCost reports whether a mob of the entity type passed has a
// spawn cost in any biome, so that callers skip finding the biome of mobs
// that have none anywhere, as most have.
func HasSpawnCost(id string) bool {
	spawnSettingsOnce.Do(loadBiomeSpawnSettings)
	_, ok := spawnCostTypes[id]
	return ok
}

// JavaBiomeSpawnCosts returns the spawn costs of the biome named, keyed by
// entity type.
func BiomeSpawnCosts(biome string) map[string]NaturalSpawnCost {
	spawnSettingsOnce.Do(loadBiomeSpawnSettings)
	return spawnCosts[strings.TrimPrefix(biome, "minecraft:")]
}

// JavaBiomeAttribute returns a float environment attribute of the biome
// named, such as minecraft:gameplay/surface_slime_spawn_chance, or def if
// the biome does not set it.
func BiomeAttribute(biome, attribute string, def float32) float32 {
	spawnSettingsOnce.Do(loadBiomeSpawnSettings)
	if v, ok := biomeFloats[strings.TrimPrefix(biome, "minecraft:")][attribute]; ok {
		return v
	}
	return def
}

var ErrBiomeNaturalSpawnsUnavailable = errors.New("biome natural spawn settings unavailable")

// JavaBiomeNaturalSpawns returns the exact natural mob spawn rows embedded from
// the Java 26.3 biome registry for the biome name passed.
func BiomeNaturalSpawns(biome string) ([]NaturalSpawnEntry, error) {
	spawnSettingsOnce.Do(loadBiomeSpawnSettings)
	if spawnSettingsErr != nil {
		return nil, spawnSettingsErr
	}
	entries, ok := spawnSettings[strings.TrimPrefix(biome, "minecraft:")]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrBiomeNaturalSpawnsUnavailable, biome)
	}
	return entries, nil
}

func loadBiomeSpawnSettings() {
	files, err := fs.Glob(gamedata.Assets(), "assets/data/minecraft/worldgen/biome/*.json")
	if err != nil {
		spawnSettingsErr = err
		return
	}
	spawnSettings = make(map[string][]NaturalSpawnEntry, len(files))
	spawnCosts = make(map[string]map[string]NaturalSpawnCost, len(files))
	spawnCostTypes = make(map[string]struct{})
	biomeFloats = make(map[string]map[string]float32, len(files))
	for _, name := range files {
		data, err := gamedata.Assets().ReadFile(name)
		if err != nil {
			spawnSettingsErr = err
			return
		}
		var root struct {
			Attributes map[string]json.RawMessage `json:"attributes"`
		}
		if err := json.Unmarshal(data, &root); err != nil {
			spawnSettingsErr = fmt.Errorf("decode biome spawn settings %s: %w", name, err)
			return
		}
		attribute, ok := root.Attributes["minecraft:gameplay/natural_mob_spawns"]
		if !ok {
			spawnSettingsErr = fmt.Errorf("biome %s has no natural mob spawn settings", filepath.Base(name))
			return
		}
		biome := strings.TrimSuffix(filepath.Base(name), ".json")
		for key, raw := range root.Attributes {
			var v float32
			if json.Unmarshal(raw, &v) == nil {
				if biomeFloats[biome] == nil {
					biomeFloats[biome] = map[string]float32{}
				}
				biomeFloats[biome][key] = v
			}
		}
		var settings struct {
			Argument struct {
				Spawns json.RawMessage `json:"spawns_by_category"`
				Costs  map[string]struct {
					Charge       float64 `json:"charge"`
					EnergyBudget float64 `json:"energy_budget"`
				} `json:"spawn_costs"`
			} `json:"argument"`
		}
		if err := json.Unmarshal(attribute, &settings); err != nil {
			spawnSettingsErr = fmt.Errorf("decode Java biome natural mob spawns %s: %w", name, err)
			return
		}
		entries, err := decodeSpawnCategories(settings.Argument.Spawns, name)
		if err != nil {
			spawnSettingsErr = err
			return
		}
		spawnSettings[biome] = entries
		if len(settings.Argument.Costs) != 0 {
			costs := make(map[string]NaturalSpawnCost, len(settings.Argument.Costs))
			for id, c := range settings.Argument.Costs {
				costs[id] = NaturalSpawnCost{Charge: c.Charge, EnergyBudget: c.EnergyBudget}
			}
			spawnCosts[biome] = costs
			for id := range costs {
				spawnCostTypes[id] = struct{}{}
			}
		}
	}
}

func decodeSpawnCategories(raw json.RawMessage, biomePath string) ([]NaturalSpawnEntry, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, fmt.Errorf("invalid Java spawn categories in %s", filepath.Base(biomePath))
	}
	entries := make([]NaturalSpawnEntry, 0, 32)
	for decoder.More() {
		token, err := decoder.Token()
		category, ok := token.(string)
		if err != nil || !ok {
			return nil, fmt.Errorf("invalid Java spawn category in %s", filepath.Base(biomePath))
		}
		if !validSpawnCategory(category) {
			return nil, fmt.Errorf("unknown Java spawn category %q in %s", category, filepath.Base(biomePath))
		}
		var rowsJSON json.RawMessage
		if err := decoder.Decode(&rowsJSON); err != nil {
			return nil, fmt.Errorf("decode Java spawn category %s in %s: %w", category, filepath.Base(biomePath), err)
		}
		if rows := bytes.TrimSpace(rowsJSON); len(rows) == 0 || rows[0] != '[' {
			return nil, fmt.Errorf("invalid Java spawn rows for category %s in %s", category, filepath.Base(biomePath))
		}
		var rows []struct {
			Type   *string         `json:"type"`
			Weight *int            `json:"weight"`
			Count  json.RawMessage `json:"count"`
		}
		if err := json.Unmarshal(rowsJSON, &rows); err != nil {
			return nil, fmt.Errorf("decode Java spawn category %s in %s: %w", category, filepath.Base(biomePath), err)
		}
		for _, row := range rows {
			if row.Type == nil || *row.Type == "" || row.Weight == nil || len(row.Count) == 0 {
				return nil, fmt.Errorf("incomplete Java spawn row in category %s in %s", category, filepath.Base(biomePath))
			}
			minCount, maxCount, err := decodeSpawnCount(row.Count)
			if err != nil {
				return nil, fmt.Errorf("invalid Java spawn count for %s in %s: %w", *row.Type, filepath.Base(biomePath), err)
			}
			entries = append(entries, NaturalSpawnEntry{ID: *row.Type, Category: category, Weight: *row.Weight, MinCount: minCount, MaxCount: maxCount})
		}
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("invalid Java spawn categories in %s: %w", filepath.Base(biomePath), err)
	}
	return entries, nil
}

func validSpawnCategory(category string) bool {
	switch category {
	case "monster", "creature", "ambient", "axolotls", "underground_water_creature", "water_creature", "water_ambient", "misc":
		return true
	default:
		return false
	}
}

func decodeSpawnCount(raw json.RawMessage) (int, int, error) {
	trimmed := bytes.TrimSpace(raw)
	var count int
	if len(trimmed) > 0 && (trimmed[0] == '-' || trimmed[0] >= '0' && trimmed[0] <= '9') {
		if err := json.Unmarshal(trimmed, &count); err == nil {
			return count, count, nil
		}
	}
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return 0, 0, errors.New("count must be an integer or provider object")
	}
	var provider struct {
		Type         *string `json:"type"`
		MinInclusive *int    `json:"min_inclusive"`
		MaxInclusive *int    `json:"max_inclusive"`
		Value        *int    `json:"value"`
	}
	if err := json.Unmarshal(trimmed, &provider); err != nil {
		return 0, 0, err
	}
	if provider.Type == nil {
		return 0, 0, errors.New("count provider has no type")
	}
	switch *provider.Type {
	case "minecraft:constant":
		if provider.Value == nil {
			return 0, 0, errors.New("constant count provider has no value")
		}
		return *provider.Value, *provider.Value, nil
	case "minecraft:uniform":
		if provider.MinInclusive == nil || provider.MaxInclusive == nil || *provider.MaxInclusive < *provider.MinInclusive {
			return 0, 0, errors.New("invalid uniform count bounds")
		}
		return *provider.MinInclusive, *provider.MaxInclusive, nil
	default:
		return 0, 0, fmt.Errorf("unsupported count provider %q", *provider.Type)
	}
}
