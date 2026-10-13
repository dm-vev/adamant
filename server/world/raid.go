package world

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/goleveldb/leveldb"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

const (
	raidVillageRadius   = 96
	raidTimeoutTicks    = 48000
	raidWaveCooldown    = 300
	raidIDBossBarPrefix = "raid:"
)

// RaidStatus is the persisted lifecycle status of a raid.
type RaidStatus uint8

const (
	RaidOngoing RaidStatus = iota
	RaidVictory
	RaidDefeat
	RaidStopped
)

// Raid is the world-owned state of a village raid.
type Raid struct {
	id               int64
	center           cube.Pos
	status           RaidStatus
	active           bool
	started          bool
	ticksActive      int64
	omenLevel        int
	groupsSpawned    int
	groupCount       int
	cooldown         int
	postRaidTicks    int
	celebrationTicks int
	spawnFailures    int
	totalHealth      float64
	members          map[uuid.UUID]struct{}
	heroes           map[uuid.UUID]struct{}
}

// ID returns the persistent raid identifier.
func (r *Raid) ID() int64 { return r.id }

// Center returns the village center currently used by the raid.
func (r *Raid) Center() cube.Pos { return r.center }

// Active reports whether the raid is active and ongoing.
func (r *Raid) Active() bool { return r != nil && r.active && r.status == RaidOngoing }

// Status returns the raid's terminal or ongoing state.
func (r *Raid) Status() RaidStatus { return r.status }

// Contains reports whether e is registered as a member of this raid.
func (r *Raid) Contains(e Entity) bool {
	if r == nil || e == nil {
		return false
	}
	_, ok := r.members[e.H().UUID()]
	return ok
}

// RaidAt returns the nearest active raid whose 96-block village radius contains pos.
func (tx *Tx) RaidAt(pos cube.Pos) *Raid {
	var found *Raid
	best := int64(1<<63 - 1)
	for _, raid := range tx.World().raids {
		if !raid.Active() {
			continue
		}
		dx, dy, dz := int64(pos[0]-raid.center[0]), int64(pos[1]-raid.center[1]), int64(pos[2]-raid.center[2])
		distance := dx*dx + dy*dy + dz*dz
		if distance > raidVillageRadius*raidVillageRadius {
			continue
		}
		if found == nil || distance < best || distance == best && raid.id < found.id {
			found, best = raid, distance
		}
	}
	return found
}

// RaidForRaider returns the active raid to which e belongs.
func (tx *Tx) RaidForRaider(e Entity) *Raid {
	if e == nil {
		return nil
	}
	id, ok := tx.World().raidByRaider[e.H().UUID()]
	if !ok {
		return nil
	}
	return tx.World().raids[id]
}

// RegisterRaider adds a newly published raid member to its raid.
func (tx *Tx) RegisterRaider(raidID int64, e Entity) bool {
	r := tx.World().raids[raidID]
	if r == nil || !r.Active() || e == nil {
		return false
	}
	id := e.H().UUID()
	if _, exists := r.members[id]; exists {
		return false
	}
	if health, ok := e.(interface{ MaxHealth() float64 }); ok {
		r.totalHealth += health.MaxHealth()
	}
	r.members[id] = struct{}{}
	tx.World().raidByRaider[id] = raidID
	return true
}

// RaiderDied removes a dead or removed raider from its registered raid.
func (tx *Tx) RaiderDied(e Entity) {
	if e == nil {
		return
	}
	id := e.H().UUID()
	raidID, ok := tx.World().raidByRaider[id]
	if !ok {
		return
	}
	delete(tx.World().raidByRaider, id)
	raid := tx.World().raids[raidID]
	if raid != nil {
		if health, ok := e.(interface {
			Health() float64
			MaxHealth() float64
		}); ok {
			remaining := health.Health()
			if dead, ok := e.(interface{ Dead() bool }); ok && dead.Dead() {
				remaining = health.MaxHealth()
			}
			raid.totalHealth = max(0.0, raid.totalHealth-remaining)
		}
		delete(raid.members, id)
	}
}
func (tx *Tx) RaiderKilled(raider, killer Entity) {
	if killer == nil || killer.H().Type().EncodeEntity() != "minecraft:player" || raider == nil {
		return
	}
	id, ok := tx.World().raidByRaider[raider.H().UUID()]
	if !ok {
		return
	}
	r := tx.World().raids[id]
	if r == nil {
		return
	}
	if r.heroes == nil {
		r.heroes = make(map[uuid.UUID]struct{})
	}
	r.heroes[killer.H().UUID()] = struct{}{}
}

// TryApplyBadOmen reports whether Bad Omen may transition to Raid Omen at pos.
func (tx *Tx) TryApplyBadOmen(player Entity, pos cube.Pos, amplifier int) bool {
	if !tx.World().GameRuleBool("raids") || player == nil || amplifier < 0 || amplifier >= 5 || tx.World().Difficulty() == DifficultyPeaceful || !tx.IsVillage(pos) {
		return false
	}
	if p, ok := player.(interface{ IsSpectator() bool }); ok && p.IsSpectator() {
		return false
	}
	_, _, ok := tx.NearestPOI(pos, Home, raidVillageRadius, func(p POI) bool { return p.HasOwner })
	return ok
}

// TryApplyRaidOmen starts or extends a raid at the saved Raid Omen position.
func (tx *Tx) TryApplyRaidOmen(player Entity) bool {
	if player == nil || tx.World().Difficulty() == DifficultyPeaceful || !tx.World().GameRuleBool("raids") {
		return false
	}
	if spectator, ok := player.(interface{ IsSpectator() bool }); ok && spectator.IsSpectator() {
		return false
	}
	positioned, ok := player.(interface {
		RaidOmenPosition() cube.Pos
		RaidOmenLevel() int
	})
	if !ok || positioned.RaidOmenLevel() <= 0 {
		return false
	}
	pos := positioned.RaidOmenPosition()
	if !tx.IsVillage(pos) {
		return false
	}
	center, _, ok := tx.NearestPOI(pos, Home, raidVillageRadius, func(p POI) bool { return p.HasOwner })
	if !ok {
		return false
	}
	r := tx.RaidAt(center)
	if r != nil {
		if r.started && r.omenLevel >= 5 {
			return false
		}
		r.omenLevel = min(5, max(1, r.omenLevel+positioned.RaidOmenLevel()))
		return true
	}
	groups := map[Difficulty]int{DifficultyEasy: 3, DifficultyNormal: 5, DifficultyHard: 7}[tx.World().Difficulty()]
	if groups == 0 {
		return false
	}
	id := tx.World().nextRaidID + 1
	tx.World().nextRaidID = id
	tx.World().raids[id] = &Raid{id: id, center: center, status: RaidOngoing, active: true, omenLevel: min(5, max(1, positioned.RaidOmenLevel())), groupCount: groups, cooldown: 300, members: make(map[uuid.UUID]struct{}), heroes: make(map[uuid.UUID]struct{})}
	return true
}

// TickRaids advances raid timers, spawns due waves, updates bars and resolves outcomes.
func (tx *Tx) TickRaids() {
	w := tx.World()
	ids := make([]int64, 0, len(w.raids))
	for id := range w.raids {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		r := w.raids[id]
		if r == nil {
			continue
		}
		if r.status == RaidVictory || r.status == RaidDefeat {
			r.celebrationTicks++
			tx.updateRaidBar(r)
			if r.celebrationTicks >= 600 {
				r.status, r.active = RaidStopped, false
				tx.hideRaidBar(r)
			}
			continue
		}
		if r.status != RaidOngoing {
			continue
		}
		r.active = tx.ChunkLoaded(chunkPosFromBlockPos(r.center))
		if !r.active {
			tx.hideRaidBar(r)
			continue
		}
		if !tx.IsVillage(r.center) {
			sections := tx.VillageSections(r.center, 2)
			if len(sections) > 0 {
				r.center = cube.Pos{sections[0][0] + 8, sections[0][1] + 8, sections[0][2] + 8}
			} else if r.groupsSpawned == 0 {
				r.status, r.active = RaidStopped, false
				tx.hideRaidBar(r)
				continue
			} else {
				r.status, r.active = RaidDefeat, false
				r.celebrationTicks = 1
				tx.updateRaidBar(r)
				continue
			}
		}
		r.ticksActive++
		for entity := range tx.Entities() {
			if _, member := r.members[entity.H().UUID()]; !member {
				continue
			}
			d := entity.Position().Sub(mgl64.Vec3{float64(r.center[0]), float64(r.center[1]), float64(r.center[2])})
			if d.LenSqr() >= 12544 {
				tx.RaiderDied(entity)
			}
		}
		if r.ticksActive >= raidTimeoutTicks {
			r.status, r.active = RaidStopped, false
			tx.hideRaidBar(r)
			continue
		}
		alive := len(r.members)
		if r.cooldown == -1 && r.groupsSpawned > 0 {
			r.cooldown = raidWaveCooldown
		}
		if alive == 0 && r.groupsSpawned < r.groupCount+javaStateBoolInt(r.omenLevel > 1) {
			if r.cooldown <= 0 {
				if r.cooldown == 0 && r.groupsSpawned > 0 {
					r.cooldown = raidWaveCooldown
				} else if !tx.spawnRaidWave(r) {
					r.spawnFailures++
					if r.spawnFailures > 5 {
						r.status, r.active = RaidStopped, false
						tx.hideRaidBar(r)
					} else {
						r.cooldown = 1
					}
				}
			} else {
				r.cooldown--
			}
		}
		if r.started && r.groupsSpawned >= r.groupCount+javaStateBoolInt(r.omenLevel > 1) && alive == 0 {
			if r.postRaidTicks < 40 {
				r.postRaidTicks++
			} else {
				r.status, r.active = RaidVictory, false
				for hero := range r.heroes {
					for player := range tx.Players() {
						if player.H().UUID() == hero {
							if reward, ok := player.(interface{ GrantHeroOfTheVillage(*Tx, int) }); ok {
								reward.GrantHeroOfTheVillage(tx, 48000)
							}
						}
					}
				}
			}
		}
		if r.status != RaidOngoing {
			continue
		}
		tx.updateRaidBar(r)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func (tx *Tx) spawnRaidWave(r *Raid) bool {
	factory := tx.World().EntityRegistry().Config().RaidSpawner
	if factory == nil {
		return false
	}
	spawn, ok := tx.raidSpawnPosition(r)
	if !ok {
		return false
	}
	wave := r.groupsSpawned + 1
	members := raidWaveComposition(tx, wave, r.groupCount, r.omenLevel, tx.World().Difficulty(), tx.LocalDifficulty(spawn).Effective)
	spawned := 0
	for _, member := range members {
		for range member.count {
			pos := mgl64.Vec3{float64(spawn[0]) + .5, float64(spawn[1]), float64(spawn[2]) + .5}
			handle := factory(EntitySpawnOpts{Position: pos, SpawnReason: "event", SpawnSeed: tx.World().r.Int64()}, member.id, r.id, wave, member.leader && spawned == 0)
			if handle == nil {
				continue
			}
			entity := tx.AddEntity(handle)
			if tx.RegisterRaider(r.id, entity) {
				spawned++
			}
		}
	}
	if spawned == 0 {
		return false
	}
	r.started = true
	r.groupsSpawned++
	r.cooldown = -1
	return true
}

type raidWaveMember struct {
	id     string
	count  int
	leader bool
}

func raidWaveComposition(tx *Tx, wave, groups, omen int, difficulty Difficulty, regional float32) []raidWaveMember {
	index := min(wave, groups)
	counts := [][]int{{0, 4, 3, 3, 4, 4, 4, 2}, {0, 0, 2, 0, 1, 4, 2, 5}, {0, 0, 0, 0, 0, 1, 1, 2}, {0, 0, 0, 0, 3, 0, 0, 1}, {0, 0, 0, 1, 0, 1, 0, 2}}
	ids := []string{"minecraft:pillager", "minecraft:vindicator", "minecraft:evoker", "minecraft:witch", "minecraft:ravager"}
	out := make([]raidWaveMember, 0, len(ids))
	for i, id := range ids {
		count := counts[i][min(index, 7)]
		bonus := 0
		switch i {
		case 0, 1:
			limit := 1
			if difficulty == DifficultyHard {
				limit = 2
			}
			if difficulty == DifficultyEasy || difficulty == DifficultyNormal || difficulty == DifficultyHard {
				bonus = tx.World().r.IntN(limit + 1)
			}
		case 3:
			if difficulty != DifficultyEasy && wave > 2 && wave != 4 {
				bonus = tx.World().r.IntN(2)
			}
		case 4:
			if difficulty != DifficultyEasy && wave > groups {
				bonus = tx.World().r.IntN(2)
			}
		}
		_ = regional
		count += bonus
		if count > 0 {
			out = append(out, raidWaveMember{id: id, count: count, leader: i == 0})
		}
	}
	if omen > 1 && wave > groups {
		for i := range out {
			if out[i].id == "minecraft:pillager" {
				out[i].leader = true
			}
		}
	}
	return out
}

func (tx *Tx) raidSpawnPosition(r *Raid) (cube.Pos, bool) {
	seconds := r.cooldown / 20
	distanceScale := max(0.0, 0.22*float64(seconds)-0.24)
	angle := tx.World().r.Float64() * 2 * math.Pi
	for attempt := range 8 {
		a := angle + math.Pi*float64(attempt)/8
		x := r.center[0] + int(math.Floor(math.Cos(a)*32*distanceScale)) + tx.World().r.IntN(3)*int(distanceScale)
		z := r.center[2] + int(math.Floor(math.Sin(a)*32*distanceScale)) + tx.World().r.IntN(3)*int(distanceScale)
		y := tx.HighestBlock(x, z)
		candidate := cube.Pos{x, y, z}
		airPos := candidate.Side(cube.FaceUp)
		headPos := airPos.Side(cube.FaceUp)
		if absInt(y-r.center[1]) <= raidVillageRadius && (!tx.IsVillage(candidate) || seconds <= 7) && tx.Block(candidate).Model().FaceSolid(candidate, cube.FaceUp, tx) && len(tx.Block(airPos).Model().BBox(airPos, tx)) == 0 && len(tx.Block(headPos).Model().BBox(headPos, tx)) == 0 {
			return airPos, true
		}
	}
	return cube.Pos{}, false
}

func (tx *Tx) updateRaidBar(r *Raid) {
	currentHealth := 0.0
	for entity := range tx.Entities() {
		if _, member := r.members[entity.H().UUID()]; !member {
			continue
		}
		if health, ok := entity.(interface{ Health() float64 }); ok {
			currentHealth += max(0.0, health.Health())
		}
	}
	percentage := 1.0
	if r.totalHealth > 0 {
		percentage = currentHealth / r.totalHealth
	}
	text := "Raid"
	if r.status == RaidVictory {
		text = "Raid Victory"
	}
	if r.status == RaidDefeat {
		text = "Raid Defeat"
	}
	bar := bossbar.New(text).WithID(fmt.Sprintf("%s%d", raidIDBossBarPrefix, r.id)).WithColour(bossbar.Red()).WithHealthPercentage(max(0.0, min(1.0, percentage)))
	id := fmt.Sprintf("%s%d", raidIDBossBarPrefix, r.id)
	for player := range tx.Players() {
		distance := player.Position().Sub(mgl64.Vec3{float64(r.center[0]), float64(r.center[1]), float64(r.center[2])}).LenSqr()
		if viewer, ok := player.(interface {
			SendBossBar(bossbar.BossBar)
			RemoveBossBarID(string)
		}); ok {
			if distance <= float64(raidVillageRadius*raidVillageRadius) {
				viewer.SendBossBar(bar)
			} else {
				viewer.RemoveBossBarID(id)
			}
		}
	}
}

func (tx *Tx) hideRaidBar(r *Raid) {
	id := fmt.Sprintf("%s%d", raidIDBossBarPrefix, r.id)
	for player := range tx.Players() {
		if viewer, ok := player.(interface{ RemoveBossBarID(string) }); ok {
			viewer.RemoveBossBarID(id)
		}
	}
}

type raidSave struct {
	ID               int64       `json:"id"`
	Center           cube.Pos    `json:"center"`
	Status           RaidStatus  `json:"status"`
	Active           bool        `json:"active"`
	Started          bool        `json:"started"`
	TicksActive      int64       `json:"ticks_active"`
	OmenLevel        int         `json:"omen_level"`
	GroupsSpawned    int         `json:"groups_spawned"`
	GroupCount       int         `json:"group_count"`
	Cooldown         int         `json:"cooldown"`
	PostRaidTicks    int         `json:"post_raid_ticks"`
	CelebrationTicks int         `json:"celebration_ticks"`
	SpawnFailures    int         `json:"spawn_failures"`
	TotalHealth      float64     `json:"total_health"`
	Members          []uuid.UUID `json:"members"`
	Heroes           []uuid.UUID `json:"heroes"`
}

func (w *World) raidDataNamespace() string {
	return fmt.Sprintf("raids:%s", dimensionStorageKey(w.Dimension()))
}

func (w *World) loadRaids() {
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return
	}
	data, err := provider.LoadWorldData(w.raidDataNamespace())
	if err != nil {
		if err != leveldb.ErrNotFound {
			w.conf.Log.Error("load raid data: " + err.Error())
		}
		return
	}
	var records []raidSave
	if err := json.Unmarshal(data, &records); err != nil {
		w.conf.Log.Error("decode raid data: " + err.Error())
		return
	}
	for _, saved := range records {
		raid := &Raid{
			id: saved.ID, center: saved.Center, status: saved.Status, active: saved.Active,
			started: saved.Started, ticksActive: saved.TicksActive, omenLevel: saved.OmenLevel,
			groupsSpawned: saved.GroupsSpawned, groupCount: saved.GroupCount, cooldown: saved.Cooldown,
			postRaidTicks: saved.PostRaidTicks, celebrationTicks: saved.CelebrationTicks, spawnFailures: saved.SpawnFailures, totalHealth: saved.TotalHealth,
			members: make(map[uuid.UUID]struct{}, len(saved.Members)), heroes: make(map[uuid.UUID]struct{}, len(saved.Heroes)),
		}
		for _, member := range saved.Members {
			raid.members[member] = struct{}{}
			w.raidByRaider[member] = raid.id
		}
		for _, hero := range saved.Heroes {
			raid.heroes[hero] = struct{}{}
		}
		w.raids[raid.id] = raid
		if raid.id > w.nextRaidID {
			w.nextRaidID = raid.id
		}
	}
}

func (w *World) saveRaids() {
	provider, ok := w.conf.Provider.(worldDataProvider)
	if !ok {
		return
	}
	ids := make([]int64, 0, len(w.raids))
	for id := range w.raids {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	records := make([]raidSave, 0, len(ids))
	for _, id := range ids {
		raid := w.raids[id]
		members := make([]uuid.UUID, 0, len(raid.members))
		for member := range raid.members {
			members = append(members, member)
		}
		sort.Slice(members, func(i, j int) bool { return members[i].String() < members[j].String() })
		heroes := make([]uuid.UUID, 0, len(raid.heroes))
		for hero := range raid.heroes {
			heroes = append(heroes, hero)
		}
		sort.Slice(heroes, func(i, j int) bool { return heroes[i].String() < heroes[j].String() })
		records = append(records, raidSave{
			ID: raid.id, Center: raid.center, Status: raid.status, Active: raid.active,
			Started: raid.started, TicksActive: raid.ticksActive, OmenLevel: raid.omenLevel,
			GroupsSpawned: raid.groupsSpawned, GroupCount: raid.groupCount, Cooldown: raid.cooldown,
			PostRaidTicks: raid.postRaidTicks, CelebrationTicks: raid.celebrationTicks, SpawnFailures: raid.spawnFailures, TotalHealth: raid.totalHealth,
			Members: members, Heroes: heroes,
		})
	}
	data, err := json.Marshal(records)
	if err != nil {
		w.conf.Log.Error("encode raid data: " + err.Error())
		return
	}
	if err := provider.SaveWorldData(w.raidDataNamespace(), data); err != nil {
		w.conf.Log.Error("save raid data: " + err.Error())
	}
}
