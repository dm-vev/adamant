package entity_test

import (
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	endgen "github.com/df-mc/dragonfly/server/world/generator/advanced/end"
	"github.com/df-mc/dragonfly/server/world/mcdb"
	"github.com/go-gl/mathgl/mgl64"
)

func TestEndFightNativeLifecycleAndMCDBRestart(t *testing.T) {
	dir := t.TempDir()
	const seed = 5023
	open := func() (*world.World, *mcdb.DB) {
		db, err := mcdb.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		seed := db.Settings().ResolveSeed(seed)
		w := world.Config{
			Provider: db, Dim: world.End, Seed: seed, Generator: endgen.New(seed),
			Entities: entity.DefaultRegistry, Synchronous: true, RandomTickSpeed: -1,
		}.New()
		return w, db
	}
	closeWorld := func(w *world.World, db *mcdb.DB) {
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
	}
	participantLoaders := make(map[*world.EntityHandle]*world.Loader)
	addPlayer := func(w *world.World) *world.EntityHandle {
		handle := (world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{0, 128, 0}, GameMode: world.GameModeCreative})
		var added *world.EntityHandle
		<-w.Exec(func(tx *world.Tx) {
			p := tx.AddEntity(handle).(*player.Player)
			// Keep the real participant stationary without a network client's
			// movement loop while the arena ticket is exercised.
			p.SetImmobile()
			added = p.H()
			// A connected player owns a client chunk loader independently of
			// the fight ticket. Keep that ownership across the 20-tick scan.
			loader := world.NewLoader(1, w, world.NopViewer{})
			loader.Move(tx, p.Position())
			loader.Load(tx, 9)
			participantLoaders[handle] = loader
		})
		return added
	}
	advanceUntil := func(w *world.World, limit int, ready func(*world.Tx) bool) {
		for range limit {
			w.AdvanceTick()
			var ok bool
			<-w.Exec(func(tx *world.Tx) { ok = ready(tx) })
			if ok {
				return
			}
		}
		t.Fatal("End fight did not reach the expected lifecycle state")
	}
	countDragons := func(tx *world.Tx) int {
		count := 0
		for e := range tx.Entities() {
			if e.H().Type().EncodeEntity() == "minecraft:ender_dragon" {
				count++
			}
		}
		return count
	}
	removePlayer := func(w *world.World, handle *world.EntityHandle) {
		<-w.Exec(func(tx *world.Tx) {
			if player, ok := handle.Entity(tx); ok {
				tx.RemoveEntity(player)
			}
			if loader := participantLoaders[handle]; loader != nil {
				loader.Close(tx)
				delete(participantLoaders, handle)
			}
		})
	}

	w, db := open()
	player := addPlayer(w)
	advanceUntil(w, 3000, func(tx *world.Tx) bool {
		fight := tx.DragonFight()
		return fight != nil && fight.Data().DragonSpawned && countDragons(tx) == 1
	})
	<-w.Exec(func(tx *world.Tx) {
		var dragon *entity.EnderDragon
		for e := range tx.Entities() {
			if d, ok := e.(*entity.EnderDragon); ok {
				dragon = d
				break
			}
		}
		if dragon == nil {
			t.Fatal("startup registered fight state without a loaded dragon")
		}
		var attacker world.Entity
		if p, ok := player.Entity(tx); ok {
			attacker = p
		}
		if _, ok := dragon.HurtPart("head", 1000, entity.AttackDamageSource{Attacker: attacker}); !ok {
			t.Fatal("lethal player damage did not enter the dragon death lifecycle")
		}
	})
	advanceUntil(w, 1000, func(tx *world.Tx) bool {
		for e := range tx.Entities() {
			if d, ok := e.(*entity.EnderDragon); ok {
				return d.Health() == 0
			}
		}
		return false
	})
	advanceUntil(w, 240, func(tx *world.Tx) bool { return tx.DragonFight().Data().DragonKilled })
	var killed world.DragonFightData
	var eggPos cube.Pos
	<-w.Exec(func(tx *world.Tx) {
		killed = tx.DragonFight().Data()
		eggPos = cube.Pos{0, tx.JavaMotionBlockingHeight(0, 0, false) - 1, 0}
		if len(killed.Gateways) != 19 || countDragons(tx) != 0 {
			t.Fatalf("first kill state: gateways=%d dragons=%d", len(killed.Gateways), countDragons(tx))
		}
		if _, ok := tx.Block(eggPos).(block.DragonEgg); !ok {
			t.Fatalf("first kill did not place the egg at the podium: %T", tx.Block(eggPos))
		}
	})
	removePlayer(w, player)
	closeWorld(w, db)

	w, db = open()
	player = addPlayer(w)
	advanceUntil(w, 3000, func(tx *world.Tx) bool {
		return tx.LoadDragonArena(true) && tx.DragonFight() != nil && tx.DragonFight().Data().DragonKilled && countDragons(tx) == 0
	})
	<-w.Exec(func(tx *world.Tx) {
		data := tx.DragonFight().Data()
		if len(data.Gateways) != len(killed.Gateways) || !data.PreviouslyKilled || !data.DragonKilled {
			t.Fatalf("kill/gateway state was not restored: %+v", data)
		}
		if _, ok := tx.Block(eggPos).(block.DragonEgg); !ok {
			t.Fatalf("egg was lost or overwritten on restart: %T", tx.Block(eggPos))
		}
		// Dragon death XP can remain at the podium after restart and is a
		// genuine placement blocker. Keep the lifecycle fixture's placement
		// independent of those unrelated drops.
		for e := range tx.EntitiesWithin(cube.Box(-5, -2, -5, 5, 6, 5).Translate(data.ExitPortal.Vec3())) {
			if e.H().Type().EncodeEntity() == "minecraft:xp_orb" {
				tx.RemoveEntity(e)
			}
		}

		portal := *data.ExitPortal
		for _, face := range cube.HorizontalFaces() {
			offset := cube.Pos{}
			switch face {
			case cube.FaceNorth:
				offset[2] = -3
			case cube.FaceSouth:
				offset[2] = 3
			case cube.FaceEast:
				offset[0] = 3
			case cube.FaceWest:
				offset[0] = -3
			}
			target := portal.Side(cube.FaceUp).Add(offset)
			support := target.Side(cube.FaceDown)
			tx.SetBlock(support, block.Obsidian{}, nil)
			ctx := &item.UseContext{}
			if !((item.EndCrystal{}).UseOnBlock(support, cube.FaceUp, mgl64.Vec3{}, tx, nil, ctx)) {
				t.Fatalf("real End Crystal item placement failed at %v", support)
			}
		}
		if !tx.DragonFight().Data().IsRespawning {
			t.Fatal("four placed crystals did not start respawn")
		}
	})
	advanceUntil(w, 20, func(tx *world.Tx) bool { return tx.DragonFight().Data().RespawnTime > 0 })
	var progress world.DragonFightData
	<-w.Exec(func(tx *world.Tx) { progress = tx.DragonFight().Data() })
	removePlayer(w, player)
	closeWorld(w, db)

	w, db = open()
	player = addPlayer(w)
	beforeRestart := world.DragonFightData{}
	<-w.Exec(func(tx *world.Tx) { beforeRestart = tx.DragonFight().Data() })
	if beforeRestart.RespawnStage != progress.RespawnStage || beforeRestart.RespawnTime != progress.RespawnTime || len(beforeRestart.Gateways) != len(progress.Gateways) || !slices.Equal(beforeRestart.RespawnCrystalIDs, progress.RespawnCrystalIDs) {
		t.Fatalf("respawn/crystal/gateway progress did not survive MCDB restart: before=%+v saved=%+v", beforeRestart, progress)
	}
	advanceUntil(w, 3000, func(tx *world.Tx) bool { return tx.LoadDragonArena(true) && tx.DragonFight().Data().IsRespawning })
	advanceUntil(w, 900, func(tx *world.Tx) bool {
		data := tx.DragonFight().Data()
		return !data.IsRespawning && !data.DragonKilled && data.DragonSpawned && countDragons(tx) == 1
	})
	<-w.Exec(func(tx *world.Tx) {
		if countDragons(tx) != 1 {
			t.Fatalf("respawn completed with %d dragons", countDragons(tx))
		}
		if _, ok := tx.Block(eggPos).(block.DragonEgg); !ok {
			t.Fatalf("respawn duplicated/removed first-kill egg at %v: %T", eggPos, tx.Block(eggPos))
		}
		if len(tx.DragonFight().Data().Gateways) != len(killed.Gateways) {
			t.Fatal("respawn changed gateway progress")
		}
	})
	var active world.DragonFightData
	<-w.Exec(func(tx *world.Tx) {
		active = tx.DragonFight().Data()
	})
	removePlayer(w, player)
	closeWorld(w, db)

	w, db = open()
	player = addPlayer(w)
	advanceUntil(w, 3000, func(tx *world.Tx) bool {
		return tx.LoadDragonArena(true) && tx.DragonFight().Data().DragonSpawned && countDragons(tx) == 1
	})
	for range 1300 {
		w.AdvanceTick()
	}
	<-w.Exec(func(tx *world.Tx) {
		data := tx.DragonFight().Data()
		if data.DragonUniqueID != active.DragonUniqueID || !data.DragonSpawned || countDragons(tx) != 1 {
			t.Fatalf("living dragon was duplicated or lost after MCDB restart: data=%+v dragons=%d", data, countDragons(tx))
		}
		if _, ok := tx.Block(eggPos).(block.DragonEgg); !ok {
			t.Fatalf("first-kill egg was not retained: %T", tx.Block(eggPos))
		}
		if len(data.Gateways) != len(killed.Gateways) {
			t.Fatal("active-fight restart changed gateway progress")
		}
	})
	removePlayer(w, player)
	closeWorld(w, db)
}

func TestDragonDyingFlightPrecedesSourceDeathAnimation(t *testing.T) {
	w := world.Config{Entities: entity.DefaultRegistry, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		tx.SetBlock(cube.Pos{0, 64, 0}, block.Bedrock{}, nil)
		attacker := tx.AddEntity((world.EntitySpawnOpts{}).New(player.Type, player.Config{Position: mgl64.Vec3{20.5, 70, .5}, GameMode: world.GameModeCreative}))
		handle := entity.NewEnderDragon(world.EntitySpawnOpts{Position: mgl64.Vec3{.5, 100, .5}})
		dragon := tx.AddEntity(handle).(*entity.EnderDragon)
		dragon.SetPhase(entity.DragonHoldingPattern)
		if _, hit := dragon.HurtPart("head", 1000, entity.AttackDamageSource{Attacker: attacker}); !hit {
			t.Fatal("player killing blow was rejected")
		}
		dragon.Tick(tx, 1)
		if dragon.Health() != 1 || dragon.Phase() != entity.DragonDying || dragon.Position()[1] >= 100 {
			t.Fatalf("DYING flight was skipped or movement was not applied: phase=%d health=%v pos=%v", dragon.Phase(), dragon.Health(), dragon.Position())
		}
		// Source's living phase terminates within squared distance 100;
		// only the following tick begins the separate 200-tick animation.
		dragon.Teleport(mgl64.Vec3{.5, 70, .5})
		dragon.SetVelocity(mgl64.Vec3{})
		dragon.Tick(tx, 2)
		if dragon.Health() != 0 || !dragon.Dead() {
			t.Fatalf("arrival at the podium did not end living DYING flight: health=%v", dragon.Health())
		}
		startY := dragon.Position()[1]
		for tick := int64(3); tick < 202; tick++ {
			dragon.Tick(tx, tick)
		}
		if _, loaded := handle.Entity(tx); !loaded {
			t.Fatal("dragon was removed before Source death tick 200")
		}
		wantY := startY + 199*float64(float32(.1))
		if delta := dragon.Position()[1] - wantY; delta < -1e-8 || delta > 1e-8 {
			t.Fatalf("death animation ascent=%v, Source widened 0.1F result=%v", dragon.Position()[1], wantY)
		}
		dragon.Tick(tx, 202)
		if _, loaded := handle.Entity(tx); loaded {
			t.Fatal("Source death tick 200 did not remove the registered dragon")
		}
	})
}
