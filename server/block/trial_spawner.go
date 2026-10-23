package block

import (
	"maps"
	"math"
	"math/rand/v2"
	"slices"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/loot"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// TrialSpawnerState is the stage of its trial a trial spawner is in, the
// trial_spawner_state block property.
type TrialSpawnerState uint8

const (
	TrialSpawnerInactive TrialSpawnerState = iota
	TrialSpawnerWaitingForPlayers
	TrialSpawnerActive
	TrialSpawnerWaitingForReward
	TrialSpawnerEjectingReward
	TrialSpawnerCooldown
)

// lightLevel returns the light a trial spawner emits in the state.
func (s TrialSpawnerState) lightLevel() uint8 {
	switch s {
	case TrialSpawnerWaitingForPlayers:
		return 4
	case TrialSpawnerActive, TrialSpawnerWaitingForReward, TrialSpawnerEjectingReward:
		return 8
	}
	return 0
}

// TrialSpawner spawns waves of mobs for the players near it and ejects a
// reward for each of them once the mobs are defeated. Its behaviour is ported
// from Java's TrialSpawner and its block actor is saved as BDS saves it.
type TrialSpawner struct {
	solid
	transparent
	State   TrialSpawnerState
	Ominous bool

	// data is the block actor, shared by the copies of the block.
	data *trialSpawnerData
}

// NewTrialSpawner returns a trial spawner with the default configuration,
// which spawns nothing until a spawn egg is used on it.
func NewTrialSpawner() TrialSpawner {
	return TrialSpawner{data: newTrialSpawnerData()}
}

func (s TrialSpawner) withData() TrialSpawner {
	if s.data == nil {
		s.data = newTrialSpawnerData()
	}
	return s
}

// LightEmissionLevel ...
func (s TrialSpawner) LightEmissionLevel() uint8 {
	return s.State.lightLevel()
}

// BreakInfo ...
func (s TrialSpawner) BreakInfo() BreakInfo {
	return newBreakInfo(50, alwaysHarvestable, pickaxeEffective, simpleDrops())
}

// EncodeItem ...
func (s TrialSpawner) EncodeItem() (string, int16) {
	return "minecraft:trial_spawner", 0
}

// EncodeBlock ...
func (s TrialSpawner) EncodeBlock() (string, map[string]any) {
	return "minecraft:trial_spawner", map[string]any{"trial_spawner_state": int32(s.State), "ominous": boolByte(s.Ominous)}
}

// Hash ...
func (s TrialSpawner) Hash() (uint64, uint64) {
	return hashTrialSpawner, uint64(s.State) | uint64(boolByte(s.Ominous))<<3
}

// WithEntity returns the trial spawner set to spawn the entity with the name
// passed, as a spawn egg sets it: its trial starts over with both of its
// configurations spawning only that entity.
func (s TrialSpawner) WithEntity(entity string) world.Block {
	s = s.withData()
	d := s.data.clone()
	d.reset()
	potentials := []trialSpawnPotential{{data: map[string]any{"entity": map[string]any{"id": entity}}, weight: 1}}
	normal, ominous := d.normal.config(), d.ominous.config()
	normal.spawnPotentials, ominous.spawnPotentials = potentials, potentials
	d.normal, d.ominous = trialConfigRef{inline: &normal}, trialConfigRef{inline: &ominous}
	s.data, s.State = d, TrialSpawnerInactive
	return s
}

// EncodeNBT ...
func (s TrialSpawner) EncodeNBT() map[string]any {
	d := s.withData().data
	java := map[string]any{}
	m := map[string]any{
		"id":                    "TrialSpawner",
		"normal_config":         d.normal.encode("normal_config", d.targetCooldownLength, java),
		"ominous_config":        d.ominous.encode("ominous_config", d.targetCooldownLength, java),
		"required_player_range": int32(d.requiredPlayerRange),
		"registered_players":    uniqueIDCompoundsNBT(d.players),
		"current_mobs":          uniqueIDCompoundsNBT(d.mobs),
		"total_mobs_spawned":    int32(d.totalMobsSpawned),
		"next_mob_spawns_at":    d.nextMobSpawnsAt,
		"cooldown_end_at":       d.cooldownEndsAt,
	}
	if spawnData := d.bedrockSpawnData(); spawnData != nil {
		m["spawn_data"] = spawnData
	}
	if d.next != nil {
		java["spawn_data"] = d.next
	}
	if d.ejecting != "" {
		m["selected_loot_table"] = loot.BedrockTablePath(d.ejecting)
	}
	if d.normal.key != "" && d.ominous.key != "" && d.targetCooldownLength != trialDefaultCooldown {
		java["target_cooldown_length"] = int32(d.targetCooldownLength)
	}
	if len(java) != 0 {
		m["adamant:java"] = java
	}
	return m
}

// UpdateNBT returns what BDS updates trial spawners on clients with: the mob
// it spins inside, if it has one.
func (s TrialSpawner) UpdateNBT() map[string]any {
	if spawnData := s.withData().data.bedrockSpawnData(); spawnData != nil {
		return map[string]any{"spawn_data": spawnData}
	}
	return nil
}

// DecodeNBT reads a trial spawner saved by BDS, placed from a Java structure
// template, or saved in the Java layout by earlier Adamant versions.
func (s TrialSpawner) DecodeNBT(m map[string]any) any {
	d := newTrialSpawnerData()
	java, _ := m["adamant:java"].(map[string]any)
	d.normal = decodeTrialConfigRef(m["normal_config"], java["normal_config"])
	d.ominous = decodeTrialConfigRef(m["ominous_config"], java["ominous_config"])
	if r, ok := trialInteger(m["required_player_range"]); ok && r >= 1 && r <= 128 {
		d.requiredPlayerRange = int(r)
	}
	inline, _ := m["normal_config"].(map[string]any)
	for _, v := range []any{java["target_cooldown_length"], inline["target_cooldown_length"], m["target_cooldown_length"]} {
		if n, ok := trialInteger(v); ok && n >= 0 {
			d.targetCooldownLength = int(n)
			break
		}
	}
	d.players = readUniqueIDs(m["registered_players"])
	d.mobs = readUniqueIDs(m["current_mobs"])
	if n, ok := trialInteger(m["total_mobs_spawned"]); ok && n > 0 {
		d.totalMobsSpawned = int(n)
	}
	d.nextMobSpawnsAt, _ = trialInteger(m["next_mob_spawns_at"])
	if n, ok := trialInteger(m["cooldown_end_at"]); ok {
		d.cooldownEndsAt = n
	} else {
		d.cooldownEndsAt, _ = trialInteger(m["cooldown_ends_at"])
	}
	spawnData, _ := m["spawn_data"].(map[string]any)
	if w, ok := trialInteger(spawnData["Weight"]); ok {
		d.nextWeight = int(w)
	}
	if next, ok := java["spawn_data"].(map[string]any); ok {
		d.next = next
	} else if typeID, _ := spawnData["TypeId"].(string); typeID != "" {
		d.next = map[string]any{"entity": map[string]any{"id": typeID}}
	} else if _, ok := spawnData["entity"]; ok {
		d.next = spawnData
	}
	for _, key := range []string{"selected_loot_table", "ejecting_loot_table"} {
		if table, _ := m[key].(string); table != "" {
			d.ejecting = loot.JavaTableKey(table)
			break
		}
	}
	s.data = d
	return s
}

const (
	trialDefaultCooldown    = 36000
	trialDefaultPlayerRange = 14
	trialMobTrackingRange   = 47
	trialItemSpawnerDelay   = 160
)

// trialSpawnerData is the block actor of a trial spawner: the FullConfig and
// TrialSpawnerStateData of Java.
type trialSpawnerData struct {
	normal, ominous      trialConfigRef
	targetCooldownLength int
	requiredPlayerRange  int

	// players and mobs are the actor unique IDs of the players detected and
	// the mobs spawned that are still tracked.
	players, mobs   []int64
	cooldownEndsAt  int64
	nextMobSpawnsAt int64
	// totalMobsSpawned counts the mobs of the current trial.
	totalMobsSpawned int
	// next is the Java SpawnData of the mob to spawn next, nil before one is
	// chosen, and nextWeight the weight of the potential it was chosen from.
	next       map[string]any
	nextWeight int
	// ejecting is the loot table rewards are ejected from.
	ejecting string
	// dispensing caches the items an ominous spawner drops on players.
	dispensing []trialDispensedItem
}

type trialDispensedItem struct {
	stack  item.Stack
	weight int
}

func newTrialSpawnerData() *trialSpawnerData {
	return &trialSpawnerData{targetCooldownLength: trialDefaultCooldown, requiredPlayerRange: trialDefaultPlayerRange}
}

func (d *trialSpawnerData) clone() *trialSpawnerData {
	c := *d
	c.players, c.mobs = slices.Clone(d.players), slices.Clone(d.mobs)
	return &c
}

// reset ports TrialSpawnerStateData.reset.
func (d *trialSpawnerData) reset() {
	d.mobs, d.next = nil, nil
	d.resetStatistics()
}

// resetStatistics ports TrialSpawnerStateData.resetStatistics.
func (d *trialSpawnerData) resetStatistics() {
	d.players = nil
	d.totalMobsSpawned, d.nextMobSpawnsAt, d.cooldownEndsAt = 0, 0, 0
}

// bedrockSpawnData returns the spawn_data compound BDS saves and clients show
// the spinning mob from, or nil if no mob is chosen.
func (d *trialSpawnerData) bedrockSpawnData() map[string]any {
	id := trialSpawnEntityID(d.next)
	if id == "" {
		return nil
	}
	return map[string]any{"TypeId": world.BedrockEntityIdentifier(id), "Weight": int32(max(d.nextWeight, 1))}
}

// trialSpawnEntityID returns the id of the entity of a Java SpawnData.
func trialSpawnEntityID(spawnData map[string]any) string {
	entity, _ := spawnData["entity"].(map[string]any)
	id, _ := entity["id"].(string)
	return id
}

// Tick runs the trial of the spawner, ported from TrialSpawner.tickServer.
func (s TrialSpawner) Tick(currentTick int64, pos cube.Pos, tx *world.Tx) {
	t := trialSpawnerTick{s: s, pos: pos, tx: tx, tick: currentTick, dirty: s.data == nil}
	t.s = s.withData()
	random := vanilla.NewXoroshiroRandom(rand.Int64())
	t.random = &random
	before := t.s
	d := t.s.data
	if t.untrackMobs() {
		d.nextMobSpawnsAt = currentTick + int64(t.s.config().ticksBetweenSpawn)
		t.dirty = true
	}
	if next := t.nextState(); next != t.s.State {
		t.s.State = next
	}
	switch {
	case t.s.State != before.State || t.s.Ominous != before.Ominous || t.updated:
		tx.SetBlock(pos, t.s, nil)
	case t.dirty:
		tx.SetBlockEntity(pos, t.s)
	}
}

// config returns the configuration the spawner uses now.
func (s TrialSpawner) config() trialSpawnerConfig {
	if s.Ominous {
		return s.data.ominous.config()
	}
	return s.data.normal.config()
}

// trialSpawnerTick holds a trial spawner through one tick. updated is set
// where Java marks the spawner updated, which clients are sent, and dirty
// where only its saved data changed.
type trialSpawnerTick struct {
	s              TrialSpawner
	pos            cube.Pos
	tx             *world.Tx
	tick           int64
	random         *vanilla.Random
	dirty, updated bool
}

// nextState ports TrialSpawnerState.tickAndGetNext.
func (t *trialSpawnerTick) nextState() TrialSpawnerState {
	d, config := t.s.data, t.s.config()
	switch state := t.s.State; state {
	case TrialSpawnerInactive:
		if t.hasDisplayEntity() {
			return TrialSpawnerWaitingForPlayers
		}
	case TrialSpawnerWaitingForPlayers:
		if !trialCanSpawnInLevel(t.tx) {
			t.resetStatistics()
			return state
		}
		if !t.hasMobToSpawn() {
			return TrialSpawnerInactive
		}
		t.tryDetectPlayers()
		if len(d.players) != 0 {
			return TrialSpawnerActive
		}
	case TrialSpawnerActive:
		if !trialCanSpawnInLevel(t.tx) {
			t.resetStatistics()
			return TrialSpawnerWaitingForPlayers
		}
		if !t.hasMobToSpawn() {
			return TrialSpawnerInactive
		}
		additional := max(0, len(d.players)-1)
		t.tryDetectPlayers()
		if t.s.Ominous {
			t.spawnOminousItemSpawner()
		}
		if d.totalMobsSpawned >= config.targetTotalMobs(additional) {
			if len(d.mobs) == 0 {
				d.cooldownEndsAt = t.tick + int64(d.targetCooldownLength)
				d.totalMobsSpawned, d.nextMobSpawnsAt = 0, 0
				t.dirty = true
				return TrialSpawnerWaitingForReward
			}
		} else if t.tick >= d.nextMobSpawnsAt && len(d.mobs) < config.targetSimultaneousMobs(additional) {
			if id, ok := t.spawnMob(); ok {
				d.mobs = append(d.mobs, id)
				d.totalMobsSpawned++
				d.nextMobSpawnsAt = t.tick + int64(config.ticksBetweenSpawn)
				t.dirty = true
				if potential, ok := pickTrialSpawnPotential(config.spawnPotentials, t.random); ok {
					d.next, d.nextWeight = potential.data, potential.weight
					t.updated = true
				}
			}
		}
	case TrialSpawnerWaitingForReward:
		if t.tick >= d.cooldownEndsAt-int64(d.targetCooldownLength)+40 {
			t.tx.PlaySound(t.pos.Vec3Centre(), sound.Custom{Name: "block.trial_spawner.open_shutter", Volume: 1, Pitch: 1})
			return TrialSpawnerEjectingReward
		}
	case TrialSpawnerEjectingReward:
		if (t.tick-(d.cooldownEndsAt-int64(d.targetCooldownLength)))%30 != 0 {
			return state
		}
		if len(d.players) == 0 {
			t.tx.PlaySound(t.pos.Vec3Centre(), sound.Custom{Name: "block.trial_spawner.close_shutter", Volume: 1, Pitch: 1})
			d.ejecting = ""
			t.dirty = true
			return TrialSpawnerCooldown
		}
		if d.ejecting == "" {
			d.ejecting, _ = pickTrialLootTable(config.lootTablesToEject, t.random)
		}
		if d.ejecting != "" {
			t.ejectReward(d.ejecting)
		}
		d.players = d.players[1:]
		t.dirty = true
	case TrialSpawnerCooldown:
		t.tryDetectPlayers()
		if len(d.players) != 0 {
			d.totalMobsSpawned, d.nextMobSpawnsAt = 0, 0
			t.dirty = true
			return TrialSpawnerActive
		}
		if t.tick >= d.cooldownEndsAt {
			t.s.Ominous = false
			d.reset()
			t.dirty = true
			return TrialSpawnerWaitingForPlayers
		}
	}
	return t.s.State
}

// resetStatistics resets the statistics of the trial, which a spawner that
// may not spawn does every tick.
func (t *trialSpawnerTick) resetStatistics() {
	d := t.s.data
	if len(d.players) != 0 || d.totalMobsSpawned != 0 || d.nextMobSpawnsAt != 0 || d.cooldownEndsAt != 0 {
		d.resetStatistics()
		t.dirty = true
	}
}

// untrackMobs stops tracking the mobs that are gone, dead or too far away,
// as TrialSpawner.shouldMobBeUntracked selects them. It reports if any were.
func (t *trialSpawnerTick) untrackMobs() bool {
	d := t.s.data
	before := len(d.mobs)
	d.mobs = slices.DeleteFunc(d.mobs, func(id int64) bool {
		e, ok := t.tx.EntityByUniqueID(id)
		if !ok {
			return true
		}
		if dead, ok := e.(interface{ Dead() bool }); ok && dead.Dead() {
			return true
		}
		return cube.PosFromVec3(e.Position()).Sub(t.pos).Vec3().LenSqr() > trialMobTrackingRange*trialMobTrackingRange
	})
	return len(d.mobs) != before
}

// nextSpawnData ports TrialSpawnerStateData.getOrCreateNextSpawnData.
func (t *trialSpawnerTick) nextSpawnData() map[string]any {
	d := t.s.data
	if d.next != nil {
		return d.next
	}
	if potential, ok := pickTrialSpawnPotential(t.s.config().spawnPotentials, t.random); ok {
		d.next, d.nextWeight = potential.data, potential.weight
	} else {
		d.next, d.nextWeight = map[string]any{"entity": map[string]any{}}, 0
	}
	t.updated = true
	return d.next
}

// hasDisplayEntity reports if the spawner has a mob to show spinning inside
// it, which Java's INACTIVE state waits for.
func (t *trialSpawnerTick) hasDisplayEntity() bool {
	id := trialSpawnEntityID(t.nextSpawnData())
	if id == "" {
		return false
	}
	_, ok := t.tx.World().EntityRegistry().Lookup(id)
	return ok
}

// hasMobToSpawn ports TrialSpawnerStateData.hasMobToSpawn.
func (t *trialSpawnerTick) hasMobToSpawn() bool {
	return trialSpawnEntityID(t.nextSpawnData()) != "" || len(t.s.config().spawnPotentials) != 0
}

// tryDetectPlayers ports TrialSpawnerStateData.tryDetectPlayers.
func (t *trialSpawnerTick) tryDetectPlayers() {
	d := t.s.data
	if (blockPosLong(t.pos)+t.tick)%20 != 0 || (t.s.State == TrialSpawnerCooldown && t.s.Ominous) {
		return
	}
	r := float64(d.requiredPlayerRange)
	inSight := trialPlayersNear(t.tx, t.pos, r, false, true)
	becameOminous := false
	if !t.s.Ominous && len(inSight) != 0 {
		if player, ok := findOminousPlayer(inSight); ok {
			t.tx.AddParticle(trialEyePosition(player), particle.TrialSpawnerBecomeOminous{})
			t.applyOminous()
			becameOminous = true
		}
	}
	if t.s.State == TrialSpawnerCooldown && !becameOminous {
		return
	}
	found := inSight
	if len(d.players) != 0 {
		found = trialPlayersNear(t.tx, t.pos, r, false, false)
	}
	added := false
	for _, p := range found {
		if id := p.H().UniqueID(); !containsID(d.players, id) {
			d.players, added = append(d.players, id), true
		}
	}
	if added {
		d.nextMobSpawnsAt = max(t.tick+40, d.nextMobSpawnsAt)
		t.dirty = true
		if !becameOminous {
			t.tx.AddParticle(t.pos.Vec3(), particle.TrialSpawnerDetection{Players: len(d.players), Ominous: t.s.Ominous})
		}
	}
}

// findOminousPlayer returns the first player with Trial Omen, or else the
// last with Bad Omen, which it turns into Trial Omen.
func findOminousPlayer(players []world.Entity) (world.Entity, bool) {
	var badOmen world.Entity
	for _, p := range players {
		holder, ok := p.(trialOmenHolder)
		if !ok {
			continue
		}
		if _, ok := holder.Effect(effect.TrialOmen); ok {
			return p, true
		}
		if _, ok := holder.Effect(effect.BadOmen); ok {
			badOmen = p
		}
	}
	if badOmen == nil {
		return nil, false
	}
	holder := badOmen.(trialOmenHolder)
	omen, _ := holder.Effect(effect.BadOmen)
	holder.RemoveEffect(effect.BadOmen)
	holder.AddEffect(effect.New(effect.TrialOmen, 1, time.Duration(18000*omen.Level())*time.Second/20))
	return badOmen, true
}

type trialOmenHolder interface {
	Effect(effect.Type) (effect.Effect, bool)
	AddEffect(effect.Effect)
	RemoveEffect(effect.Type)
}

// applyOminous turns the spawner ominous, ported from TrialSpawner.applyOminous
// and TrialSpawnerStateData.resetAfterBecomingOminous. Its mobs are removed.
func (t *trialSpawnerTick) applyOminous() {
	d := t.s.data
	t.s.Ominous = true
	t.tx.AddParticle(t.pos.Vec3(), particle.TrialSpawnerBecomeOminous{})
	t.tx.PlaySound(t.pos.Vec3Centre(), sound.Custom{Name: "block.trial_spawner.ominous_activate", Volume: 1, Pitch: 1})
	for _, id := range d.mobs {
		if e, ok := t.tx.EntityByUniqueID(id); ok {
			t.tx.AddParticle(cube.PosFromVec3(e.Position()).Vec3(), particle.TrialSpawnerSpawning{})
			t.tx.RemoveEntity(e)
		}
	}
	ominous := d.ominous.config()
	if len(ominous.spawnPotentials) != 0 {
		d.next = nil
	}
	d.totalMobsSpawned, d.mobs = 0, nil
	d.nextMobSpawnsAt = t.tick + int64(ominous.ticksBetweenSpawn)
	d.cooldownEndsAt = t.tick + trialItemSpawnerDelay
	t.updated = true
}

// spawnMob ports TrialSpawner.spawnMob and returns the actor unique ID of the
// mob spawned.
func (t *trialSpawnerTick) spawnMob() (int64, bool) {
	spawnData := t.nextSpawnData()
	entity, _ := spawnData["entity"].(map[string]any)
	id, _ := entity["id"].(string)
	conf := t.tx.World().EntityRegistry().Config()
	typ, ok := t.tx.World().EntityRegistry().Lookup(id)
	if id == "" || !ok || conf.SpawnMobFromNBT == nil {
		return 0, false
	}
	r := t.random
	spawnPos, ok := trialVec3(entity["Pos"])
	if !ok {
		spawnRange := float64(t.s.config().spawnRange)
		spawnPos = mgl64.Vec3{
			float64(t.pos.X()) + (r.NextDouble()-r.NextDouble())*spawnRange + 0.5,
			float64(t.pos.Y() + int(r.NextInt(3)) - 1),
			float64(t.pos.Z()) + (r.NextDouble()-r.NextDouble())*spawnRange + 0.5,
		}
	}
	if !trialNoBlockCollision(t.tx, typ.BBox(nil).Translate(spawnPos)) || !trialLineOfSight(t.tx, spawnPos, t.pos) {
		return 0, false
	}
	// A mob spawned from nothing but its id is finalised like a natural spawn.
	unconfigured := len(entity) == 1
	data := maps.Clone(entity)
	data["PersistenceRequired"] = byte(1)
	opts := world.EntitySpawnOpts{
		Position:       spawnPos,
		Rotation:       cube.Rotation{float64(r.NextFloat()) * 360, 0},
		SpawnReason:    "trial_spawner",
		SpawnSeed:      r.NextLong(),
		SpawnData:      spawnData,
		SpawnFinalized: !unconfigured,
	}
	handle := conf.SpawnMobFromNBT(id, opts, data)
	if handle == nil {
		return 0, false
	}
	e := t.tx.AddEntity(handle)
	mobPos := cube.PosFromVec3(spawnPos)
	t.tx.AddParticle(t.pos.Vec3(), particle.TrialSpawnerSpawning{Ominous: t.s.Ominous})
	t.tx.AddParticle(mobPos.Vec3(), particle.TrialSpawnerSpawning{Ominous: t.s.Ominous})
	t.tx.PlaySound(mobPos.Vec3(), sound.Custom{Name: "block.trial_spawner.spawn_mob", Volume: 1, Pitch: 1})
	return e.H().UniqueID(), true
}

// ejectReward ports TrialSpawner.ejectReward.
func (t *trialSpawnerTick) ejectReward(table string) {
	random := vanilla.NewLegacyRandom(rand.Int64())
	stacks, err := loot.Generate(table, 0, loot.Context{Random: &random, Tx: t.tx, Origin: t.pos.Vec3Centre()})
	if err != nil || len(stacks) == 0 {
		return
	}
	for _, s := range stacks {
		ejectTrialItem(t.tx, s, t.pos)
	}
	t.tx.AddParticle(t.pos.Vec3(), particle.TrialSpawnerEjecting{})
	t.tx.PlaySound(t.pos.Vec3Centre(), sound.Custom{Name: "block.trial_spawner.eject_item", Volume: 1, Pitch: 1})
}

// spawnOminousItemSpawner ports the ominous item spawners an active ominous
// trial spawner drops above its players and mobs.
func (t *trialSpawnerTick) spawnOminousItemSpawner() {
	d := t.s.data
	stack := t.dispensedItem()
	if stack.Empty() || t.tick < d.cooldownEndsAt {
		return
	}
	pos, ok := t.itemSpawnerPosition()
	if !ok {
		return
	}
	factory := t.tx.World().EntityRegistry().Config().OminousItemSpawner
	if factory == nil {
		return
	}
	r := t.random
	delay := int64(60 + r.NextInt(61))
	t.tx.AddEntity(factory(world.EntitySpawnOpts{Position: pos, SpawnReason: "trial_spawner"}, stack, delay))
	pitch := float64((r.NextFloat()-r.NextFloat())*0.2 + 1)
	t.tx.PlaySound(pos, sound.Custom{Name: "block.trial_spawner.spawn_item_begin", Volume: 1, Pitch: pitch})
	d.cooldownEndsAt = t.tick + trialItemSpawnerDelay
	t.dirty = true
}

// dispensedItem picks one of the items an ominous spawner drops, weighted by
// how many of it its loot table rolled for the spawner's area.
func (t *trialSpawnerTick) dispensedItem() item.Stack {
	d := t.s.data
	if d.dispensing == nil {
		p := t.pos
		low := cube.Pos{
			int(math.Floor(float64(float32(p.X()) / 30))),
			int(math.Floor(float64(float32(p.Y()) / 20))),
			int(math.Floor(float64(float32(p.Z()) / 30))),
		}
		seed := t.tx.World().Seed() + blockPosLong(low)
		stacks, _ := loot.Generate(t.s.config().itemsToDropWhenOminous, seed, loot.Context{Tx: t.tx, Origin: p.Vec3Centre()})
		d.dispensing = make([]trialDispensedItem, 0, len(stacks))
		for _, s := range stacks {
			d.dispensing = append(d.dispensing, trialDispensedItem{stack: s.Grow(1 - s.Count()), weight: s.Count()})
		}
	}
	total := 0
	for _, entry := range d.dispensing {
		total += entry.weight
	}
	if total <= 0 {
		return item.Stack{}
	}
	selection := int(t.random.NextInt(int32(total)))
	for _, entry := range d.dispensing {
		if selection -= entry.weight; selection < 0 {
			return entry.stack
		}
	}
	return item.Stack{}
}

// itemSpawnerPosition returns where to drop an ominous item spawner: above a
// random registered player or tracked mob near the spawner.
func (t *trialSpawnerTick) itemSpawnerPosition() (mgl64.Vec3, bool) {
	d := t.s.data
	r := float64(d.requiredPlayerRange)
	centre := t.pos.Vec3Centre()
	var players, mobs []world.Entity
	for p := range t.tx.Players() {
		if !containsID(d.players, p.H().UniqueID()) || p.Position().Sub(centre).LenSqr() > r*r {
			continue
		}
		if mode, ok := p.(interface{ GameMode() world.GameMode }); ok && (mode.GameMode() == world.GameModeCreative || mode.GameMode() == world.GameModeSpectator) {
			continue
		}
		if dead, ok := p.(interface{ Dead() bool }); ok && dead.Dead() {
			continue
		}
		players = append(players, p)
	}
	if len(players) == 0 {
		return mgl64.Vec3{}, false
	}
	for _, id := range d.mobs {
		if e, ok := t.tx.EntityByUniqueID(id); ok && e.Position().Sub(centre).LenSqr() <= r*r {
			if dead, ok := e.(interface{ Dead() bool }); !ok || !dead.Dead() {
				mobs = append(mobs, e)
			}
		}
	}
	eligible := players
	if t.random.NextBoolean() {
		eligible = mobs
	}
	if len(eligible) == 0 {
		return mgl64.Vec3{}, false
	}
	target := eligible[0]
	if len(eligible) > 1 {
		target = eligible[t.random.NextInt(int32(len(eligible)))]
	}
	from := target.Position()
	to := from.Add(mgl64.Vec3{0, target.H().Type().BBox(target).Height() + 2 + float64(t.random.NextInt(4))})
	hit := cube.PosFromVec3(to)
	trace.TraverseBlocks(from, to, func(p cube.Pos) bool {
		if _, ok := trace.BlockIntercept(p, t.tx, t.tx.Block(p), from, to, trace.CollisionShape); ok {
			hit = p
			return false
		}
		return true
	})
	below := hit.Vec3Centre().Sub(mgl64.Vec3{0, 1})
	belowPos := cube.PosFromVec3(below)
	if len(t.tx.Block(belowPos).Model().BBox(belowPos, t.tx)) != 0 {
		return mgl64.Vec3{}, false
	}
	return below, true
}

// trialCanSpawnInLevel ports TrialSpawner.canSpawnInLevel: spawning needs
// spawner_blocks_work, a difficulty other than peaceful and spawn_mobs.
func trialCanSpawnInLevel(tx *world.Tx) bool {
	w := tx.World()
	return w.GameRuleBool("spawner_blocks_work") && w.Difficulty() != world.DifficultyPeaceful && w.SpawnMobsEnabled()
}

// trialNoBlockCollision reports if no block collides with the box, as Java's
// noCollision checks the spawn box of a mob.
func trialNoBlockCollision(tx *world.Tx, box cube.BBox) bool {
	for x := int(math.Floor(box.Min()[0])); x <= int(math.Floor(box.Max()[0])); x++ {
		for y := int(math.Floor(box.Min()[1])) - 1; y <= int(math.Floor(box.Max()[1])); y++ {
			for z := int(math.Floor(box.Min()[2])); z <= int(math.Floor(box.Max()[2])); z++ {
				pos := cube.Pos{x, y, z}
				b, ok := tx.BlockLoaded(pos)
				if !ok {
					return false
				}
				for _, bb := range b.Model().BBox(pos, tx) {
					if bb.Translate(pos.Vec3()).IntersectsWith(box) {
						return false
					}
				}
			}
		}
	}
	return true
}

// trialVec3 reads a position saved as a list of three numbers.
func trialVec3(v any) (mgl64.Vec3, bool) {
	values, ok := v.([]any)
	if !ok || len(values) != 3 {
		return mgl64.Vec3{}, false
	}
	var out mgl64.Vec3
	for i, value := range values {
		n, ok := trialNumber(value)
		if !ok {
			return mgl64.Vec3{}, false
		}
		out[i] = n
	}
	return out, true
}

// blockPosLong packs a block position as Java's BlockPos.asLong does.
func blockPosLong(p cube.Pos) int64 {
	return int64(p.X()&0x3ffffff)<<38 | int64(p.Z()&0x3ffffff)<<12 | int64(p.Y()&0xfff)
}

// trialSpawnerConfig is a TrialSpawnerConfig of Java: how a trial spawner
// spawns mobs and what it rewards. Loot tables are Java keys.
type trialSpawnerConfig struct {
	spawnRange                     int
	totalMobs                      float64
	simultaneousMobs               float64
	totalMobsAddedPerPlayer        float64
	simultaneousMobsAddedPerPlayer float64
	ticksBetweenSpawn              int
	spawnPotentials                []trialSpawnPotential
	lootTablesToEject              []trialLootChoice
	itemsToDropWhenOminous         string
}

// trialSpawnPotential is a weighted Java SpawnData.
type trialSpawnPotential struct {
	data   map[string]any
	weight int
}

type trialLootChoice struct {
	table  string
	weight int
}

// defaultTrialSpawnerConfig returns TrialSpawnerConfig.DEFAULT.
func defaultTrialSpawnerConfig() trialSpawnerConfig {
	return trialSpawnerConfig{
		spawnRange: 4, totalMobs: 6, simultaneousMobs: 2, totalMobsAddedPerPlayer: 2, simultaneousMobsAddedPerPlayer: 1,
		ticksBetweenSpawn: 40,
		lootTablesToEject: []trialLootChoice{
			{table: "minecraft:spawners/trial_chamber/consumables", weight: 1},
			{table: "minecraft:spawners/trial_chamber/key", weight: 1},
		},
		itemsToDropWhenOminous: "minecraft:spawners/trial_chamber/items_to_drop_when_ominous",
	}
}

func (c trialSpawnerConfig) targetTotalMobs(additionalPlayers int) int {
	return int(math.Floor(c.totalMobs + c.totalMobsAddedPerPlayer*float64(additionalPlayers)))
}

func (c trialSpawnerConfig) targetSimultaneousMobs(additionalPlayers int) int {
	return int(math.Floor(c.simultaneousMobs + c.simultaneousMobsAddedPerPlayer*float64(additionalPlayers)))
}

// decodeTrialConfig reads the keys of a configuration set in m over c. Java
// and BDS name them alike; BDS names loot tables by path.
func decodeTrialConfig(c *trialSpawnerConfig, m map[string]any) {
	if n, ok := trialInteger(m["spawn_range"]); ok && n >= 1 && n <= 128 {
		c.spawnRange = int(n)
	}
	for key, field := range map[string]*float64{
		"total_mobs": &c.totalMobs, "simultaneous_mobs": &c.simultaneousMobs,
		"total_mobs_added_per_player": &c.totalMobsAddedPerPlayer, "simultaneous_mobs_added_per_player": &c.simultaneousMobsAddedPerPlayer,
	} {
		if n, ok := trialNumber(m[key]); ok && n >= 0 {
			*field = n
		}
	}
	if n, ok := trialInteger(m["ticks_between_spawn"]); ok && n >= 0 {
		c.ticksBetweenSpawn = int(n)
	}
	if raw, ok := m["spawn_potentials"].([]any); ok {
		c.spawnPotentials = nil
		for _, value := range raw {
			entry, _ := value.(map[string]any)
			data, _ := entry["data"].(map[string]any)
			if data == nil {
				continue
			}
			weight, _ := trialInteger(entry["weight"])
			c.spawnPotentials = append(c.spawnPotentials, trialSpawnPotential{data: data, weight: max(int(weight), 1)})
		}
	}
	if raw, ok := m["loot_tables_to_eject"].([]any); ok {
		c.lootTablesToEject = nil
		for _, value := range raw {
			entry, _ := value.(map[string]any)
			table, _ := entry["data"].(string)
			if table == "" {
				continue
			}
			weight, _ := trialInteger(entry["weight"])
			c.lootTablesToEject = append(c.lootTablesToEject, trialLootChoice{table: loot.JavaTableKey(table), weight: max(int(weight), 1)})
		}
	}
	if table, _ := m["items_to_drop_when_ominous"].(string); table != "" {
		c.itemsToDropWhenOminous = loot.JavaTableKey(table)
	}
}

// bedrockNBT encodes the configuration inline as BDS saves it, which keeps
// the target cooldown length in each configuration. BDS saves no spawn
// potentials.
func (c trialSpawnerConfig) bedrockNBT(targetCooldownLength int) map[string]any {
	tables := make([]any, len(c.lootTablesToEject))
	for i, choice := range c.lootTablesToEject {
		tables[i] = map[string]any{"data": loot.BedrockTablePath(choice.table), "weight": int32(choice.weight)}
	}
	return map[string]any{
		"spawn_range":                        int32(c.spawnRange),
		"total_mobs":                         float32(c.totalMobs),
		"simultaneous_mobs":                  float32(c.simultaneousMobs),
		"total_mobs_added_per_player":        float32(c.totalMobsAddedPerPlayer),
		"simultaneous_mobs_added_per_player": float32(c.simultaneousMobsAddedPerPlayer),
		"ticks_between_spawn":                int32(c.ticksBetweenSpawn),
		"target_cooldown_length":             int32(targetCooldownLength),
		"loot_tables_to_eject":               tables,
		"items_to_drop_when_ominous":         loot.BedrockTablePath(c.itemsToDropWhenOminous),
	}
}

// trialConfigRef is a configuration of a trial spawner: a key of the trial
// spawner configurations of Java, which BDS saves by the same name, or one
// held inline.
type trialConfigRef struct {
	key    string
	inline *trialSpawnerConfig
}

// config returns the configuration referred to. An unknown key falls back to
// the default configuration, as Java does when it can't decode one.
func (r trialConfigRef) config() trialSpawnerConfig {
	if r.key != "" {
		if c, ok := trialSpawnerConfigs()[r.key]; ok {
			return c
		}
	} else if r.inline != nil {
		return *r.inline
	}
	return defaultTrialSpawnerConfig()
}

// encode returns the value saved for the configuration under name. The spawn
// potentials of an inline configuration, which BDS has no field for, are kept
// in java.
func (r trialConfigRef) encode(name string, targetCooldownLength int, java map[string]any) any {
	if r.key != "" {
		return r.key
	}
	c := r.config()
	if len(c.spawnPotentials) != 0 {
		potentials := make([]any, len(c.spawnPotentials))
		for i, potential := range c.spawnPotentials {
			potentials[i] = map[string]any{"data": potential.data, "weight": int32(potential.weight)}
		}
		java[name] = map[string]any{"spawn_potentials": potentials}
	}
	return c.bedrockNBT(targetCooldownLength)
}

// decodeTrialConfigRef reads a configuration saved as a key or inline, with
// the Java fields kept beside an inline one.
func decodeTrialConfigRef(v, java any) trialConfigRef {
	if key, ok := v.(string); ok && key != "" {
		if _, known := trialSpawnerConfigs()[key]; known || java == nil {
			return trialConfigRef{key: key}
		}
	}
	m, _ := v.(map[string]any)
	extra, _ := java.(map[string]any)
	if m == nil && extra == nil {
		return trialConfigRef{}
	}
	c := defaultTrialSpawnerConfig()
	decodeTrialConfig(&c, m)
	decodeTrialConfig(&c, extra)
	return trialConfigRef{inline: &c}
}

// trialSpawnerConfigs returns the trial spawner configurations of Java by key.
var trialSpawnerConfigs = sync.OnceValue(func() map[string]trialSpawnerConfig {
	names, err := gamedata.ResourceNames("trial_spawner")
	if err != nil {
		panic(err)
	}
	configs := make(map[string]trialSpawnerConfig, len(names))
	for _, name := range names {
		data, err := gamedata.LoadJSON("trial_spawner", name)
		if err != nil {
			panic(err)
		}
		c := defaultTrialSpawnerConfig()
		decodeTrialConfig(&c, trialJSONToNBT("", data).(map[string]any))
		configs["minecraft:"+name] = c
	}
	return configs
})

// trialJSONToNBT converts a decoded JSON configuration to the NBT Java loads
// it as: whole numbers are ints, or bytes for the booleans of entities.
func trialJSONToNBT(key string, v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, value := range v {
			out[k] = trialJSONToNBT(k, value)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, value := range v {
			out[i] = trialJSONToNBT("", value)
		}
		return out
	case float64:
		switch {
		case key == "IsBaby":
			return byte(v)
		case v == math.Trunc(v) && math.Abs(v) <= math.MaxInt32:
			return int32(v)
		}
		return float32(v)
	}
	return v
}

// pickTrialSpawnPotential picks a weighted spawn potential as Java's
// WeightedList.getRandom does.
func pickTrialSpawnPotential(potentials []trialSpawnPotential, r *vanilla.Random) (trialSpawnPotential, bool) {
	total := 0
	for _, p := range potentials {
		total += p.weight
	}
	if total <= 0 {
		return trialSpawnPotential{}, false
	}
	selection := int(r.NextInt(int32(total)))
	for _, p := range potentials {
		if selection -= p.weight; selection < 0 {
			return p, true
		}
	}
	return trialSpawnPotential{}, false
}

// pickTrialLootTable picks a weighted loot table as Java's
// WeightedList.getRandom does.
func pickTrialLootTable(choices []trialLootChoice, r *vanilla.Random) (string, bool) {
	total := 0
	for _, c := range choices {
		total += c.weight
	}
	if total <= 0 {
		return "", false
	}
	selection := int(r.NextInt(int32(total)))
	for _, c := range choices {
		if selection -= c.weight; selection < 0 {
			return c.table, true
		}
	}
	return "", false
}

// trialNumber reads a number of any NBT or JSON type.
func trialNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case uint8:
		return float64(n), true
	case int16:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case float32:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// trialInteger reads a whole number of any NBT or JSON type.
func trialInteger(v any) (int64, bool) {
	n, ok := trialNumber(v)
	return int64(n), ok
}
