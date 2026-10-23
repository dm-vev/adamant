package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/google/uuid"
)

// The expectations below are what BDS 1.26.52.3 saves for the vaults and
// trial spawners of a trial chamber it generated, and sends clients for them.

func TestVaultNBTMatchesBDS(t *testing.T) {
	check := func(name string, m map[string]any, key, table string) {
		t.Helper()
		config := m["config"].(map[string]any)
		for k, want := range map[string]any{"activation_range": float32(4), "deactivation_range": float32(4.5), "loot_table": table, "override_loot_table_to_display": ""} {
			if config[k] != want {
				t.Errorf("%s: config.%s = %#v, want %#v", name, k, config[k], want)
			}
		}
		keyItem := config["key_item"].(map[string]any)
		for k, want := range map[string]any{"Name": key, "Count": byte(1), "Damage": int16(0), "WasPickedUp": byte(0)} {
			if keyItem[k] != want {
				t.Errorf("%s: config.key_item.%s = %#v, want %#v", name, k, keyItem[k], want)
			}
		}
		data := m["data"].(map[string]any)
		if items, ok := data["items_to_eject"].([]any); !ok || len(items) != 0 {
			t.Errorf("%s: data.items_to_eject = %#v, want an empty list", name, data["items_to_eject"])
		}
		if players, ok := data["rewarded_players"].([]any); !ok || len(players) != 0 {
			t.Errorf("%s: data.rewarded_players = %#v, want an empty list", name, data["rewarded_players"])
		}
		if _, ok := data["state_updating_resumes_at"].(int64); !ok {
			t.Errorf("%s: data.state_updating_resumes_at = %#v, want a long", name, data["state_updating_resumes_at"])
		}
	}
	check("new vault", NewVault().EncodeNBT(), "minecraft:trial_key", "loot_tables/chests/trial_chambers/reward.json")

	template := map[string]any{"id": "Vault", "config": map[string]any{
		"key_item":   map[string]any{"count": int32(1), "id": "minecraft:ominous_trial_key"},
		"loot_table": "minecraft:chests/trial_chambers/reward_ominous",
	}}
	ominous := Vault{}.DecodeNBT(template).(Vault).EncodeNBT()
	check("Java template", ominous, "minecraft:ominous_trial_key", "loot_tables/chests/trial_chambers/reward_ominous.json")
	check("BDS save", Vault{}.DecodeNBT(ominous).(Vault).EncodeNBT(), "minecraft:ominous_trial_key", "loot_tables/chests/trial_chambers/reward_ominous.json")

	player := uuid.MustParse("00000000-0000-0000-0102-030405060708")
	legacy := Vault{}.DecodeNBT(map[string]any{
		"id":     "Vault",
		"config": template["config"],
		"server_data": map[string]any{
			"rewarded_players":          []any{player.String()},
			"items_to_eject":            []any{map[string]any{"id": "minecraft:diamond", "count": int32(2)}},
			"state_updating_resumes_at": int64(77),
		},
		"shared_data": map[string]any{"display_item": map[string]any{"id": "minecraft:emerald", "count": int32(3)}},
	}).(Vault).EncodeNBT()
	data := legacy["data"].(map[string]any)
	if ids, ok := data["rewarded_players"].([]int64); !ok || len(ids) != 1 || ids[0] != uuidUniqueID(player) {
		t.Errorf("earlier save: rewarded_players = %#v, want the player's unique ID", data["rewarded_players"])
	}
	if items := data["items_to_eject"].([]any); len(items) != 1 || items[0].(map[string]any)["Name"] != "minecraft:diamond" || items[0].(map[string]any)["Count"] != byte(2) {
		t.Errorf("earlier save: items_to_eject = %#v, want 2 diamonds", items)
	}
	if display, _ := data["display_item"].(map[string]any); display["Name"] != "minecraft:emerald" || data["state_updating_resumes_at"] != int64(77) {
		t.Errorf("earlier save: data = %#v", data)
	}
}

func TestVaultUpdatesClientsAsBDS(t *testing.T) {
	v := NewVault()
	u := v.UpdateNBT()
	if players, ok := u["connected_players"].([]any); !ok || len(players) != 0 || u["connected_particle_range"] != float32(4.5) {
		t.Fatalf("idle vault update %#v", u)
	}
	if display := u["display_item"].(map[string]any); display["Name"] != "" || display["Count"] != byte(0) {
		t.Fatalf("idle vault displays %#v, want an empty item", display)
	}
	v.data.connected, v.data.display = []int64{39}, item.NewStack(item.Emerald{}, 8)
	u = v.UpdateNBT()
	if players, ok := u["connected_players"].([]int64); !ok || len(players) != 1 || players[0] != 39 {
		t.Fatalf("connected_players = %#v, want long array [39]", u["connected_players"])
	}
	if display := u["display_item"].(map[string]any); display["Name"] != "minecraft:emerald" || display["Count"] != byte(8) || display["WasPickedUp"] != byte(0) {
		t.Fatalf("display_item = %#v, want 8 emeralds", display)
	}
}

func TestTrialSpawnerNBTMatchesBDS(t *testing.T) {
	template := map[string]any{"id": "TrialSpawner", "normal_config": "minecraft:trial_chamber/melee/spider/normal", "ominous_config": "minecraft:trial_chamber/melee/spider/ominous"}
	s := TrialSpawner{}.DecodeNBT(template).(TrialSpawner)
	m := s.EncodeNBT()
	for k, want := range map[string]any{
		"normal_config": "minecraft:trial_chamber/melee/spider/normal", "ominous_config": "minecraft:trial_chamber/melee/spider/ominous",
		"required_player_range": int32(14), "total_mobs_spawned": int32(0), "next_mob_spawns_at": int64(0), "cooldown_end_at": int64(0),
	} {
		if m[k] != want {
			t.Errorf("%s = %#v, want %#v", k, m[k], want)
		}
	}
	for _, k := range []string{"registered_players", "current_mobs"} {
		if l, ok := m[k].([]any); !ok || len(l) != 0 {
			t.Errorf("%s = %#v, want an empty list", k, m[k])
		}
	}
	if _, ok := m["adamant:java"]; ok {
		t.Errorf("a template spawner keeps Java state %#v", m["adamant:java"])
	}

	// BDS saves the mob chosen and the players and mobs by unique ID.
	s.data.next, s.data.nextWeight = map[string]any{"entity": map[string]any{"id": "minecraft:spider"}}, 1
	s.data.players, s.data.mobs = []int64{-4294967273}, []int64{-4294967247, -4294967246}
	m = s.EncodeNBT()
	spawnData := m["spawn_data"].(map[string]any)
	if spawnData["TypeId"] != "minecraft:spider" || spawnData["Weight"] != int32(1) {
		t.Errorf("spawn_data = %#v", spawnData)
	}
	if mobs := m["current_mobs"].([]any); len(mobs) != 2 || mobs[0].(map[string]any)["uuid"] != int64(-4294967247) {
		t.Errorf("current_mobs = %#v", mobs)
	}
	if u := s.UpdateNBT(); len(u) != 1 || u["spawn_data"].(map[string]any)["TypeId"] != "minecraft:spider" {
		t.Errorf("update = %#v, want only spawn_data", u)
	}

	// What BDS saved reads back the same.
	bds := map[string]any{"id": "TrialSpawner", "normal_config": "minecraft:trial_chamber/melee/spider/normal", "ominous_config": "minecraft:trial_chamber/melee/spider/ominous",
		"required_player_range": int32(14), "total_mobs_spawned": int32(3), "next_mob_spawns_at": int64(3314), "cooldown_end_at": int64(0),
		"registered_players": []any{map[string]any{"uuid": int64(-4294967273)}},
		"current_mobs":       []any{map[string]any{"uuid": int64(-4294967247)}},
		"spawn_data":         map[string]any{"TypeId": "minecraft:spider", "Weight": int32(1)},
	}
	read := TrialSpawner{}.DecodeNBT(bds).(TrialSpawner).data
	if read.totalMobsSpawned != 3 || read.nextMobSpawnsAt != 3314 || len(read.players) != 1 || read.players[0] != -4294967273 || trialSpawnEntityID(read.next) != "minecraft:spider" {
		t.Errorf("BDS save read as %+v", read)
	}

	// A spawner set by a spawn egg saves its configurations inline, with the
	// keys and types BDS saves.
	egg := NewTrialSpawner().WithEntity("minecraft:zombie").(TrialSpawner)
	m = egg.EncodeNBT()
	normal := m["normal_config"].(map[string]any)
	for k, want := range map[string]any{"spawn_range": int32(4), "total_mobs": float32(6), "simultaneous_mobs": float32(2), "total_mobs_added_per_player": float32(2),
		"simultaneous_mobs_added_per_player": float32(1), "target_cooldown_length": int32(36000),
		"items_to_drop_when_ominous": "loot_tables/spawners/trial_chamber/items_to_drop_when_ominous.json"} {
		if normal[k] != want {
			t.Errorf("normal_config.%s = %#v, want %#v", k, normal[k], want)
		}
	}
	if _, ok := normal["ticks_between_spawn"].(int32); !ok {
		t.Errorf("normal_config.ticks_between_spawn = %#v, want an int", normal["ticks_between_spawn"])
	}
	if tables := normal["loot_tables_to_eject"].([]any); len(tables) != 2 || tables[1].(map[string]any)["data"] != "loot_tables/spawners/trial_chamber/key.json" {
		t.Errorf("normal_config.loot_tables_to_eject = %#v", tables)
	}
	reread := TrialSpawner{}.DecodeNBT(m).(TrialSpawner)
	if potentials := reread.data.normal.config().spawnPotentials; len(potentials) != 1 || trialSpawnEntityID(potentials[0].data) != "minecraft:zombie" {
		t.Errorf("spawn egg zombie lost on reload: %+v", potentials)
	}
}

func TestTrialSpawnerConfigsLoad(t *testing.T) {
	configs := trialSpawnerConfigs()
	if len(configs) != 28 {
		t.Fatalf("%d trial spawner configurations, want 28", len(configs))
	}
	breeze := configs["minecraft:trial_chamber/breeze/normal"]
	if breeze.simultaneousMobs != 1 || breeze.totalMobs != 2 || breeze.ticksBetweenSpawn != 20 || trialSpawnEntityID(breeze.spawnPotentials[0].data) != "minecraft:breeze" {
		t.Errorf("breeze configuration %+v", breeze)
	}
	slime := configs["minecraft:trial_chamber/small_melee/slime/normal"].spawnPotentials
	if len(slime) != 2 || slime[0].weight != 3 || slime[0].data["entity"].(map[string]any)["Size"] != int32(1) {
		t.Errorf("slime potentials %+v", slime)
	}
	baby := configs["minecraft:trial_chamber/small_melee/baby_zombie/ominous"]
	entity := baby.spawnPotentials[0].data["entity"].(map[string]any)
	equipment := baby.spawnPotentials[0].data["equipment"].(map[string]any)
	if entity["IsBaby"] != byte(1) || equipment["loot_table"] != "minecraft:equipment/trial_chamber_melee" {
		t.Errorf("baby zombie potential %+v", baby.spawnPotentials[0].data)
	}
	if baby.lootTablesToEject[0] != (trialLootChoice{table: "minecraft:spawners/ominous/trial_chamber/key", weight: 3}) {
		t.Errorf("ominous rewards %+v", baby.lootTablesToEject)
	}
}
