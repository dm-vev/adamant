package block

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// VaultState is the Bedrock-visible state of a trial chamber vault.
type VaultState uint8

const (
	VaultInactive VaultState = iota
	VaultActive
	VaultUnlocking
	VaultEjecting
)

// Vault is a single-player reward container. Java block-entity subtrees are
// retained intact, while the native block actor identity remains Bedrock's.
type Vault struct {
	solid
	transparent
	Facing cube.Direction
	State VaultState
	Ominous bool
	config, serverData, sharedData, clientData *map[string]any
}

// NewVault constructs a fresh vault block actor.
func NewVault() Vault {
	return Vault{config: actorMap(), serverData: actorMap(), sharedData: actorMap(), clientData: actorMap()}
}

func (v Vault) EncodeItem() (string, int16) { return "minecraft:vault", 0 }
func (v Vault) EncodeBlock() (string, map[string]any) {
	return "minecraft:vault", map[string]any{"minecraft:cardinal_direction": v.Facing.String(), "ominous": boolByte(v.Ominous), "vault_state": vaultStateName(v.State)}
}
func (v Vault) BreakInfo() BreakInfo { return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, simpleDrops()) }
func (v Vault) EncodeNBT() map[string]any {
	m := map[string]any{"id": "Vault", "server_data": cloneActorCompound(*v.serverData), "shared_data": cloneActorCompound(*v.sharedData), "config": cloneActorCompound(*v.config), "client_data": cloneActorCompound(*v.clientData)}
	return m
}
func (v Vault) DecodeNBT(m map[string]any) any {
	v = ensureVault(v)
	if d, ok := m["server_data"].(map[string]any); ok { v.serverData = &d }
	if d, ok := m["shared_data"].(map[string]any); ok { v.sharedData = &d }
	if d, ok := m["config"].(map[string]any); ok { v.config = &d }
	if d, ok := m["client_data"].(map[string]any); ok { v.clientData = &d }
	return v
}
func ensureVault(v Vault) Vault {
	if v.config == nil { v.config = actorMap() }; if v.serverData == nil { v.serverData = actorMap() }; if v.sharedData == nil { v.sharedData = actorMap() }; if v.clientData == nil { v.clientData = actorMap() }
	return v
}
func vaultStateName(s VaultState) string { switch s { case VaultActive: return "active"; case VaultUnlocking: return "unlocking"; case VaultEjecting: return "ejecting"; default: return "inactive" } }

// Activate inserts the matching trial key and starts the source unlock/eject
// cycle. A player is recorded only after a usable matching key was provided.
func (v Vault) Activate(pos cube.Pos, _ cube.Face, tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil { return false }
	v = ensureVault(v)
	if v.State == VaultInactive { return true }
	key, _ := u.HeldItems()
	if key.Empty() { return true }
	_, normal := key.Item().(item.TrialKey)
	_, ominous := key.Item().(item.OminousTrialKey)
	if (v.Ominous && !ominous) || (!v.Ominous && !normal) { return true }
	id := u.H().UUID().String()
	rewarded := actorStringSet((*v.serverData)["rewarded_players"])
	if rewarded[id] || len(rewarded) >= 128 { return true }
	context := vaultLootContext(pos, tx, u, key.Item())
	table := actorString((*v.config)["loot_table"])
	if table == "" { table = "minecraft:chests/trial_chambers/reward" }
	rewards, err := loot.Generate(table, vaultLootSeed(tx, pos, id), context)
	if err != nil || len(rewards) == 0 { return true }
	items := make([]any, 0, len(rewards))
	for _, reward := range rewards {
		encoded, err := item.StackToStructureNBT(reward)
		if err != nil { return true }
		items = append(items, encoded)
	}
	rewarded[id] = true
	players := make([]any, 0, len(rewarded))
	for player := range rewarded { players = append(players, player) }
	(*v.serverData)["rewarded_players"] = players
	(*v.serverData)["items_to_eject"] = items
	(*v.serverData)["total_ejections_needed"] = int32(len(items))
	(*v.serverData)["state_updating_resumes_at"] = tx.World().CurrentTick() + 14
	v.State = VaultUnlocking
	ctx.CountSub++
	tx.SetBlock(pos, v, nil)
	return true
}

func (v Vault) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	v = ensureVault(v)
	if currentTick%20 == 0 && v.State != VaultUnlocking && v.State != VaultEjecting {
		oldPlayers := actorStringSet((*v.sharedData)["connected_players"])
		players := make([]any, 0, 4)
		for player := range tx.Players() {
			if player.Position().Sub(pos.Vec3Centre()).LenSqr() <= 4.5*4.5 { players = append(players, player.H().UUID().String()) }
		}
		newPlayers := actorStringSet(players)
		state := VaultInactive
		if len(newPlayers) > 0 { state = VaultActive }
		if !sameStringSet(oldPlayers, newPlayers) || state != v.State {
			(*v.sharedData)["connected_players"] = players
			v.State = state
			tx.SetBlock(pos, v, nil)
		}
		if v.State == VaultActive { v.updateDisplayItem(pos, tx) }
	}
	if currentTick < actorLong((*v.serverData)["state_updating_resumes_at"]) { return }
	switch v.State {
	case VaultUnlocking:
		v.State = VaultEjecting
		(*v.serverData)["state_updating_resumes_at"] = currentTick + 20
		tx.SetBlock(pos, v, nil)
	case VaultEjecting:
		items := actorCompoundList((*v.serverData)["items_to_eject"])
		if len(items) == 0 {
			if len(actorStringSet((*v.sharedData)["connected_players"])) > 0 { v.State = VaultActive } else { v.State = VaultInactive }
			(*v.serverData)["state_updating_resumes_at"] = currentTick + 20
			(*v.sharedData)["display_item"] = map[string]any{}
			tx.SetBlock(pos, v, nil)
			return
		}
		stack, err := item.StackFromStructureNBT(items[len(items)-1])
		if err == nil && !stack.Empty() { dropItem(tx, stack, mgl64.Vec3{float64(pos.X())+0.5, float64(pos.Y())+1.2, float64(pos.Z())+0.5}) }
		(*v.serverData)["items_to_eject"] = compoundListToAny(items[:len(items)-1])
		(*v.serverData)["state_updating_resumes_at"] = currentTick + 20
		v.updateDisplayItem(pos, tx)
		tx.SetBlock(pos, v, nil)
	}
}
func (v Vault) updateDisplayItem(pos cube.Pos, tx *world.Tx) {
	table := actorString((*v.config)["override_loot_table_to_display"])
	if table == "" { table = actorString((*v.config)["loot_table"]) }
	if table == "" { table = "minecraft:chests/trial_chambers/reward" }
	seed := vaultLootSeed(tx, pos, "")
	random := vanilla.NewLegacyRandom(seed)
	rewards, err := loot.Generate(table, seed, loot.Context{Random: &random, Origin: pos.Vec3Centre()})
	if err != nil || len(rewards) == 0 { (*v.sharedData)["display_item"] = map[string]any{}; return }
	encoded, err := item.StackToStructureNBT(rewards[0])
	if err == nil { (*v.sharedData)["display_item"] = encoded }
}
func (v Vault) nextEjection() map[string]any { items := actorCompoundList((*v.serverData)["items_to_eject"]); if len(items)==0 { return map[string]any{} }; return items[len(items)-1] }
func vaultLootContext(pos cube.Pos, tx *world.Tx, player item.User, tool world.Item) loot.Context {
	seed := vaultLootSeed(tx, pos, player.H().UUID().String())
	random := vanilla.NewLegacyRandom(seed)
	luck := 0.0
	if carrier, ok := player.(interface{ Luck() float64 }); ok { luck = carrier.Luck() }
	return loot.Context{Random: &random, Tool: tool, Luck: luck, Player: player, Origin: pos.Vec3Centre()}
}
func vaultLootSeed(tx *world.Tx, pos cube.Pos, player string) int64 {
	seed := tx.World().Seed() + int64(pos.X())*341873128712 + int64(pos.Y())*132897987541 + int64(pos.Z())*42317861 + tx.World().CurrentTick()
	for i := range len(player) { seed = seed*31 + int64(player[i]) }
	return seed
}
func actorString(v any) string { s, _ := v.(string); return s }
func actorFloat(v any) float64 {
	switch n := v.(type) { case float32: return float64(n); case float64: return n; case int8: return float64(n); case int16: return float64(n); case int32: return float64(n); case int64: return float64(n); case int: return float64(n) }
	return 0
}
func compoundListToAny(items []map[string]any) []any {
	out := make([]any, len(items))
	for i, item := range items { out[i] = item }
	return out
}
func sameStringSet(a, b map[string]bool) bool {
	if len(a) != len(b) { return false }
	for value := range a { if !b[value] { return false } }
	return true
}

func (v Vault) Hash() (uint64, uint64) { return hashVault, uint64(v.State) | uint64(boolByte(v.Ominous))<<3 | uint64(v.Facing)<<4 }

// TrialSpawnerState follows the native trial-spawner state property.


type TrialSpawnerState uint8
const (
	TrialSpawnerInactive TrialSpawnerState = iota
	TrialSpawnerWaitingForPlayers
	TrialSpawnerActive
	TrialSpawnerWaitingForReward
	TrialSpawnerEjectingReward
	TrialSpawnerCooldown
)

// TrialSpawner owns the persistent trial budget and configuration references
// needed by the Java trial-chamber templates.
type TrialSpawner struct {
	solid
	transparent
	State TrialSpawnerState
	Ominous bool
	data *map[string]any
}

// NewTrialSpawner constructs a fresh native trial-spawner actor.
func NewTrialSpawner() TrialSpawner { return TrialSpawner{data: actorMap()} }
func (s TrialSpawner) EncodeItem() (string, int16) { return "minecraft:trial_spawner", 0 }
func (s TrialSpawner) EncodeBlock() (string, map[string]any) {
	return "minecraft:trial_spawner", map[string]any{"trial_spawner_state": int32(s.State), "ominous": boolByte(s.Ominous)}
}
func (s TrialSpawner) BreakInfo() BreakInfo { return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, simpleDrops()) }
func (s TrialSpawner) EncodeNBT() map[string]any { s = ensureTrialSpawner(s); m := cloneActorCompound(*s.data); m["id"] = "TrialSpawner"; return m }
func (s TrialSpawner) DecodeNBT(m map[string]any) any { data := cloneActorCompound(m); delete(data, "id"); s.data = &data; return s }
func ensureTrialSpawner(s TrialSpawner) TrialSpawner { if s.data == nil { s.data = actorMap() }; return s }
func (s TrialSpawner) Hash() (uint64, uint64) { return hashTrialSpawner, uint64(s.State) | uint64(boolByte(s.Ominous))<<3 }

// Tick implements player activation, configured spawn budgets, tracked-mob
// completion, reward timing and the source 36,000-tick cooldown.
func (s TrialSpawner) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	s = ensureTrialSpawner(s)
	playerRange := int(actorLong((*s.data)["required_player_range"]))
	if playerRange < 1 { playerRange = 14 }
	players := trialPlayers(tx, pos, playerRange)
	registered := actorStringSet((*s.data)["registered_players"])
	becameOminous := false
	if trialPlayerScan(pos, currentTick) && (s.State != TrialSpawnerCooldown || !s.Ominous) {
		if !s.Ominous {
			for _, player := range players {
				if transitionTrialOmen(player) { becameOminous = true; break }
			}
		}
		if becameOminous {
			tracked := actorStringSet((*s.data)["current_mobs"])
			box := cube.Box(float64(pos.X()-47), float64(pos.Y()-47), float64(pos.Z()-47), float64(pos.X()+48), float64(pos.Y()+48), float64(pos.Z()+48))
			for mob := range tx.EntitiesWithin(box) { if tracked[mob.H().UUID().String()] { tx.RemoveEntity(mob) } }
			s.Ominous = true
			(*s.data)["current_mobs"] = []any{}
			(*s.data)["total_mobs_spawned"] = int32(0)
			if potentials, _ := activeTrialConfig(s)["spawn_potentials"].([]any); len(potentials) != 0 { delete(*s.data, "spawn_data") }
			(*s.data)["next_mob_spawns_at"] = currentTick + int64(trialSpawnInterval(s))
			(*s.data)["cooldown_ends_at"] = currentTick + actorLong(activeTrialConfig(s)["ticks_between_item_spawners"])
			s.State = TrialSpawnerInactive
			tx.SetBlock(pos, s, nil)
		}
		if s.State != TrialSpawnerCooldown || becameOminous {
			before := len(registered)
			for _, player := range players { registered[player.H().UUID().String()] = true }
			if len(registered) != before {
				next := actorLong((*s.data)["next_mob_spawns_at"])
				if next < currentTick+40 { next = currentTick+40 }
				(*s.data)["next_mob_spawns_at"] = next
				(*s.data)["registered_players"] = stringSetToAny(registered)
				tx.SetBlock(pos, s, nil)
			}
		}
	}
	priorMobs := actorStringSet((*s.data)["current_mobs"])
	currentMobs := trackedTrialMobs(tx, pos, priorMobs)
	if !sameStringSet(priorMobs, currentMobs) {
		(*s.data)["current_mobs"] = stringSetToAny(currentMobs)
		(*s.data)["next_mob_spawns_at"] = currentTick + int64(trialSpawnInterval(s))
		tx.SetBlock(pos, s, nil)
	}
	config := activeTrialConfig(s)
	targetTotal := trialMobBudget(s, len(registered))
	targetCooldown := actorLong((*s.data)["target_cooldown_length"])
	if targetCooldown <= 0 { targetCooldown = 36000 }
	switch s.State {
	case TrialSpawnerInactive:
		if trialSpawnerEntityID(s, tx, pos) != "" {
			s.State = TrialSpawnerWaitingForPlayers
			tx.SetBlock(pos, s, nil)
		}
	case TrialSpawnerWaitingForPlayers:
		if trialSpawnerEntityID(s, tx, pos) == "" {
			s.State = TrialSpawnerInactive
			tx.SetBlock(pos, s, nil)
		} else if len(registered) > 0 {
			s.State = TrialSpawnerActive
			tx.SetBlock(pos, s, nil)
		}
	case TrialSpawnerActive:
		if trialSpawnerEntityID(s, tx, pos) == "" {
			s.State = TrialSpawnerInactive
			tx.SetBlock(pos, s, nil)
			return
		}
		if s.Ominous { s.spawnOminousItemSpawner(currentTick, pos, tx, registered) }
		totalSpawned := int(actorLong((*s.data)["total_mobs_spawned"]))
		if totalSpawned >= targetTotal && len(currentMobs) == 0 {
			(*s.data)["cooldown_ends_at"] = currentTick + targetCooldown
			(*s.data)["total_mobs_spawned"] = int32(0)
			(*s.data)["next_mob_spawns_at"] = int64(0)
			s.State = TrialSpawnerWaitingForReward
			tx.SetBlock(pos, s, nil)
			return
		}
		if totalSpawned < targetTotal && len(currentMobs) < trialSimultaneousBudget(s, len(registered)) && currentTick >= actorLong((*s.data)["next_mob_spawns_at"]) && trialCanSpawnInLevel(tx) {
			spawned, err := s.spawnConfiguredMob(tx, pos, currentTick)
			if err != nil { slog.Error("trial spawner failed to spawn configured entity", "pos", pos, "error", err); return }
			if spawned != nil {
				currentMobs[spawned.H().UUID().String()] = true
				(*s.data)["current_mobs"] = stringSetToAny(currentMobs)
				(*s.data)["total_mobs_spawned"] = int32(totalSpawned + 1)
				(*s.data)["next_mob_spawns_at"] = currentTick + int64(trialSpawnInterval(s))
				if potential := chooseTrialSpawnData(s, tx, pos); potential != nil { (*s.data)["spawn_data"] = potential }
				tx.SetBlock(pos, s, nil)
			}
		}
	case TrialSpawnerWaitingForReward:
		startedAt := actorLong((*s.data)["cooldown_ends_at"]) - targetCooldown
		if currentTick >= startedAt+40 { s.State = TrialSpawnerEjectingReward; tx.SetBlock(pos, s, nil) }
	case TrialSpawnerEjectingReward:
		startedAt := actorLong((*s.data)["cooldown_ends_at"]) - targetCooldown
		if (currentTick-startedAt)%30 != 0 { return }
		if len(registered) == 0 {
			delete(*s.data, "ejecting_loot_table")
			s.State = TrialSpawnerCooldown
			tx.SetBlock(pos, s, nil)
			return
		}
		table := actorString((*s.data)["ejecting_loot_table"])
		if table == "" {
			table = chooseTrialRewardTable(config, trialLootSeed(tx, pos, currentTick))
			if table == "" { slog.Error("trial spawner has no reward loot table", "pos", pos); return }
			(*s.data)["ejecting_loot_table"] = table
		}
		if err := dispenseTrialLoot(table, tx, pos, trialLootSeed(tx, pos, currentTick)); err != nil {
			slog.Error("trial spawner failed to generate reward", "table", table, "error", err)
			return
		}
		for player := range registered { delete(registered, player); break }
		(*s.data)["registered_players"] = stringSetToAny(registered)
		if len(registered) == 0 { delete(*s.data, "ejecting_loot_table") }
		tx.SetBlock(pos, s, nil)
	case TrialSpawnerCooldown:
		if len(registered) > 0 {
			(*s.data)["total_mobs_spawned"] = int32(0)
			(*s.data)["next_mob_spawns_at"] = int64(0)
			s.State = TrialSpawnerActive
			tx.SetBlock(pos, s, nil)
		} else if currentTick >= actorLong((*s.data)["cooldown_ends_at"]) {
			resetTrialSpawnerData(*s.data)
			s.State, s.Ominous = TrialSpawnerWaitingForPlayers, false
			tx.SetBlock(pos, s, nil)
		}
	}
}

func (s TrialSpawner) spawnConfiguredMob(tx *world.Tx, pos cube.Pos, tick int64) (world.Entity, error) {
	spawnData, _ := (*s.data)["spawn_data"].(map[string]any)
	if spawnData == nil { return nil, fmt.Errorf("trial spawner has no selected spawn data") }
	entityData, _ := spawnData["entity"].(map[string]any)
	if entityData == nil { entityData = spawnData }
	id := actorString(entityData["id"])
	if id == "" { return nil, fmt.Errorf("trial spawner spawn data has no entity id") }
	if _, ok := tx.World().EntityRegistry().Lookup(id); !ok { return nil, fmt.Errorf("trial spawner entity %q is not registered", id) }
	spawnMob := tx.World().EntityRegistry().Config().SpawnMobFromNBT
	if spawnMob == nil { return nil, fmt.Errorf("entity registry has no SpawnMobFromNBT factory") }
	seed := trialLootSeed(tx, pos, tick)
	random := vanilla.NewLegacyRandom(seed)
	spawnRange := int(actorLong(activeTrialConfig(s)["spawn_range"]))
	if spawnRange < 1 { spawnRange = 4 }
	var spawnPos mgl64.Vec3
	if provided, ok := trialVec3(entityData["Pos"]); ok {
		spawnPos = provided
	} else {
		spawnPos = mgl64.Vec3{
			float64(pos.X()) + (random.NextDouble()-random.NextDouble())*float64(spawnRange) + 0.5,
			float64(pos.Y() + int(random.NextInt(3))-1),
			float64(pos.Z()) + (random.NextDouble()-random.NextDouble())*float64(spawnRange) + 0.5,
		}
	}
	rotation := cube.Rotation{float64(random.NextFloat()) * 360, 0}
	if raw, ok := entityData["Rotation"].([]any); ok && len(raw) > 0 { rotation[0] = actorFloat(raw[0]) }
	p := cube.PosFromVec3(spawnPos)
	if !trialSpawnSpace(tx, p) { return nil, nil }
	opts := world.EntitySpawnOpts{Position: spawnPos, Rotation: rotation, SpawnReason: "trial_spawner", SpawnSeed: seed, SpawnData: spawnData}
	handle := spawnMob(id, opts, entityData)
	if handle == nil { return nil, fmt.Errorf("entity factory rejected trial spawn data for %q", id) }
	return tx.AddEntity(handle), nil
}
func activeTrialConfig(s TrialSpawner) map[string]any {
	key := "normal_config"
	if s.Ominous { key = "ominous_config" }
	config, _ := (*s.data)[key].(map[string]any)
	return config
}
func trialSpawnerEntityID(s TrialSpawner, tx *world.Tx, pos cube.Pos) string {
	spawnData, _ := (*s.data)["spawn_data"].(map[string]any)
	if spawnData == nil {
		spawnData = chooseTrialSpawnData(s, tx, pos)
		if spawnData == nil { return "" }
		(*s.data)["spawn_data"] = spawnData
		tx.SetBlock(pos, s, nil)
	}
	return findSpawnEntityID(spawnData)
}
func findSpawnEntityID(value any) string {
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"entity", "entity_to_spawn", "entityToSpawn"} {
			if nested, ok := v[key]; ok { if id := findSpawnEntityID(nested); id != "" { return id } }
		}
		if id, ok := v["id"].(string); ok && len(id) > 0 { return id }
		for _, nested := range v { if id := findSpawnEntityID(nested); id != "" { return id } }
	case []any:
		for _, nested := range v { if id := findSpawnEntityID(nested); id != "" { return id } }
	}
	return ""
}
func trialMobBudget(s TrialSpawner, players int) int {
	config := activeTrialConfig(s)
	total := actorFloat(config["total_mobs"])
	if _, ok := config["total_mobs"]; !ok { total = 6 }
	added := actorFloat(config["total_mobs_added_per_player"])
	if _, ok := config["total_mobs_added_per_player"]; !ok { added = 2 }
	return int(total + added*float64(max(0, players-1)))
}
func trialSimultaneousBudget(s TrialSpawner, players int) int {
	config := activeTrialConfig(s)
	total := actorFloat(config["simultaneous_mobs"])
	if _, ok := config["simultaneous_mobs"]; !ok { total = 2 }
	added := actorFloat(config["simultaneous_mobs_added_per_player"])
	if _, ok := config["simultaneous_mobs_added_per_player"]; !ok { added = 1 }
	return int(total + added*float64(max(0, players-1)))
}

// trialCanSpawnInLevel ports TrialSpawner.canSpawnInLevel: spawning needs
// spawner_blocks_work, a non-peaceful difficulty and spawn_mobs.
func trialCanSpawnInLevel(tx *world.Tx) bool {
	w := tx.World()
	return w.GameRuleBool("spawner_blocks_work") && w.Difficulty() != world.DifficultyPeaceful && w.SpawnMobsEnabled()
}

func trialSpawnInterval(s TrialSpawner) int {
	interval := actorLong(activeTrialConfig(s)["ticks_between_spawn"])
	if interval == 0 { return 40 }
	return int(interval)
}
func trialPlayers(tx *world.Tx, pos cube.Pos, r int) []world.Entity {
	var players []world.Entity
	r2 := float64(r*r)
	for p := range tx.Players() {
		if p.Position().Sub(pos.Vec3Centre()).LenSqr() > r2 { continue }
		if mode, ok := p.(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeSpectator { continue }
		if mode, ok := p.(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeCreative { continue }
		players = append(players, p)
	}
	return players
}
func trialSpawnSpace(tx *world.Tx, p cube.Pos) bool {
	for _, pos := range []cube.Pos{p, p.Side(cube.FaceUp)} {
		if len(tx.Block(pos).Model().BBox(pos, tx)) != 0 { return false }
	}
	return true
}
func actorMap() *map[string]any { m := map[string]any{}; return &m }
func stringsContains(v any, s string) bool {
	text, ok := v.(string)
	if !ok || len(s) > len(text) { return false }
	for i := 0; i+len(s) <= len(text); i++ { if text[i:i+len(s)] == s { return true } }
	return false
}
func actorCompoundList(v any) []map[string]any {
	raw, ok := v.([]any)
	if !ok { return nil }
	out := make([]map[string]any, 0, len(raw))
	for _, value := range raw { if compound, ok := value.(map[string]any); ok { out = append(out, compound) } }
	return out
}
func actorStringSet(v any) map[string]bool {
	out := map[string]bool{}
	raw, ok := v.([]any)
	if !ok { return out }
	for _, value := range raw {
		switch id := value.(type) {
		case string:
			out[id] = true
		case []int32:
			if len(id) == 4 {
				var bytes [16]byte
				for i, part := range id { binary.BigEndian.PutUint32(bytes[i*4:], uint32(part)) }
				out[uuid.UUID(bytes).String()] = true
			}
		}
	}
	return out
}
func actorLong(v any) int64 {
	switch n := v.(type) { case int8: return int64(n); case int16: return int64(n); case int32: return int64(n); case int64: return n; case int: return int64(n); case uint32: return int64(n) }
	return 0
}
func cloneActorCompound(v map[string]any) map[string]any {
	out := make(map[string]any, len(v))
	for key, value := range v { out[key] = value }
	return out
}
func transitionTrialOmen(player world.Entity) bool {
	target, ok := player.(interface {
		Effects() []effect.Effect
		AddEffect(effect.Effect)
		RemoveEffect(effect.Type)
	})
	if !ok { return false }
	for _, active := range target.Effects() {
		switch active.Type() {
		case effect.TrialOmen:
			return true
		case effect.BadOmen:
			target.RemoveEffect(effect.BadOmen)
			duration := time.Duration(18000*active.Level()) * time.Second / 20
			target.AddEffect(effect.New(effect.TrialOmen, 1, duration))
			return true
		}
	}
	return false
}
func stringSetToAny(values map[string]bool) []any {
	out := make([]any, 0, len(values))
	for value := range values {
		id, err := uuid.Parse(value)
		if err != nil { continue }
		encoded := make([]int32, 4)
		for i := range encoded { encoded[i] = int32(binary.BigEndian.Uint32(id[i*4:])) }
		out = append(out, encoded)
	}
	return out
}
func trackedTrialMobs(tx *world.Tx, pos cube.Pos, tracked map[string]bool) map[string]bool {
	live := make(map[string]bool, len(tracked))
	if len(tracked) == 0 { return live }
	box := cube.Box(float64(pos.X()-48), float64(pos.Y()-48), float64(pos.Z()-48), float64(pos.X()+49), float64(pos.Y()+49), float64(pos.Z()+49))
	for entity := range tx.EntitiesWithin(box) {
		id := entity.H().UUID().String()
		if tracked[id] && cube.PosFromVec3(entity.Position()).Sub(pos).Vec3().LenSqr() <= 47*47 { live[id] = true }
	}
	return live
}
func chooseTrialSpawnData(s TrialSpawner, tx *world.Tx, pos cube.Pos) map[string]any {
	potentials, _ := activeTrialConfig(s)["spawn_potentials"].([]any)
	type choice struct { data map[string]any; weight int }
	choices := make([]choice, 0, len(potentials))
	total := 0
	for _, raw := range potentials {
		entry, ok := raw.(map[string]any); if !ok { continue }
		data, _ := entry["data"].(map[string]any); if data == nil { data = entry }
		weight := int(actorLong(entry["weight"])); if weight < 1 { weight = 1 }
		choices = append(choices, choice{data: data, weight: weight}); total += weight
	}
	if total == 0 { return nil }
	random := vanilla.NewLegacyRandom(trialLootSeed(tx, pos, tx.World().CurrentTick()))
	selection := int(random.NextInt(int32(total)))
	for _, option := range choices {
		if selection < option.weight { return cloneActorCompound(option.data) }
		selection -= option.weight
	}
	return nil
}
func chooseTrialRewardTable(config map[string]any, seed int64) string {
	raw, _ := config["loot_tables_to_eject"].([]any)
	type choice struct { name string; weight int }
	choices := make([]choice, 0, len(raw))
	total := 0
	for _, value := range raw {
		entry, ok := value.(map[string]any); if !ok { continue }
		name := actorString(entry["data"])
		if name == "" { name = findLootTableID(entry["data"]) }
		if name == "" { continue }
		weight := int(actorLong(entry["weight"])); if weight < 1 { weight = 1 }
		choices = append(choices, choice{name: name, weight: weight}); total += weight
	}
	if total == 0 { return "" }
	random := vanilla.NewLegacyRandom(seed)
	selection := int(random.NextInt(int32(total)))
	for _, option := range choices {
		if selection < option.weight { return option.name }
		selection -= option.weight
	}
	return ""
}
func findLootTableID(value any) string {
	switch v := value.(type) {
	case string:
		if stringsContains(v, "spawners/") || stringsContains(v, "trial_chamber") { return v }
	case map[string]any:
		for _, nested := range v { if name := findLootTableID(nested); name != "" { return name } }
	case []any:
		for _, nested := range v { if name := findLootTableID(nested); name != "" { return name } }
	}
	return ""
}
func trialPlayerScan(pos cube.Pos, tick int64) bool {
	x, y, z := int64(pos.X())&0x3ffffff, int64(pos.Y())&0xfff, int64(pos.Z())&0x3ffffff
	packed := x<<38 | z<<12 | y
	return (packed+tick)%20 == 0
}
func resetTrialSpawnerData(data map[string]any) {
	for _, key := range []string{"registered_players", "current_mobs", "cooldown_ends_at", "next_mob_spawns_at", "total_mobs_spawned", "spawn_data", "ejecting_loot_table"} {
		delete(data, key)
	}
}
func trialVec3(raw any) (mgl64.Vec3, bool) {
	values, ok := raw.([]any)
	if !ok || len(values) != 3 { return mgl64.Vec3{}, false }
	return mgl64.Vec3{actorFloat(values[0]), actorFloat(values[1]), actorFloat(values[2])}, true
}
func dispenseTrialLoot(table string, tx *world.Tx, pos cube.Pos, seed int64) error {
	random := vanilla.NewLegacyRandom(seed)
	stacks, err := loot.Generate(table, seed, loot.Context{Random: &random})
	if err != nil { return err }
	for _, stack := range stacks {
		if !stack.Empty() { dropItem(tx, stack, mgl64.Vec3{float64(pos.X()) + 0.5, float64(pos.Y()) + 1.2, float64(pos.Z()) + 0.5}) }
	}
	return nil
}
func (s TrialSpawner) spawnOminousItemSpawner(tick int64, pos cube.Pos, tx *world.Tx, registered map[string]bool) {
	if tick < actorLong((*s.data)["cooldown_ends_at"]) { return }
	table := actorString(activeTrialConfig(s)["items_to_drop_when_ominous"])
	if table == "" { table = "minecraft:spawners/trial_chamber/items_to_drop_when_ominous" }
	seed := trialLootSeed(tx, pos, 0)
	random := vanilla.NewLegacyRandom(seed)
	stacks, err := loot.Generate(table, seed, loot.Context{Random: &random})
	if err != nil || len(stacks) == 0 { return }
	total := 0
	for _, stack := range stacks { total += stack.Count() }
	if total == 0 { return }
	selection := int(random.NextInt(int32(total)))
	var selected item.Stack
	for _, stack := range stacks {
		if selection < stack.Count() { selected = stack.Grow(1-stack.Count()); break }
		selection -= stack.Count()
	}
	if selected.Empty() { return }
	rangeValue := int(actorLong((*s.data)["required_player_range"]))
	if rangeValue < 1 { rangeValue = 14 }
	players := trialPlayers(tx, pos, rangeValue)
	nearbyPlayers := make([]world.Entity, 0, len(players))
	for _, player := range players { if registered[player.H().UUID().String()] { nearbyPlayers = append(nearbyPlayers, player) } }
	nearbyMobs := make([]world.Entity, 0)
	tracked := actorStringSet((*s.data)["current_mobs"])
	box := cube.Box(float64(pos.X()-rangeValue), float64(pos.Y()-rangeValue), float64(pos.Z()-rangeValue), float64(pos.X()+rangeValue+1), float64(pos.Y()+rangeValue+1), float64(pos.Z()+rangeValue+1))
	for mob := range tx.EntitiesWithin(box) {
		if tracked[mob.H().UUID().String()] && cube.PosFromVec3(mob.Position()).Sub(pos).Vec3().LenSqr() <= float64(rangeValue*rangeValue) {
			nearbyMobs = append(nearbyMobs, mob)
		}
	}
	targets := nearbyPlayers
	if random.NextInt(2) == 0 { targets = nearbyMobs }
	if len(targets) == 0 { return }
	target := targets[int(random.NextInt(int32(len(targets))))]
	height := target.H().Type().BBox(target).Height()
	spawnPos := target.Position().Add(mgl64.Vec3{0, height + 2 + float64(random.NextInt(4)), 0})
	if !trialSpawnSpace(tx, cube.PosFromVec3(spawnPos)) { return }
	factory := tx.World().EntityRegistry().Config().OminousItemSpawner
	if factory == nil { slog.Error("entity registry has no OminousItemSpawner factory"); return }
	delay := int64(60 + random.NextInt(61))
	opts := world.EntitySpawnOpts{Position: spawnPos, SpawnReason: "trial_spawner", SpawnSeed: seed}
	handle := factory(opts, selected, delay)
	if handle == nil { slog.Error("entity registry rejected ominous item spawner"); return }
	tx.AddEntity(handle)
	(*s.data)["cooldown_ends_at"] = tick + 160
	tx.SetBlock(pos, s, nil)
}
func trialLootSeed(tx *world.Tx, pos cube.Pos, tick int64) int64 {
	return tx.World().Seed() + int64(pos.X())*341873128712 + int64(pos.Y())*132897987541 + int64(pos.Z())*42317861 + tick
}
