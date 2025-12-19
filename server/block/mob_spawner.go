package block

import (
	"log/slog"
	"math/rand/v2"
	"sync/atomic"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// MobSpawner is a block entity that periodically spawns mobs.
type MobSpawner struct {
	solid
	transparent

	entity string

	delay               *atomic.Int32
	minDelay            int32
	maxDelay            int32
	spawnCount          int32
	spawnRange          int32
	requiredPlayerRange int32
	maxNearbyEntities   int32
}

// NewMobSpawner creates a new initialised MobSpawner.
func NewMobSpawner() MobSpawner {
	m := MobSpawner{
		entity:              "minecraft:zombie",
		minDelay:            200,
		maxDelay:            800,
		spawnCount:          1,
		spawnRange:          4,
		requiredPlayerRange: 16,
		maxNearbyEntities:   6,
		delay:               &atomic.Int32{},
	}
	m.resetDelay()
	return m
}

// WithEntity returns the spawner updated to spawn the entity name passed.
func (m MobSpawner) WithEntity(entity string) world.Block {
	if entity == "" {
		return m
	}
	m.entity = entity
	return m
}

// Tick advances the spawner's internal state and spawns entities when active.
func (m MobSpawner) Tick(_ int64, pos cube.Pos, tx *world.Tx) {
	if m.delay == nil {
		// Should never happen for properly initialised block entities.
		return
	}
	if !m.playerWithinRange(tx, pos, int(m.requiredPlayerRange)) {
		return
	}

	if m.delay.Add(-1) > 0 {
		return
	}

	spawned := 0
	for i := int32(0); i < m.spawnCount; i++ {
		if m.spawnOnce(pos, tx) {
			spawned++
		}
	}
	if spawned == 0 {
		m.resetDelay()
		return
	}
	m.resetDelay()
}

func (m MobSpawner) spawnOnce(pos cube.Pos, tx *world.Tx) bool {
	if m.entity == "" {
		return false
	}
	if m.tooManyNearby(tx, pos) {
		return false
	}
	t, ok := tx.World().EntityRegistry().Lookup(m.entity)
	if !ok {
		slog.Default().Info("mob_spawner: unknown entity", "entity", m.entity)
		return false
	}

	spawnPos := m.randomSpawnPos(pos)
	if !m.canSpawnAt(tx, spawnPos) {
		return false
	}
	opts := world.EntitySpawnOpts{Position: spawnPos}
	tx.AddEntity(opts.New(t, emptyEntityConfig{}))
	return true
}

func (m MobSpawner) randomSpawnPos(pos cube.Pos) mgl64.Vec3 {
	r := int(m.spawnRange)
	if r < 1 {
		r = 1
	}
	x := pos.X() + rand.IntN(r*2+1) - r
	y := pos.Y() + rand.IntN(3) - 1
	z := pos.Z() + rand.IntN(r*2+1) - r
	return mgl64.Vec3{float64(x) + 0.5, float64(y), float64(z) + 0.5}
}

func (m MobSpawner) resetDelay() {
	if m.delay == nil {
		return
	}
	min := m.minDelay
	max := m.maxDelay
	if min <= 0 {
		min = 200
	}
	if max < min {
		max = min
	}
	delay := min + int32(rand.IntN(int(max-min+1)))
	m.delay.Store(delay)
}

func (m MobSpawner) playerWithinRange(tx *world.Tx, pos cube.Pos, r int) bool {
	r2 := float64(r * r)
	center := pos.Vec3Centre()
	for p := range tx.Players() {
		if p.Position().Sub(center).LenSqr() <= r2 {
			return true
		}
	}
	return false
}

func (m MobSpawner) tooManyNearby(tx *world.Tx, pos cube.Pos) bool {
	if m.maxNearbyEntities <= 0 {
		return false
	}
	r := float64(m.spawnRange * 2)
	box := cube.Box(
		float64(pos.X())-r, float64(pos.Y())-r, float64(pos.Z())-r,
		float64(pos.X())+r+1, float64(pos.Y())+r+1, float64(pos.Z())+r+1,
	)
	nearby := int32(0)
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() != m.entity {
			continue
		}
		nearby++
		if nearby >= m.maxNearbyEntities {
			return true
		}
	}
	return false
}

func (m MobSpawner) canSpawnAt(tx *world.Tx, pos mgl64.Vec3) bool {
	box := cube.Box(pos[0]-0.3, pos[1], pos[2]-0.3, pos[0]+0.3, pos[1]+1.95, pos[2]+0.3)
	min, max := box.Min(), box.Max()
	minX, minY, minZ := int(min[0]), int(min[1]), int(min[2])
	maxX, maxY, maxZ := int(max[0]), int(max[1]), int(max[2])
	for y := minY; y <= maxY; y++ {
		for x := minX; x <= maxX; x++ {
			for z := minZ; z <= maxZ; z++ {
				blockPos := cube.Pos{x, y, z}
				b := tx.Block(blockPos)
				if len(b.Model().BBox(blockPos, tx)) > 0 {
					return false
				}
			}
		}
	}
	for e := range tx.EntitiesWithin(box) {
		if e.H().Type().EncodeEntity() == m.entity {
			return false
		}
	}
	return true
}

// EncodeNBT ...
func (m MobSpawner) EncodeNBT() map[string]any {
	encoded := map[string]any{
		"id":                   "MobSpawner",
		"EntityIdentifier":     m.entity,
		"MinSpawnDelay":        m.minDelay,
		"MaxSpawnDelay":        m.maxDelay,
		"SpawnCount":           m.spawnCount,
		"SpawnRange":           m.spawnRange,
		"RequiredPlayerRange":  m.requiredPlayerRange,
		"MaxNearbyEntities":    m.maxNearbyEntities,
	}
	if m.delay != nil {
		encoded["Delay"] = int16(m.delay.Load())
	}
	return encoded
}

// DecodeNBT ...
func (m MobSpawner) DecodeNBT(data map[string]any) any {
	spawner := NewMobSpawner()
	if entity := nbtconv.String(data, "EntityIdentifier"); entity != "" {
		spawner.entity = entity
	}
	if delay, ok := data["Delay"]; ok {
		switch v := delay.(type) {
		case int16:
			spawner.delay.Store(int32(v))
		case int32:
			spawner.delay.Store(v)
		case int64:
			spawner.delay.Store(int32(v))
		}
	}
	if v := nbtconv.Int32(data, "MinSpawnDelay"); v > 0 {
		spawner.minDelay = v
	}
	if v := nbtconv.Int32(data, "MaxSpawnDelay"); v > 0 {
		spawner.maxDelay = v
	}
	if v := nbtconv.Int32(data, "SpawnCount"); v > 0 {
		spawner.spawnCount = v
	}
	if v := nbtconv.Int32(data, "SpawnRange"); v > 0 {
		spawner.spawnRange = v
	}
	if v := nbtconv.Int32(data, "RequiredPlayerRange"); v > 0 {
		spawner.requiredPlayerRange = v
	}
	if v := nbtconv.Int32(data, "MaxNearbyEntities"); v > 0 {
		spawner.maxNearbyEntities = v
	}
	return spawner
}

// BreakInfo ...
func (MobSpawner) BreakInfo() BreakInfo {
	return newBreakInfo(5, pickaxeHarvestable, pickaxeEffective, simpleDrops())
}

// EncodeItem ...
func (MobSpawner) EncodeItem() (name string, meta int16) {
	return "minecraft:mob_spawner", 0
}

// EncodeBlock ...
func (MobSpawner) EncodeBlock() (string, map[string]any) {
	return "minecraft:mob_spawner", nil
}

type emptyEntityConfig struct{}

func (emptyEntityConfig) Apply(_ *world.EntityData) {}
