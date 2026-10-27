package world

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
)

func TestVibrationSelectionSurvivesReload(t *testing.T) {
	var data VibrationData
	data.Selector.AddCandidate(VibrationInfo{Event: GameEventExplode, Distance: 5}, 10)
	data.Selector.AddCandidate(VibrationInfo{Event: GameEventStep, Distance: 3}, 10)
	data.Selector.AddCandidate(VibrationInfo{Event: GameEventProjectileLand, Distance: 3}, 10)
	data.Selector.AddCandidate(VibrationInfo{Event: GameEventBounce, Distance: 3}, 10)
	data.Selector.AddCandidate(VibrationInfo{Event: GameEventStep, Distance: 1}, 11)
	data = reloadVibrationForTest(t, data)
	if _, ok := data.Selector.Chosen(10); ok {
		t.Fatal("candidate became eligible during its collection tick")
	}
	chosen, ok := data.Selector.Chosen(11)
	if !ok || chosen.Event != GameEventProjectileLand {
		t.Fatalf("selected %v, present=%v; nearest, then highest frequency, then first equal candidate must win", chosen.Event, ok)
	}
	data.Selector.StartOver()
	data.Selector.AddCandidate(VibrationInfo{Event: GameEventStep, Distance: 1}, 11)
	if chosen, ok = data.Selector.Chosen(12); !ok || chosen.Event != GameEventStep {
		t.Fatalf("cleared selector retained an earlier collection: %v, present=%v", chosen.Event, ok)
	}
}

func TestVibrationOcclusionRequiresAllSixRays(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	wool, ok := w.conf.Blocks.BlockByName("minecraft:white_wool", nil)
	if !ok {
		t.Fatal("native palette has no white wool")
	}
	<-w.Exec(func(tx *Tx) {
		origin, destination := mgl64.Vec3{0.5, 64.5, 0.5}, mgl64.Vec3{4.5, 64.5, 4.5}
		a, b := cube.Pos{2, 64, 1}, cube.Pos{1, 64, 2}
		tx.SetBlock(a, wool, nil)
		if vibrationOccluded(tx, origin, destination) {
			t.Fatal("wool touching only one side of a diagonal seam blocked every vibration ray")
		}
		tx.SetBlock(b, wool, nil)
		if !vibrationOccluded(tx, origin, destination) {
			t.Fatal("two wool blocks enclosing the diagonal seam left an unoccluded ray")
		}
		tx.SetBlock(a, nil, nil)
		tx.SetBlock(b, nil, nil)
		tx.SetBlock(cube.Pos{2, 64, 2}, wool, nil)
		if !vibrationOccluded(tx, origin, destination) {
			t.Fatal("wool through the centre of the path did not block the vibration")
		}
	})
}

func TestVibrationReloadWaitsForLoadedNeighbours(t *testing.T) {
	w := Config{Synchronous: true}.New()
	defer w.Close()
	pos := cube.Pos{160, 64, 160}
	origin := mgl64.Vec3{157.75, 64.5, 160.5}
	sourceID := uuid.MustParse("46f49526-a516-4dad-a17e-5d89f2b7a03d")
	ownerID := uuid.MustParse("9c85c51c-b27f-449c-a5a5-c3917a2e5a92")
	source := EntitySpawnOpts{ID: sourceID, Position: origin}.New(testEntityType{}, testEntityConfig{})
	owner := EntitySpawnOpts{ID: ownerID, Position: pos.Vec3Centre()}.New(testEntityType{}, testEntityConfig{})
	data := reloadVibrationForTest(t, VibrationData{
		HasCurrent: true, TravelTime: 2,
		Current: VibrationInfo{Event: GameEventProjectileLand, Distance: 2.75, Position: origin, SourceID: sourceID, ProjectileOwnerID: ownerID},
	})
	received := 0
	user := &vibrationTestUser{receive: func(tx *Tx, info VibrationInfo, distance float32) {
		received++
		if distance != data.Current.Distance {
			t.Errorf("receiving distance = %v, want stored distance %v", distance, data.Current.Distance)
		}
		if _, ok := info.SourceEntity(tx); ok {
			t.Error("removed projectile resolved as a loaded entity")
		}
		if entity, ok := info.ProjectileOwner(tx); !ok || entity.H() != owner {
			t.Error("saved vibration lost the projectile owner after projectile removal")
		}
	}}
	<-w.Exec(func(tx *Tx) {
		tx.AddEntity(owner)
		entity := tx.AddEntity(source)
		tx.RemoveEntity(entity)
		w.TickVibrationSystem(tx, pos, user, &data)
		if data.TravelTime != 1 || received != 0 {
			t.Fatalf("first resumed tick: delay=%d, receptions=%d", data.TravelTime, received)
		}
		w.TickVibrationSystem(tx, pos, user, &data)
		w.TickVibrationSystem(tx, pos, user, &data)
		if !data.HasCurrent || data.TravelTime != 0 || received != 0 {
			t.Fatalf("arrival without neighbours: pending=%v, delay=%d, receptions=%d", data.HasCurrent, data.TravelTime, received)
		}
		for x := 9; x <= 11; x++ {
			for z := 9; z <= 11; z++ {
				tx.Block(cube.Pos{x * 16, 64, z * 16})
			}
		}
		w.TickVibrationSystem(tx, pos, user, &data)
		w.TickVibrationSystem(tx, pos, user, &data)
		if data.HasCurrent || received != 1 {
			t.Fatalf("loaded arrival: pending=%v, receptions=%d, want one delivery", data.HasCurrent, received)
		}
	})
}

func reloadVibrationForTest(t *testing.T, data VibrationData) VibrationData {
	t.Helper()
	encoded, err := nbt.MarshalEncoding(data.EncodeNBT(), nbt.LittleEndian)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := nbt.UnmarshalEncoding(encoded, &saved, nbt.LittleEndian); err != nil {
		t.Fatal(err)
	}
	return DecodeVibrationData(saved)
}

type vibrationTestUser struct {
	receive func(*Tx, VibrationInfo, float32)
}

func (*vibrationTestUser) VibrationRadius() int { return 8 }
func (*vibrationTestUser) CanReceiveVibration(*Tx, cube.Pos, cube.Pos, GameEvent, GameEventContext) bool {
	return true
}
func (v *vibrationTestUser) ReceiveVibration(tx *Tx, _, _ cube.Pos, info VibrationInfo, distance float32) {
	v.receive(tx, info, distance)
}
func (*vibrationTestUser) RequiresAdjacentChunksToBeTicking() bool { return true }
func (*vibrationTestUser) VibrationParticle(mgl64.Vec3, int) Particle {
	panic("particle constructed without any observing players")
}
