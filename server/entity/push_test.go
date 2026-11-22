package entity

import (
	"math"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/internal/faultcheck"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func TestOverlappingMobsArePushedApart(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		a := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}})).(*Cow)
		b := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.3, 64, 0}})).(*Cow)
		drop := tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: mgl64.Vec3{0.1, 64, 0}}, item.NewStack(item.Stick{}, 1))).(*Ent)
		TickEntityCollisions(tx)
		if a.Velocity()[0] >= 0 || b.Velocity()[0] <= 0 {
			t.Fatalf("cows not pushed apart: a=%v b=%v", a.Velocity(), b.Velocity())
		}
		if drop.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("item entity was pushed: %v", drop.Velocity())
		}
	})
}

func TestCrammingDamagesPackedMobs(t *testing.T) {
	t.Parallel()
	for _, limit := range []int{24, 0} {
		w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
		<-w.Exec(func(tx *world.Tx) {
			if err := tx.World().SetGameRule("max_entity_cramming", limit); err != nil {
				t.Fatal(err)
			}
			cows := make([]*Cow, 0, 26)
			for i := range 26 {
				cows = append(cows, tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{float64(i) * 0.001, 64, 0}})).(*Cow))
			}
			for range 40 {
				TickEntityCollisions(tx)
			}
			hurt := 0
			for _, c := range cows {
				if c.Health() < c.MaxHealth() {
					hurt++
				}
			}
			if limit > 0 && hurt == 0 {
				t.Fatal("26 packed cows took no cramming damage with max_entity_cramming 24")
			}
			if limit == 0 && hurt != 0 {
				t.Fatalf("%d cows took cramming damage with max_entity_cramming 0", hurt)
			}
		})
		_ = w.Close()
	}
}

func TestPushAcrossChunkBoundaryAndExactVelocity(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		a := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{15.9, 64, 8}})).(*Cow)
		b := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{16.1, 64, 8.05}})).(*Cow)
		far := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{40, 64, 40}})).(*Cow)

		TickEntityCollisions(tx)

		// Compute reference Java Entity.push(Entity) applied symmetrically (a pushes b, then b pushes a).
		refDelta := func(p1, p2 mgl64.Vec3) (float64, float64) {
			xa, za := p2[0]-p1[0], p2[2]-p1[2]
			dd := math.Max(math.Abs(xa), math.Abs(za))
			dd = math.Sqrt(dd)
			xa, za = xa/dd, za/dd
			pow := min(1/dd, 1)
			return xa * pow * 0.05, za * pow * 0.05
		}
		xa1, za1 := refDelta(a.Position(), b.Position())
		xa2, za2 := refDelta(b.Position(), a.Position())
		wantA := mgl64.Vec3{-xa1, 0, -za1}.Add(mgl64.Vec3{xa2, 0, za2})
		wantB := mgl64.Vec3{xa1, 0, za1}.Add(mgl64.Vec3{-xa2, 0, -za2})

		if a.Velocity() != wantA {
			t.Fatalf("cow a velocity = %v, want %v", a.Velocity(), wantA)
		}
		if b.Velocity() != wantB {
			t.Fatalf("cow b velocity = %v, want %v", b.Velocity(), wantB)
		}
		if far.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("distant cow velocity = %v, want zero", far.Velocity())
		}
	})
}

func TestCrammingKillMidPassStopsSubsequentPushesAndCramming(t *testing.T) {
	t.Parallel()
	// Find a deterministic seed where the first randInt(r, 4) returns 0.
	var triggerSeed int64
	for seed := int64(1); seed < 1000; seed++ {
		r := vanilla.NewXoroshiroRandom(seed)
		if randInt(&r, 4) == 0 {
			triggerSeed = seed
			break
		}
	}
	if triggerSeed == 0 {
		t.Fatal("failed to find cramming trigger seed")
	}

	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().SetGameRule("max_entity_cramming", 2); err != nil {
			t.Fatal(err)
		}

		// Three overlapping cows with max_entity_cramming=2 (limit-1 = 1).
		// Cow 0 has 1 HP and will die immediately during its own pushEntities call.
		c0 := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.0, 64, 0}})).(*Cow)
		c0.SetMaxHealth(1)
		c1 := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.2, 64, 0}})).(*Cow)
		c2 := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.4, 64, 0}})).(*Cow)
		*randomOf(tx, c0) = vanilla.NewXoroshiroRandom(triggerSeed)
		*randomOf(tx, c1) = vanilla.NewXoroshiroRandom(triggerSeed)
		*randomOf(tx, c2) = vanilla.NewXoroshiroRandom(triggerSeed)

		TickEntityCollisions(tx)

		if !c0.Dead() {
			t.Fatalf("expected cow 0 to die from cramming, health=%v", c0.Health())
		}
		// Because cow 0 died mid-pass, c1 and c2 only see 1 living neighbour each (<= 1),
		// so neither c1 nor c2 should take cramming damage on this tick.
		if c1.Health() < c1.MaxHealth() || c2.Health() < c2.MaxHealth() {
			t.Fatalf("later cows should not take cramming damage after cow 0 died mid-pass: c1=%v/%v c2=%v/%v",
				c1.Health(), c1.MaxHealth(), c2.Health(), c2.MaxHealth())
		}
	})
}

func TestCrammingExcludesPassengersAndSameVehicleNoPush(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		if err := tx.World().SetGameRule("max_entity_cramming", 2); err != nil {
			t.Fatal(err)
		}
		victim := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}})).(*Cow)
		boats := make([]*Boat, 0, 5)
		riders := make([]*Cow, 0, 5)
		for i := range 5 {
			boat := tx.AddEntity(NewBoat(world.EntitySpawnOpts{Position: mgl64.Vec3{0.05 * float64(i+1), 64, 0}}, 0)).(*Boat)
			rider := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.05 * float64(i+1), 64, 0}})).(*Cow)
			if !boat.Mount(tx, rider) {
				t.Fatalf("failed to mount cow %d into boat", i)
			}
			boats = append(boats, boat)
			riders = append(riders, rider)
		}

		for range 40 {
			TickEntityCollisions(tx)
		}

		if victim.Health() < victim.MaxHealth() {
			t.Fatalf("victim took cramming damage from passengers: %v/%v", victim.Health(), victim.MaxHealth())
		}
		// Passengers must not receive push velocity themselves.
		for i, r := range riders {
			if r.Velocity() != (mgl64.Vec3{}) {
				t.Fatalf("passenger %d was pushed: %v", i, r.Velocity())
			}
		}
		// Non-passenger victim should still be pushed by overlapping passengers.
		if victim.Velocity() == (mgl64.Vec3{}) {
			t.Fatal("victim was not pushed by overlapping passengers")
		}
		_ = boats
	})
}

func TestPushSameVehicleDoesNotPushPigOrRider(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		pig := tx.AddEntity(NewPig(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}})).(*Pig)
		rider := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.2, 64, 0}})).(*Cow)
		if !pig.Mount(tx, rider) {
			t.Fatal("failed to mount cow on pig")
		}
		TickEntityCollisions(tx)
		if pig.Velocity() != (mgl64.Vec3{}) || rider.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("vehicle and its rider pushed each other: pig=%v rider=%v", pig.Velocity(), rider.Velocity())
		}
	})
}

type pushStubType struct {
	id string
}

func (t pushStubType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &pushStubEntity{
		Ent:   &Ent{tx: tx, handle: handle, data: data},
		id:    t.id,
		state: data.Data.(*pushStubState),
	}
}

func (t pushStubType) EncodeEntity() string                      { return t.id }
func (pushStubType) BBox(world.Entity) cube.BBox                 { return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3) }
func (pushStubType) DecodeNBT(map[string]any, *world.EntityData) {}
func (pushStubType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type pushStubState struct {
	dead       bool
	mode       world.GameMode
	hasMode    bool
	panicOnVel bool
}

func (*pushStubState) Tick(*Ent, *world.Tx) *Movement { return nil }

type pushStubConfig struct {
	state *pushStubState
}

func (c pushStubConfig) Apply(data *world.EntityData) { data.Data = c.state }

type pushStubEntity struct {
	*Ent
	id    string
	state *pushStubState
}

func (e *pushStubEntity) Health() float64                                  { return 20 }
func (e *pushStubEntity) MaxHealth() float64                               { return 20 }
func (e *pushStubEntity) SetMaxHealth(float64)                             {}
func (e *pushStubEntity) Dead() bool                                       { return e.state.dead }
func (e *pushStubEntity) Hurt(float64, world.DamageSource) (float64, bool) { return 0, false }
func (e *pushStubEntity) Heal(float64, world.HealingSource) float64        { return 0 }
func (e *pushStubEntity) KnockBack(mgl64.Vec3, float64, float64)           {}
func (e *pushStubEntity) AddEffect(effect.Effect)                          {}
func (e *pushStubEntity) RemoveEffect(effect.Type)                         {}
func (e *pushStubEntity) Effects() []effect.Effect                         { return nil }
func (e *pushStubEntity) Speed() float64                                   { return 0.1 }
func (e *pushStubEntity) SetSpeed(float64)                                 {}
func (e *pushStubEntity) Velocity() mgl64.Vec3 {
	if e.state.panicOnVel {
		e.state.panicOnVel = false
		panic(faultcheck.Expected{Why: "push fault isolation"})
	}
	return e.Ent.Velocity()
}

type pushPlayerStubType struct{}

func (pushPlayerStubType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &pushPlayerStub{pushStubEntity: &pushStubEntity{
		Ent:   &Ent{tx: tx, handle: handle, data: data},
		id:    "minecraft:player",
		state: data.Data.(*pushStubState),
	}}
}

func (pushPlayerStubType) EncodeEntity() string                        { return "minecraft:player" }
func (pushPlayerStubType) BBox(world.Entity) cube.BBox                 { return cube.Box(-0.3, 0, -0.3, 0.3, 1.8, 0.3) }
func (pushPlayerStubType) DecodeNBT(map[string]any, *world.EntityData) {}
func (pushPlayerStubType) EncodeNBT(*world.EntityData) map[string]any  { return nil }

type pushPlayerStub struct {
	*pushStubEntity
}

func (p *pushPlayerStub) GameMode() world.GameMode { return p.state.mode }

func TestPushableVehiclesArmourStandAndSpectator(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		opts := world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}
		pushableEntities := []struct {
			name string
			e    world.Entity
		}{
			{"boat", tx.AddEntity(NewBoat(opts, 0))},
			{"chest_boat", tx.AddEntity(NewChestBoat(opts, 0))},
			{"minecart", tx.AddEntity(opts.New(MinecartType, minecartConf))},
			{"chest_minecart", tx.AddEntity(opts.New(ChestMinecartType, chestMinecartConf))},
			{"hopper_minecart", tx.AddEntity(opts.New(HopperMinecartType, hopperMinecartConf))},
			{"tnt_minecart", tx.AddEntity(opts.New(TNTMinecartType, tntMinecartConf))},
			{"command_block_minecart", tx.AddEntity(NewCommandBlockMinecart(opts, CommandBlockMinecartConfig{}))},
			{"live_cow", tx.AddEntity(NewCow(opts))},
			{"survival_player", tx.AddEntity(opts.New(pushPlayerStubType{}, pushStubConfig{state: &pushStubState{mode: world.GameModeSurvival}}))},
		}
		for _, tc := range pushableEntities {
			if !pushable(tc.e) {
				t.Errorf("pushable(%s) = false, want true", tc.name)
			}
		}

		deadCow := tx.AddEntity(NewCow(opts)).(*Cow)
		deadCow.Hurt(100, CrammingDamageSource{})

		notPushable := []struct {
			name string
			e    world.Entity
		}{
			{"armour_stand", tx.AddEntity(NewArmourStand(opts))},
			{"item_drop", tx.AddEntity(NewItem(opts, item.NewStack(item.Stick{}, 1)))},
			{"dead_cow", deadCow},
			{"spectator_player", tx.AddEntity(opts.New(pushPlayerStubType{}, pushStubConfig{state: &pushStubState{mode: world.GameModeSpectator}}))},
			{"living_armor_stand_stub", tx.AddEntity(opts.New(pushStubType{id: "minecraft:armor_stand"}, pushStubConfig{state: &pushStubState{}}))},
		}
		for _, tc := range notPushable {
			if pushable(tc.e) {
				t.Errorf("pushable(%s) = true, want false", tc.name)
			}
		}
	})
}

func TestPushSpectatorIgnoredAndPlayerPushesMobOnly(t *testing.T) {
	t.Parallel()
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		cowSpec := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}})).(*Cow)
		spec := tx.AddEntity((world.EntitySpawnOpts{Position: mgl64.Vec3{0.2, 64, 0}}).New(
			pushPlayerStubType{}, pushStubConfig{state: &pushStubState{mode: world.GameModeSpectator}},
		)).(*pushPlayerStub)

		cowSurv := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{32, 64, 0}})).(*Cow)
		surv := tx.AddEntity((world.EntitySpawnOpts{Position: mgl64.Vec3{32.2, 64, 0}}).New(
			pushPlayerStubType{}, pushStubConfig{state: &pushStubState{mode: world.GameModeSurvival}},
		)).(*pushPlayerStub)

		cowStand := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{64, 64, 0}})).(*Cow)
		tx.AddEntity(NewArmourStand(world.EntitySpawnOpts{Position: mgl64.Vec3{64.2, 64, 0}}))

		TickEntityCollisions(tx)

		if cowSpec.Velocity() != (mgl64.Vec3{}) || spec.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("spectator interacted with cow: cow=%v spec=%v", cowSpec.Velocity(), spec.Velocity())
		}
		if cowSurv.Velocity()[0] >= 0 {
			t.Fatalf("survival player did not push cow away: cow=%v", cowSurv.Velocity())
		}
		if surv.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("survival player was moved server-side: %v", surv.Velocity())
		}
		if cowStand.Velocity() != (mgl64.Vec3{}) {
			t.Fatalf("armour stand pushed cow: %v", cowStand.Velocity())
		}
	})
}

func TestTickEntityCollisionsFaultIsolation(t *testing.T) {
	t.Parallel()
	var faults []world.Fault
	w := world.Config{
		Synchronous:     true,
		RandomTickSpeed: -1,
		Entities:        DefaultRegistry,
		Fault:           func(f world.Fault) { faults = append(faults, f) },
	}.New()
	defer w.Close()
	<-w.Exec(func(tx *world.Tx) {
		tx.AddEntity((world.EntitySpawnOpts{Position: mgl64.Vec3{0, 64, 0}}).New(
			pushStubType{id: "test:panicker"}, pushStubConfig{state: &pushStubState{panicOnVel: true}},
		))
		tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0.2, 64, 0}}))

		// Separate cluster later in the same pass must still be pushed despite the earlier panic.
		a := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{32, 64, 0}})).(*Cow)
		b := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{32.2, 64, 0}})).(*Cow)

		TickEntityCollisions(tx)

		if len(faults) != 1 || faults[0].Type != "test:panicker" {
			t.Fatalf("faults = %+v, want 1 fault blamed on test:panicker", faults)
		}
		if a.Velocity()[0] >= 0 || b.Velocity()[0] <= 0 {
			t.Fatalf("later cows were not pushed after earlier entity panicked: a=%v b=%v", a.Velocity(), b.Velocity())
		}
	})
}

func TestTickEntityCollisionsConcurrentWorldsAndPoolReset(t *testing.T) {
	t.Parallel()
	const worlds = 8
	var wg sync.WaitGroup
	wg.Add(worlds)
	for id := range worlds {
		go func(id int) {
			defer wg.Done()
			w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
			defer w.Close()
			<-w.Exec(func(tx *world.Tx) {
				cows := make([]*Cow, 0, 20)
				for i := range 20 {
					c := tx.AddEntity(NewCow(world.EntitySpawnOpts{
						Position: mgl64.Vec3{float64(i%5)*0.15 + float64(id), 64, float64(i/5) * 0.15},
					})).(*Cow)
					cows = append(cows, c)
				}
				for range 30 {
					TickEntityCollisions(tx)
				}
				moved := 0
				for _, c := range cows {
					if c.Velocity() != (mgl64.Vec3{}) {
						moved++
					}
				}
				if moved == 0 {
					t.Errorf("world %d: no cows moved", id)
				}
			})
		}(id)
	}
	wg.Wait()

	// Verify pooled scratch clears all entity/transaction pointers before reuse.
	s := pushScratchPool.Get().(*pushScratch)
	defer pushScratchPool.Put(s)
	if len(s.meta) != 0 || len(s.spatial) != 0 || len(s.cells) != 0 || s.actTx != nil {
		t.Fatalf("pooled scratch not reset: meta=%d spatial=%d cells=%d actTx=%v",
			len(s.meta), len(s.spatial), len(s.cells), s.actTx)
	}
	backing := s.meta[:cap(s.meta)]
	for i, m := range backing {
		if m.e != nil || m.l != nil || m.gm != nil || m.root != nil {
			t.Fatalf("pooled scratch leaked pointer at meta[%d]: %+v", i, m)
		}
	}
}
