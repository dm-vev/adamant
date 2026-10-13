package block

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
)

func TestCreakingHeartActivationRequiresAlignedPaleOak(t *testing.T) {
	for _, axis := range cube.Axes() {
		w := world.Config{Synchronous: true}.New()
		w.SetTime(12600)
		w.SetSpawnMobs(false)
		<-w.Exec(func(tx *world.Tx) {
			pos := cube.Pos{0, 64, 0}
			offset := cube.PosFromVec3(axis.Vec3())
			h := NewCreakingHeart()
			h.Axis = axis
			tx.SetBlock(pos, h, nil)
			tx.SetBlock(pos.Add(offset), Log{Wood: PaleOakWood(), Axis: axis}, nil)
			tx.SetBlock(pos.Sub(offset), Wood{Wood: OakWood(), Axis: axis}, nil)
			h.ScheduledTick(pos, tx, nil)
			if got := tx.Block(pos).(CreakingHeart).State; got != CreakingHeartUprooted {
				t.Fatalf("axis %v: oak activated heart: %v", axis, got)
			}
			tx.SetBlock(pos.Sub(offset), Wood{Wood: PaleOakWood(), Axis: cube.Axis((int(axis) + 1) % 3)}, nil)
			h.ScheduledTick(pos, tx, nil)
			if got := tx.Block(pos).(CreakingHeart).State; got != CreakingHeartUprooted {
				t.Fatalf("axis %v: misaligned wood activated heart: %v", axis, got)
			}
			tx.SetBlock(pos.Sub(offset), Wood{Wood: PaleOakWood(), Axis: axis, Stripped: true}, nil)
			h.ScheduledTick(pos, tx, nil)
			h = tx.Block(pos).(CreakingHeart)
			if h.State != CreakingHeartAwake {
				t.Fatalf("axis %v: aligned logs did not activate heart: %v", axis, h.State)
			}
			tx.SetBlock(pos.Add(offset), nil, nil)
			h.ScheduledTick(pos, tx, nil)
			if tx.Block(pos).(CreakingHeart).State != CreakingHeartAwake {
				t.Fatal("scheduled activation tick prematurely uprooted heart")
			}
			h.Tick(0, pos, tx)
			if tx.Block(pos).(CreakingHeart).State != CreakingHeartAwake {
				t.Fatal("ticker decremented zero before testing it")
			}
			h.Tick(1, pos, tx)
			if tx.Block(pos).(CreakingHeart).State != CreakingHeartUprooted {
				t.Fatal("unlinked heart retained activation without supporting logs")
			}
		})
		w.Close()
	}
}

func TestCreakingHeartUnresolvedLinkGraceAfterReload(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	w.SetSpawnMobs(false)
	<-w.Exec(func(tx *world.Tx) {
		pos := cube.Pos{0, 64, 0}
		h := NewCreakingHeart()
		h.State = CreakingHeartDormant
		h.data.hasProtector = true
		h.data.protectorUUID = uuid.MustParse("67491480-7a42-4168-980a-c6906b09f268")
		h.data.protectorID = 1
		h.data.ticker, h.data.emitter, h.data.output = 24, 100, 15
		h = h.DecodeNBT(h.EncodeNBT()).(CreakingHeart)
		if h.data.ticker != 0 || h.data.emitter != 0 || h.ComparatorOutput(tx, pos) != 0 {
			t.Fatal("reload persisted Java transient state")
		}
		tx.SetBlock(pos, h, nil)
		for tick := range 29 {
			h.Tick(int64(tick), pos, tx)
		}
		if !h.data.hasProtector {
			t.Fatal("unresolved protector link expired before 30 ticks")
		}
		h.Tick(29, pos, tx)
		if h.data.hasProtector {
			t.Fatal("unresolved protector link survived 30-tick lookup grace")
		}
	})
}

func TestCreakingResinPreservesSourceWater(t *testing.T) {
	w := world.Config{Synchronous: true}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		*tx.JavaRandom() = java26.NewLegacyRandom(1729)
		origin := cube.Pos{0, 64, 0}
		log := origin.Side(cube.FaceEast)
		water := log.Side(cube.FaceUp)
		for _, face := range []cube.Face{cube.FaceDown, cube.FaceUp, cube.FaceNorth, cube.FaceSouth, cube.FaceWest, cube.FaceEast} {
			tx.SetBlock(log.Side(face), Stone{}, nil)
		}
		tx.SetBlock(origin, NewCreakingHeart(), nil)
		tx.SetBlock(log, Log{Wood: PaleOakWood(), Axis: cube.Y}, nil)
		tx.SetBlock(water, Water{Depth: 8}, nil)
		pos, ok := (CreakingHeart{}).spreadResin(origin, tx)
		if !ok || pos != water {
			t.Fatalf("resin placement = %v, %v; want source water %v", pos, ok, water)
		}
		resin, ok := tx.Block(water).(ResinClump)
		if !ok || resin.Faces != MultiFace(0).With(cube.FaceDown, true) {
			t.Fatalf("resin did not attach toward log: %#v", tx.Block(water))
		}
		liquid, ok := tx.Liquid(water)
		if !ok || liquid.LiquidDepth() != 8 || liquid.LiquidFalling() {
			t.Fatal("resin displaced source water")
		}
		if _, ok := (CreakingHeart{}).spreadResin(origin, tx); ok {
			t.Fatal("resin counted its existing attachment as a new clump")
		}
	})
}
