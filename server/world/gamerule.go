package world

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/df-mc/goleveldb/leveldb"
)

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
	w.set.PlayersSleepingPercentage = percentage
	w.set.Unlock()
}

const gameruleNamespace = "gamerules"

// GameRule returns a Java gamerule value. Native rules are read from their live setting.
func (w *World) GameRule(name string) (any, bool) {
	if w == nil {
		return nil, false
	}
	switch strings.ToLower(name) {
	case "dodaylightcycle":
		return w.TimeCycle(), true
	case "doweathercycle":
		return w.WeatherCycleEnabled(), true
	case "domobspawning":
		return w.SpawnMobsEnabled(), true
	case "falldamage":
		return w.FallDamage(), true
	case "playerssleepingpercentage":
		return w.PlayersSleepingPercentage(), true
	}
	var rules map[string]any
	data, err := w.LoadWorldData(gameruleNamespace)
	if err != nil {
		return nil, false
	}
	if json.Unmarshal(data, &rules) != nil {
		return nil, false
	}
	v, ok := rules[strings.ToLower(name)]
	return v, ok
}

// SetGameRule changes known consumer-backed Java rules or stores another rule in Adamant's namespace.
func (w *World) SetGameRule(name string, value any) error {
	if w == nil {
		return fmt.Errorf("world unavailable")
	}
	name = strings.ToLower(name)
	if b, ok := value.(bool); ok {
		switch name {
		case "dodaylightcycle":
			w.SetTimeCycle(b)
			return nil
		case "doweathercycle":
			if b {
				w.StartWeatherCycle()
			} else {
				w.StopWeatherCycle()
			}
			return nil
		case "domobspawning":
			w.SetSpawnMobs(b)
			return nil
		case "falldamage":
			w.SetFallDamage(b)
			return nil
		case "spawn_monsters":
			w.SetSpawnMonsters(b)
			return nil
		case "spawn_wardens":
			w.SetSpawnWardens(b)
			return nil
		case "water_source_conversion":
			w.SetWaterSourceConversion(b)
			return nil
		case "lava_source_conversion":
			w.SetLavaSourceConversion(b)
			return nil
		}
	}
	if name == "playerssleepingpercentage" {
		n, ok := value.(int64)
		if !ok {
			return fmt.Errorf("playersSleepingPercentage requires an integer")
		}
		w.SetPlayersSleepingPercentage(int32(n))
		return nil
	}
	data, err := w.LoadWorldData(gameruleNamespace)
	rules := map[string]any{}
	if err == nil {
		if err = json.Unmarshal(data, &rules); err != nil {
			return err
		}
	} else if err != leveldb.ErrNotFound {
		return err
	}
	rules[name] = value
	data, err = json.Marshal(rules)
	if err != nil {
		return err
	}
	return w.SaveWorldData(gameruleNamespace, data)
}
