package world

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/goleveldb/leveldb"
)

// gameRule describes a Java 26.3 GameRules entry: its type, default and the
// valid integer range.
type gameRule struct {
	boolean  bool
	def      any
	min, max int
}

func boolRule(def bool) gameRule { return gameRule{boolean: true, def: def} }
func intRule(def, min, max int) gameRule {
	return gameRule{def: def, min: min, max: max}
}

const maxRuleInt = 1<<31 - 1

// gameRules is the Java 26.3 GameRules registry with its defaults.
var gameRules = map[string]gameRule{
	"advance_time":                         boolRule(true),
	"advance_weather":                      boolRule(true),
	"allow_entering_nether_using_portals":  boolRule(true),
	"block_drops":                          boolRule(true),
	"block_explosion_drop_decay":           boolRule(true),
	"command_blocks_work":                  boolRule(true),
	"command_block_output":                 boolRule(true),
	"drowning_damage":                      boolRule(true),
	"elytra_movement_check":                boolRule(true),
	"ender_pearls_vanish_on_death":         boolRule(true),
	"entity_drops":                         boolRule(true),
	"fall_damage":                          boolRule(true),
	"fire_damage":                          boolRule(true),
	"fire_spread_radius_around_player":     intRule(128, -1, maxRuleInt),
	"forgive_dead_players":                 boolRule(true),
	"freeze_damage":                        boolRule(true),
	"global_sound_events":                  boolRule(true),
	"immediate_respawn":                    boolRule(false),
	"keep_inventory":                       boolRule(false),
	"lava_source_conversion":               boolRule(false),
	"limited_crafting":                     boolRule(false),
	"locator_bar":                          boolRule(true),
	"log_admin_commands":                   boolRule(true),
	"max_block_modifications":              intRule(32768, 1, maxRuleInt),
	"max_command_forks":                    intRule(65536, 0, maxRuleInt),
	"max_command_sequence_length":          intRule(65536, 0, maxRuleInt),
	"max_entity_cramming":                  intRule(24, 0, maxRuleInt),
	"max_minecart_speed":                   intRule(8, 1, 1000),
	"max_snow_accumulation_height":         intRule(1, 0, 8),
	"mob_drops":                            boolRule(true),
	"mob_explosion_drop_decay":             boolRule(true),
	"mob_griefing":                         boolRule(true),
	"natural_health_regeneration":          boolRule(true),
	"player_movement_check":                boolRule(true),
	"players_nether_portal_creative_delay": intRule(0, 0, maxRuleInt),
	"players_nether_portal_default_delay":  intRule(80, 0, maxRuleInt),
	"players_sleeping_percentage":          intRule(100, 0, maxRuleInt),
	"projectiles_can_break_blocks":         boolRule(true),
	"pvp":                                  boolRule(true),
	"raids":                                boolRule(true),
	"random_tick_speed":                    intRule(3, 0, maxRuleInt),
	"reduced_debug_info":                   boolRule(false),
	"respawn_radius":                       intRule(10, 0, maxRuleInt),
	"send_command_feedback":                boolRule(true),
	"show_advancement_messages":            boolRule(true),
	"show_death_messages":                  boolRule(true),
	"spawner_blocks_work":                  boolRule(true),
	"spawn_mobs":                           boolRule(true),
	"spawn_monsters":                       boolRule(true),
	"spawn_patrols":                        boolRule(true),
	"spawn_phantoms":                       boolRule(true),
	"spawn_wandering_traders":              boolRule(true),
	"spawn_wardens":                        boolRule(true),
	"spectators_generate_chunks":           boolRule(true),
	"spread_vines":                         boolRule(true),
	"tnt_explodes":                         boolRule(true),
	"tnt_explosion_drop_decay":             boolRule(false),
	"universal_anger":                      boolRule(false),
	"water_source_conversion":              boolRule(true),
}

const gameruleNamespace = "gamerules"

// GameRuleNames returns every Java 26.3 gamerule identifier, sorted.
func GameRuleNames() []string {
	names := make([]string, 0, len(gameRules))
	for name := range gameRules {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func normalizeRule(name string) string {
	return strings.TrimPrefix(strings.ToLower(name), "minecraft:")
}

// GameRule returns the value of a Java gamerule, or false for unknown rules.
// Rules backed by native level.dat settings read the live setting.
func (w *World) GameRule(name string) (any, bool) {
	if w == nil {
		return nil, false
	}
	name = normalizeRule(name)
	rule, known := gameRules[name]
	if !known {
		return nil, false
	}
	switch name {
	case "advance_time":
		return w.TimeCycle(), true
	case "advance_weather":
		return w.WeatherCycleEnabled(), true
	case "spawn_mobs":
		return w.SpawnMobsEnabled(), true
	case "fall_damage":
		return w.FallDamage(), true
	case "players_sleeping_percentage":
		return int(w.PlayersSleepingPercentage()), true
	case "random_tick_speed":
		return max(w.conf.RandomTickSpeed, 0), true
	case "spawn_monsters", "spawn_wardens", "water_source_conversion", "lava_source_conversion":
		return w.nativeBoolRule(name), true
	}
	w.loadGameRules()
	w.gameRulesMu.Lock()
	defer w.gameRulesMu.Unlock()
	if v, ok := w.gameRules[name]; ok {
		return v, true
	}
	return rule.def, true
}

// CanSpreadFireAround ports ServerLevel.canSpreadFireAround: fire ticks only
// within fire_spread_radius_around_player of a non-spectator player, unless
// the radius is -1.
func (tx *Tx) CanSpreadFireAround(pos cube.Pos) bool {
	radius := tx.World().GameRuleInt("fire_spread_radius_around_player")
	if radius == -1 {
		return true
	}
	target := pos.Vec3()
	for p := range tx.Players() {
		if mode, ok := p.(interface{ GameMode() GameMode }); ok && mode.GameMode() == GameModeSpectator {
			continue
		}
		if p.Position().Sub(target).Len() < float64(radius) {
			return true
		}
	}
	return false
}

// GameRuleBool returns a boolean gamerule, or its Java default.
func (w *World) GameRuleBool(name string) bool {
	v, _ := w.GameRule(name)
	b, _ := v.(bool)
	return b
}

// GameRuleInt returns an integer gamerule, or its Java default.
func (w *World) GameRuleInt(name string) int {
	v, _ := w.GameRule(name)
	n, _ := v.(int)
	return n
}

func (w *World) nativeBoolRule(name string) bool {
	w.set.Lock()
	defer w.set.Unlock()
	switch name {
	case "spawn_monsters":
		return w.set.SpawnMonsters
	case "spawn_wardens":
		return w.set.SpawnWardens
	case "water_source_conversion":
		return w.set.WaterSourceConversion
	default:
		return w.set.LavaSourceConversion
	}
}

// ErrUnknownGameRule is returned for identifiers outside the 26.3 registry.
var ErrUnknownGameRule = errors.New("unknown gamerule")

// SetGameRule validates value against the rule type, applies native-backed
// rules to the live settings and persists the others in world data.
func (w *World) SetGameRule(name string, value any) error {
	if w == nil {
		return fmt.Errorf("world unavailable")
	}
	name = normalizeRule(name)
	rule, known := gameRules[name]
	if !known {
		return ErrUnknownGameRule
	}
	value, err := rule.parse(value)
	if err != nil {
		return fmt.Errorf("invalid value for %s: %w", name, err)
	}
	switch name {
	case "advance_time":
		w.SetTimeCycle(value.(bool))
		return nil
	case "advance_weather":
		if value.(bool) {
			w.StartWeatherCycle()
		} else {
			w.StopWeatherCycle()
		}
		return nil
	case "spawn_mobs":
		w.SetSpawnMobs(value.(bool))
		return nil
	case "fall_damage":
		w.SetFallDamage(value.(bool))
		return nil
	case "spawn_monsters":
		w.SetSpawnMonsters(value.(bool))
		return nil
	case "spawn_wardens":
		w.SetSpawnWardens(value.(bool))
		return nil
	case "water_source_conversion":
		w.SetWaterSourceConversion(value.(bool))
		return nil
	case "lava_source_conversion":
		w.SetLavaSourceConversion(value.(bool))
		return nil
	case "players_sleeping_percentage":
		w.SetPlayersSleepingPercentage(int32(value.(int)))
		return nil
	case "random_tick_speed":
		// Dragonfly disables random ticks with a negative speed.
		w.conf.RandomTickSpeed = value.(int)
		if w.conf.RandomTickSpeed == 0 {
			w.conf.RandomTickSpeed = -1
		}
	}
	w.loadGameRules()
	w.gameRulesMu.Lock()
	w.gameRules[name] = value
	data, err := json.Marshal(w.gameRules)
	w.gameRulesMu.Unlock()
	if err != nil {
		return err
	}
	viewers, _ := w.allViewers()
	for _, viewer := range viewers {
		if v, ok := viewer.(interface{ ViewGameRule(string, any) }); ok {
			v.ViewGameRule(name, value)
		}
	}
	w.releaseViewers(viewers)
	return w.SaveWorldData(gameruleNamespace, data)
}

func (r gameRule) parse(value any) (any, error) {
	if s, ok := value.(string); ok {
		if r.boolean {
			b, err := strconv.ParseBool(s)
			return b, err
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			return nil, err
		}
		value = n
	}
	if r.boolean {
		b, ok := value.(bool)
		if !ok {
			return nil, fmt.Errorf("expected true or false")
		}
		return b, nil
	}
	var n int
	switch v := value.(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	default:
		return nil, fmt.Errorf("expected an integer")
	}
	if n < r.min || n > r.max {
		return nil, fmt.Errorf("integer must be between %d and %d", r.min, r.max)
	}
	return n, nil
}

// loadGameRules reads persisted non-native rules once per world.
func (w *World) loadGameRules() {
	w.gameRulesMu.Lock()
	defer w.gameRulesMu.Unlock()
	if w.gameRules != nil {
		return
	}
	w.gameRules = map[string]any{}
	data, err := w.LoadWorldData(gameruleNamespace)
	if err != nil {
		if !errors.Is(err, leveldb.ErrNotFound) {
			w.conf.Log.Error("load gamerules: " + err.Error())
		}
		return
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		w.conf.Log.Error("decode gamerules: " + err.Error())
		return
	}
	for name, value := range saved {
		rule, known := gameRules[name]
		if !known {
			continue
		}
		if parsed, err := rule.parse(value); err == nil {
			w.gameRules[name] = parsed
		}
	}
}

// WeatherCycleEnabled reports whether the weather cycle is enabled.
func (w *World) WeatherCycleEnabled() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.WeatherCycle
}

// SpawnMobsEnabled reports whether Java's global mob spawning rule is enabled.
func (w *World) SpawnMobsEnabled() bool {
	if w == nil {
		return false
	}
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.SpawnMobs
}

// SetTimeCycle changes whether world time advances.
func (w *World) SetTimeCycle(enabled bool) {
	if enabled {
		w.StartTime()
	} else {
		w.StopTime()
	}
}

// SetPlayersSleepingPercentage changes the sleep-skip threshold.
func (w *World) SetPlayersSleepingPercentage(percentage int32) {
	if w == nil {
		return
	}
	w.set.Lock()
	defer w.set.Unlock()
	w.set.PlayersSleepingPercentage = percentage
}
