package entity

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"slices"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/player/bossbar"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

type dragonFight struct {
	data                                world.DragonFightData
	spikes                              [10]java26.EndSpike
	players                             map[*world.EntityHandle]uint64
	playerGeneration                    uint64
	playerScan, dragonSeen, crystalScan int
	aliveCrystals                       int
	respawnScratch                      []*EndCrystal
	bar                                 bossbar.BossBar
	visible                             bool
}

type dragonFightViewer interface {
	world.Entity
	SendBossBar(bossbar.BossBar)
	RemoveBossBarID(string)
}

func newDragonFight(seed int64, data world.DragonFightData) world.DragonFightController {
	data.Gateways = slices.Clone(data.Gateways)
	data.RespawnCrystalIDs = slices.Clone(data.RespawnCrystalIDs)
	if data.ExitPortal != nil {
		pos := *data.ExitPortal
		data.ExitPortal = &pos
	}
	if len(data.Gateways) == 0 {
		data.Gateways = make([]int32, 20)
		for i := range data.Gateways {
			data.Gateways[i] = int32(i)
		}
		random := java26.NewLegacyRandom(seed)
		for n := len(data.Gateways); n > 1; n-- {
			j := int(random.NextInt(int32(n)))
			data.Gateways[n-1], data.Gateways[j] = data.Gateways[j], data.Gateways[n-1]
		}
	}
	return &dragonFight{
		data: data, spikes: java26.EndSpikes(seed), players: make(map[*world.EntityHandle]uint64),
		playerScan: 21, visible: !data.DragonKilled,
		bar: bossbar.New("Ender Dragon").WithID("end-dragon:" + uuid.NewString()).WithColour(bossbar.Pink()),
	}
}

func (f *dragonFight) Data() world.DragonFightData {
	data := f.data
	data.Gateways = slices.Clone(data.Gateways)
	data.RespawnCrystalIDs = slices.Clone(data.RespawnCrystalIDs)
	if data.ExitPortal != nil {
		pos := *data.ExitPortal
		data.ExitPortal = &pos
	}
	data.IsRespawning = data.RespawnStage != ""
	return data
}

func (f *dragonFight) Tick(tx *world.Tx) {
	f.setVisible(tx, !f.data.DragonKilled)
	f.playerScan++
	if f.playerScan >= 20 {
		f.updatePlayers(tx)
		f.playerScan = 0
	}
	if len(f.players) == 0 {
		tx.LoadDragonArena(false)
		return
	}
	if !tx.LoadDragonArena(true) {
		return
	}
	if f.data.NeedsStateScanning {
		f.scanState(tx)
		f.data.NeedsStateScanning = false
	}
	if f.data.RespawnStage != "" {
		crystals := f.respawnCrystals(tx)
		if len(crystals) == 0 {
			clear(f.respawnScratch)
			f.abortRespawn(tx)
			return
		}
		time := int(f.data.RespawnTime)
		f.data.RespawnTime++
		f.tickRespawn(tx, crystals, time)
		clear(f.respawnScratch)
	}
	if !f.data.DragonKilled {
		f.dragonSeen++
		if !f.data.DragonSpawned || f.dragonSeen >= 1200 {
			f.findOrCreateDragon(tx)
			f.dragonSeen = 0
		}
		f.crystalScan++
		if f.crystalScan >= 100 {
			f.updateCrystalCount(tx)
		}
	}
}

func (f *dragonFight) updatePlayers(tx *world.Tx) {
	f.playerGeneration++
	for e := range tx.Players() {
		p, ok := e.(dragonFightViewer)
		if !ok || e.Position().Sub(mgl64.Vec3{0, 128, 0}).LenSqr() > 192*192 {
			continue
		}
		if living, ok := e.(interface{ Dead() bool }); ok && living.Dead() {
			continue
		}
		if _, present := f.players[e.H()]; !present && f.visible {
			p.SendBossBar(f.bar)
		}
		f.players[e.H()] = f.playerGeneration
	}
	for handle, generation := range f.players {
		if generation == f.playerGeneration {
			continue
		}
		delete(f.players, handle)
		id := f.bar.ID()
		if e, ok := handle.Entity(tx); ok {
			if p, ok := e.(dragonFightViewer); ok {
				p.RemoveBossBarID(id)
			}
		} else {
			handle.Do(func(_ *world.Tx, e world.Entity) {
				if p, ok := e.(dragonFightViewer); ok {
					p.RemoveBossBarID(id)
				}
			})
		}
	}
}

func (f *dragonFight) setVisible(tx *world.Tx, visible bool) {
	if f.visible == visible {
		return
	}
	f.visible = visible
	for handle := range f.players {
		if e, ok := handle.Entity(tx); ok {
			if p, ok := e.(dragonFightViewer); ok {
				if visible {
					p.SendBossBar(f.bar)
				} else {
					p.RemoveBossBarID(f.bar.ID())
				}
			}
		}
	}
}

func (f *dragonFight) scanState(tx *world.Tx) {
	active := false
	for range tx.EndPortalPositions() {
		active = true
		break
	}
	f.data.PreviouslyKilled = active
	if !active {
		if _, found := f.findExitPortal(tx); !found {
			f.spawnExitPortal(tx, false)
		}
	}
	var dragon *EnderDragon
	for e := range tx.Entities() {
		if d, ok := e.(*EnderDragon); ok {
			dragon = d
			break
		}
	}
	f.data.DragonKilled = dragon == nil
	if dragon != nil {
		f.bindDragon(dragon)
		if !active {
			_ = dragon.CloseIn(tx)
			f.data.DragonSpawned = false
			f.data.DragonUniqueID = 0
		}
	}
	if !f.data.PreviouslyKilled && f.data.DragonKilled {
		f.data.DragonKilled = false
	}
}

func endFightEntityID(handle *world.EntityHandle) int64 {
	id := handle.UUID()
	return int64(binary.LittleEndian.Uint64(id[8:]))
}

func (f *dragonFight) AttachDragon(e world.Entity) {
	if d, ok := e.(*EnderDragon); ok && f.data.DragonSpawned && endFightEntityID(d.H()) == f.data.DragonUniqueID {
		d.SetDragonFight(f)
		d.SetFightOrigin(cube.Pos{})
	}
}

func (f *dragonFight) bindDragon(d *EnderDragon) {
	f.data.DragonSpawned, f.data.DragonUniqueID = true, endFightEntityID(d.H())
	f.AttachDragon(d)
}

func (f *dragonFight) findOrCreateDragon(tx *world.Tx) {
	for e := range tx.Entities() {
		if d, ok := e.(*EnderDragon); ok {
			f.bindDragon(d)
			return
		}
	}
	f.createDragon(tx)
}

func (f *dragonFight) createDragon(tx *world.Tx) *EnderDragon {
	opts := world.EntitySpawnOpts{Position: mgl64.Vec3{0, 128, 0}, Rotation: cube.Rotation{float64(rand.Float32() * 360), 0}, SpawnReason: "event", SpawnSeed: rand.Int64()}
	handle := opts.New(EnderDragonType, EnderDragonBehaviourConfig{Phase: DragonHoldingPattern, Fight: f})
	f.data.DragonSpawned, f.data.DragonUniqueID = true, endFightEntityID(handle)
	return tx.AddEntity(handle).(*EnderDragon)
}

func (f *dragonFight) UpdateDragon(tx *world.Tx, e world.Entity) {
	d, ok := e.(*EnderDragon)
	if !ok || !f.data.DragonSpawned || endFightEntityID(d.H()) != f.data.DragonUniqueID {
		return
	}
	f.dragonSeen = 0
	health := max(0., min(1., d.Health()/d.MaxHealth()))
	name := f.bar.Text()
	if d.NameTag() != "" {
		name = d.NameTag()
	}
	if health == f.bar.HealthPercentage() && name == f.bar.Text() {
		return
	}
	f.bar = bossbar.New(name).WithID(f.bar.ID()).WithColour(bossbar.Pink()).WithHealthPercentage(health)
	if !f.visible {
		return
	}
	for handle := range f.players {
		if p, present := handle.Entity(tx); present {
			if viewer, ok := p.(dragonFightViewer); ok {
				viewer.SendBossBar(f.bar)
			}
		}
	}
}

func (f *dragonFight) SetDragonKilled(tx *world.Tx, e world.Entity) {
	if !f.data.DragonSpawned || endFightEntityID(e.H()) != f.data.DragonUniqueID {
		return
	}
	f.bar = f.bar.WithHealthPercentage(0)
	f.setVisible(tx, false)
	f.spawnExitPortal(tx, true)
	if n := len(f.data.Gateways); n != 0 {
		index := f.data.Gateways[n-1]
		f.data.Gateways = f.data.Gateways[:n-1]
		angle := 2 * (-math.Pi + (math.Pi/20)*float64(index))
		pos := cube.Pos{int(math.Floor(96 * math.Cos(angle))), 75, int(math.Floor(96 * math.Sin(angle)))}
		spawnFightGateway(tx, pos)
	}
	if !f.data.PreviouslyKilled {
		tx.SetBlock(cube.Pos{0, tx.JavaMotionBlockingHeight(0, 0, false), 0}, block.DragonEgg{}, nil)
	}
	f.data.PreviouslyKilled, f.data.DragonKilled = true, true
}

func (f *dragonFight) HasPreviouslyKilledDragon() bool { return f.data.PreviouslyKilled }
func (f *dragonFight) AliveCrystals() int              { return f.aliveCrystals }

func (f *dragonFight) spikeCrystals(tx *world.Tx, visit func(*EndCrystal)) {
	for i := range f.spikes {
		s := &f.spikes[i]
		box := cube.Box(float64(s.X-s.Radius), float64(tx.Range().Min()), float64(s.Z-s.Radius), float64(s.X+s.Radius), float64(tx.Range().Max()+1), float64(s.Z+s.Radius))
		for e := range tx.EntitiesWithin(box.Grow(2)) {
			if c, ok := e.(*EndCrystal); ok && !c.behaviour().exploded && c.H().Type().BBox(c).Translate(c.Position()).IntersectsWith(box) {
				visit(c)
			}
		}
	}
}

func (f *dragonFight) updateCrystalCount(tx *world.Tx) {
	f.crystalScan, f.aliveCrystals = 0, 0
	f.spikeCrystals(tx, func(*EndCrystal) { f.aliveCrystals++ })
}

func (f *dragonFight) resetSpikeCrystals(tx *world.Tx) {
	f.spikeCrystals(tx, func(c *EndCrystal) { c.SetPermanentlyInvulnerable(false); c.SetBeamTarget(nil) })
}

func (f *dragonFight) OnCrystalDestroyed(tx *world.Tx, crystal world.Entity, pos cube.Pos, source world.DamageSource) {
	if f.data.RespawnStage != "" && slices.Contains(f.data.RespawnCrystalIDs, endFightEntityID(crystal.H())) {
		f.abortRespawn(tx)
		return
	}
	f.updateCrystalCount(tx)
	for e := range tx.Entities() {
		if d, ok := e.(*EnderDragon); ok && f.data.DragonSpawned && endFightEntityID(d.H()) == f.data.DragonUniqueID {
			d.CrystalDestroyed(tx, crystal, pos, source)
			return
		}
	}
}

func (f *dragonFight) abortRespawn(tx *world.Tx) {
	f.data.RespawnStage, f.data.RespawnTime, f.data.IsRespawning = "", 0, false
	f.resetSpikeCrystals(tx)
	f.spawnExitPortal(tx, true)
}

func (f *dragonFight) TryRespawn(tx *world.Tx) bool {
	if !f.data.DragonKilled || f.data.RespawnStage != "" {
		return false
	}
	if f.data.ExitPortal == nil {
		if _, found := f.findExitPortal(tx); !found {
			f.spawnExitPortal(tx, true)
		}
	}
	center := f.data.ExitPortal.Side(cube.FaceUp)
	var ids []int64
	for _, face := range cube.HorizontalFaces() {
		pos := center.Add(golemScale(golemFaceOffset(face), 3))
		box := cube.Box(float64(pos[0]), float64(pos[1]), float64(pos[2]), float64(pos[0]+1), float64(pos[1]+1), float64(pos[2]+1))
		before := len(ids)
		for e := range tx.EntitiesWithin(box.Grow(2)) {
			if c, ok := e.(*EndCrystal); ok && !c.behaviour().exploded && c.H().Type().BBox(c).Translate(c.Position()).IntersectsWith(box) {
				ids = append(ids, endFightEntityID(c.H()))
			}
		}
		if len(ids) == before {
			return false
		}
	}
	for {
		portal, found := f.findExitPortal(tx)
		if !found {
			break
		}
		for x := range 7 {
			for y := range 7 {
				for z := range 5 {
					pos := portal.block(x, y, z)
					switch tx.Block(pos).(type) {
					case block.Bedrock, block.EndPortal:
						tx.SetBlock(pos, block.EndStone{}, nil)
					}
				}
			}
		}
	}
	f.data.RespawnStage, f.data.RespawnTime, f.data.IsRespawning = "start", 0, true
	f.data.RespawnCrystalIDs = ids
	f.spawnExitPortal(tx, false)
	return true
}

func (f *dragonFight) respawnCrystals(tx *world.Tx) []*EndCrystal {
	count := len(f.data.RespawnCrystalIDs)
	if cap(f.respawnScratch) < count {
		f.respawnScratch = make([]*EndCrystal, count)
	} else {
		f.respawnScratch = f.respawnScratch[:count]
		clear(f.respawnScratch)
	}
	for e := range tx.Entities() {
		crystal, ok := e.(*EndCrystal)
		if !ok || crystal.behaviour().exploded {
			continue
		}
		id := endFightEntityID(crystal.H())
		for i, expected := range f.data.RespawnCrystalIDs {
			if id == expected {
				f.respawnScratch[i] = crystal
			}
		}
	}
	// Preserve the saved reference order without cloning the entity set once
	// per participant. Tick clears these transaction-bound values before exit.
	crystals := f.respawnScratch[:0]
	for _, crystal := range f.respawnScratch {
		if crystal != nil {
			crystals = append(crystals, crystal)
		}
	}
	return crystals
}

func (f *dragonFight) respawnStage(stage string) { f.data.RespawnStage, f.data.RespawnTime = stage, 0 }

func (f *dragonFight) tickRespawn(tx *world.Tx, crystals []*EndCrystal, time int) {
	beam := cube.Pos{0, 128, 0}
	switch f.data.RespawnStage {
	case "start":
		for _, c := range crystals {
			c.SetBeamTarget(&beam)
		}
		f.respawnStage("preparing_to_summon_pillars")
	case "preparing_to_summon_pillars":
		if time >= 100 {
			f.respawnStage("summoning_pillars")
		} else if time == 0 || time == 50 || time == 51 || time == 52 || time >= 95 {
			endFightPulse(tx)
		}
	case "summoning_pillars":
		if time%40 != 0 && time%40 != 39 {
			return
		}
		index := time / 40
		if index >= len(f.spikes) {
			if time%40 == 0 {
				f.respawnStage("summoning_dragon")
			}
			return
		}
		spike := &f.spikes[index]
		if time%40 == 0 {
			beam = cube.Pos{spike.X, spike.Height + 1, spike.Z}
			for _, c := range crystals {
				c.SetBeamTarget(&beam)
			}
		} else {
			for z := spike.Z - 10; z <= spike.Z+10; z++ {
				for y := spike.Height - 10; y <= spike.Height+10; y++ {
					for x := spike.X - 10; x <= spike.X+10; x++ {
						tx.SetBlock(cube.Pos{x, y, z}, nil, nil)
					}
				}
			}
			block.ExplosionConfig{}.Explode(tx, endFightExplosion{mgl64.Vec3{float64(spike.X) + .5, float64(spike.Height), float64(spike.Z) + .5}, 5})
			placeFightSpike(tx, spike, beam)
		}
	case "summoning_dragon":
		if time >= 100 {
			f.respawnStage("")
			f.data.DragonKilled, f.data.IsRespawning = false, false
			f.createDragon(tx)
			f.resetSpikeCrystals(tx)
			for _, c := range crystals {
				c.SetBeamTarget(nil)
				block.ExplosionConfig{NoBlockDamage: true}.Explode(tx, world.NewEntityExplosionSource(c, 6))
				_ = c.CloseIn(tx)
			}
		} else if time >= 80 || time > 0 && time < 5 {
			endFightPulse(tx)
		} else if time == 0 {
			for _, c := range crystals {
				c.SetBeamTarget(&beam)
			}
		}
	}
}

type endFightExplosion struct {
	position mgl64.Vec3
	size     float64
}

func (e endFightExplosion) Position() mgl64.Vec3 { return e.position }
func (e endFightExplosion) Size() float64        { return e.size }

var _ world.DragonFightController = (*dragonFight)(nil)
