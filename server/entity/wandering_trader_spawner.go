package entity

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/goleveldb/leveldb"
)

// wanderingTraderData is Java's WanderingTraderData saved data.
type wanderingTraderData struct {
	SpawnDelay  int `json:"spawn_delay"`
	SpawnChance int `json:"spawn_chance"`
}

// wanderingTraderSpawner is a world's WanderingTraderSpawner: its unsaved
// tick countdown and its saved data.
type wanderingTraderSpawner struct {
	tickDelay int
	data      *wanderingTraderData
}

const wanderingTraderNamespace = "wandering_trader"

var wanderingTraderSpawners sync.Map

// wanderingTraderSpawnTick ports WanderingTraderSpawner.tick: every minute
// the saved spawn delay counts down, and once a day passes a trader spawns
// with the saved chance, which grows by 25 up to 75 after each failure.
func wanderingTraderSpawnTick(tx *world.Tx, random *java26.Random) {
	w := tx.World()
	if w.Dimension() != world.Overworld || !w.SpawnMobsEnabled() || !w.GameRuleBool("spawn_wandering_traders") {
		return
	}
	v, _ := wanderingTraderSpawners.LoadOrStore(w, &wanderingTraderSpawner{tickDelay: 1200})
	s := v.(*wanderingTraderSpawner)
	if s.tickDelay--; s.tickDelay > 0 {
		return
	}
	s.tickDelay = 1200
	data := s.load(w)
	data.SpawnDelay -= 1200
	if data.SpawnDelay <= 0 {
		data.SpawnDelay = 24000
		chance := data.SpawnChance
		data.SpawnChance = min(max(chance+25, 25), 75)
		if int(random.NextInt(100)) <= chance && spawnWanderingTrader(tx, random) {
			data.SpawnChance = 25
		}
	}
	if raw, err := json.Marshal(data); err != nil || w.SaveWorldData(wanderingTraderNamespace, raw) != nil {
		// Worlds without namespaced storage keep the data in memory.
		return
	}
}

// load returns the world's WanderingTraderData, read from the world once.
func (s *wanderingTraderSpawner) load(w *world.World) *wanderingTraderData {
	if s.data != nil {
		return s.data
	}
	s.data = &wanderingTraderData{SpawnDelay: 24000, SpawnChance: 25}
	raw, err := w.LoadWorldData(wanderingTraderNamespace)
	if err != nil {
		if !errors.Is(err, leveldb.ErrNotFound) {
			panic("load wandering trader data: " + err.Error())
		}
		return s.data
	}
	if err := json.Unmarshal(raw, s.data); err != nil {
		panic("decode wandering trader data: " + err.Error())
	}
	return s.data
}

// spawnWanderingTrader ports WanderingTraderSpawner.spawn: one in ten times
// a trader appears near a random player, or near a bell within 48 blocks of
// them, with up to two trader llamas on leads. It reports true without
// players so that the spawn chance resets.
func spawnWanderingTrader(tx *world.Tx, random *java26.Random) bool {
	var players []world.Entity
	for p := range tx.Players() {
		if l, ok := p.(Living); !ok || !l.Dead() {
			players = append(players, p)
		}
	}
	if len(players) == 0 {
		return true
	}
	player := players[random.NextInt(int32(len(players)))]
	if random.NextInt(10) != 0 {
		return false
	}
	reference := cube.PosFromVec3(player.Position())
	if bell, _, ok := tx.NearestPOI(reference, world.Meeting, 48, nil); ok {
		reference = bell
	}
	pos, ok := traderSpawnPosition(tx, random, reference, 48)
	if !ok || !traderHasSpace(tx, pos) || tx.Biome(pos).String() == "the_void" {
		return false
	}
	handle := NewWanderingTrader(world.EntitySpawnOpts{Position: pos.Vec3Middle(), SpawnReason: "event"})
	e, ok := tx.AddEntity(handle).(*WanderingTrader)
	if !ok {
		return false
	}
	for range 2 {
		spawnTraderLlama(tx, random, e)
	}
	e.SetDespawnDelay(48000)
	e.SetWanderTarget(reference)
	e.SetHome(reference, 16)
	return true
}

// spawnTraderLlama ports WanderingTraderSpawner.tryToSpawnLlamaFor.
func spawnTraderLlama(tx *world.Tx, random *java26.Random, trader *WanderingTrader) {
	pos, ok := traderSpawnPosition(tx, random, cube.PosFromVec3(trader.Position()), 4)
	if !ok {
		return
	}
	handle := NewTraderLlama(world.EntitySpawnOpts{Position: pos.Vec3Middle(), SpawnReason: "event"})
	if llama, ok := tx.AddEntity(handle).(Leashable); ok {
		llama.AttachLeash(trader, tx)
	}
}

// traderSpawnPosition ports WanderingTraderSpawner.findSpawnPositionNear: up
// to ten columns within radius of reference, on top of their
// MOTION_BLOCKING_NO_LEAVES heightmap, that pass the ON_GROUND placement.
func traderSpawnPosition(tx *world.Tx, random *java26.Random, reference cube.Pos, radius int) (cube.Pos, bool) {
	for range 10 {
		x := reference[0] + int(random.NextInt(int32(radius*2))) - radius
		z := reference[2] + int(random.NextInt(int32(radius*2))) - radius
		pos := cube.Pos{x, motionBlockingNoLeavesHeight(tx, x, z), z}
		if onGroundSpawnPosition(tx, pos) {
			return pos, true
		}
	}
	return cube.Pos{}, false
}

// motionBlockingNoLeavesHeight returns the Y above the highest block of a
// column that blocks motion or holds a liquid, ignoring leaves.
func motionBlockingNoLeavesHeight(tx *world.Tx, x, z int) int {
	for y := tx.HighestBlock(x, z); y >= tx.Range()[0]; y-- {
		pos := cube.Pos{x, y, z}
		if _, leaves := tx.Block(pos).(block.Leaves); leaves {
			continue
		}
		if _, liquid := tx.Liquid(pos); liquid || !isPassable(tx, pos) {
			return y + 1
		}
	}
	return tx.Range()[0]
}

// onGroundSpawnPosition ports SpawnPlacementTypes.ON_GROUND: inside the world
// border, above a block mobs may spawn on, with the position and the block
// above it empty enough to spawn in.
func onGroundSpawnPosition(tx *world.Tx, pos cube.Pos) bool {
	if pos.OutOfBounds(tx.Range()) || !tx.World().WithinBorder(pos) {
		return false
	}
	below := pos.Side(cube.FaceDown)
	return validSpawnFloor(tx, below) && emptySpawnBlock(tx, pos) && emptySpawnBlock(tx, pos.Side(cube.FaceUp))
}

// validSpawnFloor ports BlockState.isValidSpawn for a mob that is not fire
// immune: a sturdy top face on a block emitting light below 14 that is not
// leaves, ice, glass or a barrier.
func validSpawnFloor(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	switch b.(type) {
	case block.Leaves, block.Ice, block.PackedIce, block.BlueIce, block.Glass, block.StainedGlass, block.TintedGlass, block.Barrier, block.Magma:
		return false
	}
	if emitter, ok := b.(block.LightEmitter); ok && emitter.LightEmissionLevel() >= 14 {
		return false
	}
	return b.Model().FaceSolid(pos, cube.FaceUp, tx)
}

// emptySpawnBlock ports NaturalSpawner.isValidEmptySpawnBlock: no full
// collision, redstone power, liquid, rails or blocks that hurt mobs.
func emptySpawnBlock(tx *world.Tx, pos cube.Pos) bool {
	b := tx.Block(pos)
	for _, box := range b.Model().BBox(pos, tx) {
		if box.Width() >= 1 && box.Height() >= 1 && box.Length() >= 1 {
			return false
		}
	}
	if _, liquid := tx.Liquid(pos); liquid {
		return false
	}
	switch b := b.(type) {
	case world.RedstonePowerSource, world.RedstonePowerRelayer,
		block.Rail, block.PoweredRail, block.DetectorRail, block.ActivatorRail,
		block.Fire, block.Cactus, block.SweetBerries, block.PowderSnow, block.Magma:
		return false
	case block.Flower:
		return b.Type != block.WitherRose()
	case block.Campfire:
		return b.Extinguished
	}
	return true
}

// traderHasSpace ports WanderingTraderSpawner.hasEnoughSpace: the two by
// three by two blocks from pos have no collision.
func traderHasSpace(tx *world.Tx, pos cube.Pos) bool {
	for x := range 2 {
		for y := range 3 {
			for z := range 2 {
				if !isPassable(tx, pos.Add(cube.Pos{x, y, z})) {
					return false
				}
			}
		}
	}
	return true
}
