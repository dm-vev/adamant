package entity

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/portal"
	"github.com/go-gl/mathgl/mgl64"
)

func TestPortalTravelComputerStopPortalContact(t *testing.T) {
	t.Run("keeps timer after portal contact", func(t *testing.T) {
		tc := &PortalTravelComputer{inside: true, awaitingTravel: true, start: time.Now()}
		tc.StopPortalContact()
		if !tc.awaitingTravel {
			t.Fatal("StopPortalContact() reset travel timer after portal contact")
		}
		if tc.inside {
			t.Fatal("StopPortalContact() did not clear portal contact for the next tick")
		}
	})

	t.Run("resets timer without portal contact", func(t *testing.T) {
		tc := &PortalTravelComputer{awaitingTravel: true, start: time.Now()}
		tc.StopPortalContact()
		if tc.awaitingTravel {
			t.Fatal("StopPortalContact() kept travel timer without portal contact")
		}
	})
}

func TestEntDisplace(t *testing.T) {
	w := world.New()
	t.Cleanup(func() { _ = w.Close() })
	origin := mgl64.Vec3{1, 2, 3}
	handle := world.EntitySpawnOpts{Position: origin}.New(testMovingEntType{}, testMoveConfig{})
	mustDo(t, w, func(tx *world.Tx) {
		e := tx.AddEntity(handle).(*Ent)
		e.Displace(mgl64.Vec3{0.25, 0.5, -0.25})
		if got, want := e.Position(), (mgl64.Vec3{1.25, 2.5, 2.75}); !got.ApproxEqual(want) {
			t.Fatalf("Displace() position = %v, want %v", got, want)
		}
	})
}

func TestEntProjectileTravelsThroughPortal(t *testing.T) {
	var overworld, nether *world.World
	overworld = world.Config{PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return nether
		}
		return nil
	}}.New()
	nether = world.Config{Dim: world.Nether, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
	})

	spawnRecorder := &entitySpawnRecorder{}
	nether.Handle(spawnRecorder)

	sourcePos := mgl64.Vec3{80.5, 64, 80.5}
	targetPortal := cube.Pos{10, 64, 10}
	mustDo(t, nether, func(tx *world.Tx) {
		buildActivePortal(tx, targetPortal)
	})

	handle := world.EntitySpawnOpts{Position: sourcePos}.New(EnderPearlType, enderPearlConf)
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		(block.Portal{Axis: cube.Z}).EntityInside(cube.PosFromVec3(sourcePos), tx, e)
		if _, ok := handle.Entity(tx); !ok {
			t.Fatal("non-terminal portal contact removed entity before the source transaction finished")
		}
	})

	waitForEntityWorld(t, handle, nether)
	if entityInWorld(handle, overworld) {
		t.Fatal("entity remained in the source world after portal travel")
	}
	if !spawnRecorder.called {
		t.Fatal("destination world did not fire an entity spawn event")
	}

	mustDo(t, nether, func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("entity was not added to the Nether")
		}
		if got, want := cube.PosFromVec3(e.Position()), targetPortal; got != want {
			t.Fatalf("entity position after portal travel = %v, want %v", got, want)
		}
		ent, ok := e.(*Ent)
		if !ok {
			t.Fatalf("entity after portal travel has type %T, want *Ent", e)
		}
		projectile, ok := ent.Behaviour().(*ProjectileBehaviour)
		if !ok {
			t.Fatalf("entity behaviour after portal travel has type %T, want *ProjectileBehaviour", ent.Behaviour())
		}
		if !projectile.PortalTravel() {
			t.Fatal("projectile portal travel state was not preserved")
		}
	})
}

func TestEntTravelsThroughPortalOnTick(t *testing.T) {
	var overworld, nether *world.World
	overworld = world.Config{PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return nether
		}
		return nil
	}}.New()
	nether = world.Config{Dim: world.Nether, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
	})

	sourcePortal, targetPortal := cube.Pos{80, 64, 80}, cube.Pos{10, 64, 10}
	mustDo(t, overworld, func(tx *world.Tx) {
		buildActivePortal(tx, sourcePortal)
	})
	mustDo(t, nether, func(tx *world.Tx) {
		buildActivePortal(tx, targetPortal)
	})

	releasedViewers := false
	handle := world.EntitySpawnOpts{Position: sourcePortal.Vec3Middle().Sub(mgl64.Vec3{1})}.New(testMovingEntType{}, testMoveConfig{
		delta:   mgl64.Vec3{1},
		release: func() { releasedViewers = true },
	})
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		ticker, ok := e.(world.TickerEntity)
		if !ok {
			t.Fatalf("entity has type %T, want world.TickerEntity", e)
		}
		ticker.Tick(tx, 1)
	})
	if !releasedViewers {
		t.Fatal("terminal portal travel did not release movement viewers")
	}

	waitForEntityWorld(t, handle, nether)
	if entityInWorld(handle, overworld) {
		t.Fatal("entity remained in the source world after tick-driven portal travel")
	}
	mustDo(t, nether, func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("entity was not added to the Nether")
		}
		if got, want := cube.PosFromVec3(e.Position()), targetPortal; got != want {
			t.Fatalf("entity position after tick-driven portal travel = %v, want %v", got, want)
		}
		if got := e.(*Ent).Age(); got != 0 {
			t.Fatalf("entity age after terminal portal travel tick = %v, want 0", got)
		}
	})
}

func TestRegisteredCowTravelsThroughEndPortal(t *testing.T) {
	var overworld, end *world.World
	overworld = world.Config{PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.End {
			return end
		}
		return nil
	}}.New()
	end = world.Config{Dim: world.End, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.End {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = end.Close()
	})

	origin := mgl64.Vec3{50.5, 64, 50.5}
	handle := world.EntitySpawnOpts{Position: origin, Rotation: cube.Rotation{12, 4}, Velocity: mgl64.Vec3{0.3, 0.4, 0.5}}.New(CowType, CowConfig{})
	spawned := make(chan struct{})
	end.Handle(entitySpawnSignal{handle: handle, spawned: spawned})
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		(block.EndPortal{}).EntityInside(cube.PosFromVec3(origin), tx, e)
	})
	select {
	case <-spawned:
	case <-time.After(5 * time.Second):
		t.Fatal("cow was never published in the End world")
	}
	waitForEntityWorld(t, handle, end)
	mustDo(t, end, func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("registered cow was not added to the End")
		}
		want := mgl64.Vec3{100.5, 50, 0.5}
		if got := e.Position(); !got.ApproxEqual(want) {
			t.Fatalf("registered cow position after End travel = %v, want %v", got, want)
		}
		if got := e.Rotation().Yaw(); got != 90 {
			t.Fatalf("registered cow yaw after End travel = %v, want 90", got)
		}
		if got := e.(interface{ Velocity() mgl64.Vec3 }).Velocity(); !got.ApproxEqual(mgl64.Vec3{0.3, 0.4, 0.5}) {
			t.Fatalf("registered cow velocity after End travel = %v, want original velocity", got)
		}
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				p := cube.Pos{100 + dx, 48, dz}
				if _, ok := tx.Block(p).(block.Obsidian); !ok {
					t.Fatalf("obsidian platform missing at %v: got %T", p, tx.Block(p))
				}
			}
		}
	})
}

func TestEndArrivalDestinationRegeneratesPlatformForEntityKinds(t *testing.T) {
	w := world.Config{Dim: world.End, Synchronous: true}.New()
	t.Cleanup(func() { _ = w.Close() })
	tc := &PortalTravelComputer{}

	mustDo(t, w, func(tx *world.Tx) {
		for _, player := range []bool{false, true} {
			tc.Player = player
			want := mgl64.Vec3{100.5, 50, 0.5}
			if player {
				want[1] = 49
			}
			tx.SetBlock(cube.Pos{100, 50, 0}, block.Obsidian{}, nil)
			if got, ok := tc.destinationSpawn(tx, world.Overworld, cube.Pos{}); !ok || !got.ApproxEqual(want) {
				t.Fatalf("destinationSpawn(player=%t) = %v, %v; want %v, true", player, got, ok, want)
			}
		}
		if _, ok := tx.Block(cube.Pos{100, 48, 0}).(block.Obsidian); !ok {
			t.Fatal("End ingress did not regenerate the platform floor")
		}
		if _, ok := tx.Block(cube.Pos{100, 50, 0}).(block.Air); !ok {
			t.Fatal("repeated End ingress did not clear the platform volume")
		}
	})
}

func TestEndReturnSpawnSelection(t *testing.T) {
	t.Run("overworld uses configured spawn point", func(t *testing.T) {
		w := world.New()
		t.Cleanup(func() { _ = w.Close() })
		want := mgl64.Vec3{12.5, 70, -3.5}
		tc := &PortalTravelComputer{SpawnPoint: func(*world.Tx) mgl64.Vec3 { return want }}

		mustDo(t, w, func(tx *world.Tx) {
			got, ok := tc.destinationSpawn(tx, world.End, cube.Pos{})
			if !ok || !got.ApproxEqual(want) {
				t.Fatalf("destinationSpawn() = %v, %v, want %v, true", got, ok, want)
			}
		})
	})

	t.Run("overworld falls back to world spawn", func(t *testing.T) {
		w := world.New()
		t.Cleanup(func() { _ = w.Close() })
		tc := &PortalTravelComputer{}

		mustDo(t, w, func(tx *world.Tx) {
			want := tx.World().Spawn().Vec3Middle()
			got, ok := tc.destinationSpawn(tx, world.End, cube.Pos{})
			if !ok || !got.ApproxEqual(want) {
				t.Fatalf("destinationSpawn() = %v, %v, want %v, true", got, ok, want)
			}
		})
	})

	t.Run("non-overworld default uses world spawn", func(t *testing.T) {
		w := world.Config{Dim: world.Nether}.New()
		t.Cleanup(func() { _ = w.Close() })
		tc := &PortalTravelComputer{}

		mustDo(t, w, func(tx *world.Tx) {
			want := tx.World().Spawn().Vec3Middle()
			got, ok := tc.destinationSpawn(tx, world.End, cube.Pos{})
			if !ok || !got.ApproxEqual(want) {
				t.Fatalf("destinationSpawn() = %v, %v, want %v, true", got, ok, want)
			}
		})
	})
}

func TestEntReturnsFromEndToDefaultWorldSpawn(t *testing.T) {
	defaultWorld := world.Config{Dim: world.Nether}.New()
	defaultWorld.SetSpawn(cube.Pos{23, 70, -9})
	var end *world.World
	end = world.Config{
		Dim: world.End,
		PortalDestination: func(world.Dimension) *world.World {
			return end
		},
		DefaultWorld: func() *world.World { return defaultWorld },
	}.New()
	t.Cleanup(func() {
		_ = end.Close()
		_ = defaultWorld.Close()
	})

	origin := mgl64.Vec3{5.5, 64, 5.5}
	handle := world.EntitySpawnOpts{Position: origin}.New(testMovingEntType{}, testMoveConfig{})
	spawned := make(chan struct{})
	defaultWorld.Handle(entitySpawnSignal{handle: handle, spawned: spawned})
	mustDo(t, end, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		(block.EndPortal{}).EntityInside(cube.PosFromVec3(origin), tx, e)
	})
	select {
	case <-spawned:
	case <-time.After(5 * time.Second):
		t.Fatal("entity was never published in the default world")
	}
	waitForEntityWorld(t, handle, defaultWorld)
	mustDo(t, defaultWorld, func(tx *world.Tx) {
		e, ok := handle.Entity(tx)
		if !ok {
			t.Fatal("entity was not added to the configured default world")
		}
		want := defaultWorld.Spawn().Vec3Middle()
		if got := e.Position(); !got.ApproxEqual(want) {
			t.Fatalf("End return position = %v, want default world spawn %v", got, want)
		}
		if _, ok := portal.FindNetherPortal(tx, cube.PosFromVec3(want), 16); ok {
			t.Fatal("End return unexpectedly searched for or created a Nether portal")
		}
	})
}

func TestTranslatePortalPosition(t *testing.T) {
	tests := []struct {
		name           string
		pos, want      cube.Pos
		source, target world.Dimension
	}{
		{name: "overworld to nether", pos: cube.Pos{80, 64, 81}, want: cube.Pos{10, 64, 10}, source: world.Overworld, target: world.Nether},
		{name: "negative coordinates floor towards negative infinity", pos: cube.Pos{-15, 64, -1}, want: cube.Pos{-2, 64, -1}, source: world.Overworld, target: world.Nether},
		{name: "nether to overworld", pos: cube.Pos{10, 64, -3}, want: cube.Pos{80, 64, -24}, source: world.Nether, target: world.Overworld},
		{name: "y clamped to nether range", pos: cube.Pos{0, 319, 0}, want: cube.Pos{0, 127, 0}, source: world.Overworld, target: world.Nether},
		{name: "y clamped to overworld range", pos: cube.Pos{0, -80, 0}, want: cube.Pos{0, -64, 0}, source: world.Nether, target: world.Overworld},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := translatePortalPosition(tt.pos, tt.source, tt.target); got != tt.want {
				t.Fatalf("translatePortalPosition(%v, %v, %v) = %v, want %v", tt.pos, tt.source, tt.target, got, tt.want)
			}
		})
	}
}

func TestPortalTravelComputerDelayedTravel(t *testing.T) {
	overworld, nether := portalWorlds(t)
	_ = nether

	tc := &PortalTravelComputer{Delay: func(*world.Tx, world.Dimension) time.Duration { return time.Second * 4 }}
	mustDo(t, overworld, func(tx *world.Tx) {
		if destination := tc.enterPortal(tx, world.Nether); destination != nil {
			t.Fatal("enterPortal() started travel before the portal timer finished")
		}
		if !tc.awaitingTravel {
			t.Fatal("enterPortal() did not start the portal timer")
		}

		// Backdate the timer to simulate the entity having stood in the portal for four seconds.
		tc.start = time.Now().Add(-time.Second * 4)
		if destination := tc.enterPortal(tx, world.Nether); destination != nether {
			t.Fatalf("enterPortal() destination after portal timer = %v, want the Nether", destination)
		}
	})
}

func TestPortalTravelHonoursAllowEnteringNether(t *testing.T) {
	overworld, nether := portalWorlds(t)
	tc := NewPortalTravelComputer()
	mustDo(t, overworld, func(tx *world.Tx) {
		if err := tx.World().SetGameRule("allow_entering_nether_using_portals", false); err != nil {
			t.Fatal(err)
		}
		if destination := tc.enterPortal(tx, world.Nether); destination != nil {
			t.Fatal("portal entered the Nether with allow_entering_nether_using_portals off")
		}
		if err := tx.World().SetGameRule("allow_entering_nether_using_portals", true); err != nil {
			t.Fatal(err)
		}
		if destination := tc.enterPortal(tx, world.Nether); destination != nether {
			t.Fatal("portal did not enter the Nether with the gamerule back on")
		}
	})
}

func TestPortalTravelComputerCooldown(t *testing.T) {
	overworld, nether := portalWorlds(t)
	_ = nether

	tc := NewPortalTravelComputer()
	mustDo(t, overworld, func(tx *world.Tx) {
		tc.cooldownUntil = time.Now().Add(time.Hour)
		if destination := tc.enterPortal(tx, world.Nether); destination != nil {
			t.Fatal("enterPortal() started travel during the portal cooldown")
		}

		tc.cooldownUntil = time.Now().Add(-time.Second)
		if destination := tc.enterPortal(tx, world.Nether); destination != nether {
			t.Fatal("enterPortal() did not start travel after the portal cooldown expired")
		}
	})
}

func TestEntPortalTravelWithoutDestinationPortal(t *testing.T) {
	overworld, nether := portalWorlds(t)

	spawnRecorder := &entitySpawnRecorder{}
	nether.Handle(spawnRecorder)

	sourcePos := mgl64.Vec3{80.5, 64, 80.5}
	handle := world.EntitySpawnOpts{Position: sourcePos}.New(EnderPearlType, enderPearlConf)
	var tc *PortalTravelComputer
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		tc = e.(*Ent).Behaviour().(*ProjectileBehaviour).PortalTravelComputer()
		(block.Portal{Axis: cube.Z}).EntityInside(cube.PosFromVec3(sourcePos), tx, e)
	})

	// The cooldown is stamped once the travel attempt finishes, after the entity was returned to the source world.
	deadline := time.Now().Add(10 * time.Second)
	for {
		tc.mu.Lock()
		done := !tc.cooldownUntil.IsZero()
		tc.mu.Unlock()
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the travel attempt to finish")
		}
		time.Sleep(10 * time.Millisecond)
	}

	if !entityInWorld(handle, overworld) {
		t.Fatal("entity did not return to the source world after failing to find a destination portal")
	}
	if spawnRecorder.called {
		t.Fatal("entity was spawned in the destination world without a linked portal")
	}
	mustDo(t, overworld, func(tx *world.Tx) {
		e, _ := handle.Entity(tx)
		if got := e.Position(); !got.ApproxEqual(sourcePos) {
			t.Fatalf("entity position after failed portal travel = %v, want %v", got, sourcePos)
		}
	})
	mustDo(t, nether, func(tx *world.Tx) {
		if _, ok := portal.FindNetherPortal(tx, cube.Pos{10, 64, 10}, 16); ok {
			t.Fatal("a portal was created in the destination world by a non-player entity")
		}
	})
}

func TestPortalTravelClosesHandleWhenBothWorldsClose(t *testing.T) {
	source := world.Config{Synchronous: true}.New()
	destination := world.Config{Dim: world.Nether, Synchronous: true}.New()

	origin := mgl64.Vec3{80.5, 64, 80.5}
	handle := world.EntitySpawnOpts{Position: origin}.New(EnderPearlType, enderPearlConf)
	mustDo(t, source, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		if removed := tx.RemoveEntity(e); removed != handle {
			t.Fatal("RemoveEntity() did not return the entity handle")
		}
	})
	if err := destination.Close(); err != nil {
		t.Fatalf("close destination world: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source world: %v", err)
	}

	tc := NewPortalTravelComputer()
	tc.transfer(handle, source, destination, portalTravelOrigin{position: origin}, cube.Pos{10, 64, 10}, world.Overworld, world.Nether)

	if !handle.Closed() {
		t.Fatal("entity handle remained worldless after destination and recovery worlds closed")
	}
}

func TestPortalTravelRecoversDestinationPanic(t *testing.T) {
	for _, test := range []struct {
		name         string
		spawnHandler world.Handler
		teleport     func(Traveller, mgl64.Vec3, cube.Rotation, mgl64.Vec3)
	}{
		{name: "spawn", spawnHandler: panicSpawnHandler{}},
		{name: "teleport", teleport: func(e Traveller, _ mgl64.Vec3, _ cube.Rotation, _ mgl64.Vec3) {
			e.Teleport(mgl64.Vec3{200, 90, 200})
			applyPortalRotation(e, cube.Rotation{120, -15})
			if mover, ok := e.(interface{ SetVelocity(mgl64.Vec3) }); ok {
				mover.SetVelocity(mgl64.Vec3{4, 5, 6})
			}
			panic("teleport panic")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := world.Config{Synchronous: true}.New()
			destination := world.Config{Dim: world.Nether, Synchronous: true}.New()
			t.Cleanup(func() {
				_ = source.Close()
				_ = destination.Close()
			})
			targetPortal := cube.Pos{10, 64, 10}
			mustDo(t, destination, func(tx *world.Tx) {
				buildActivePortal(tx, targetPortal)
			})
			if test.spawnHandler != nil {
				destination.Handle(test.spawnHandler)
			}

			origin := mgl64.Vec3{80.5, 64, 80.5}
			originalRotation := cube.Rotation{30, 10}
			originalVelocity := mgl64.Vec3{0.25, 0.5, -0.75}
			handle := world.EntitySpawnOpts{Position: origin}.New(EnderPearlType, enderPearlConf)
			var tc *PortalTravelComputer
			var rollbackOrigin portalTravelOrigin
			teleportCalls := 0
			mustDo(t, source, func(tx *world.Tx) {
				e := tx.AddEntity(handle).(*Ent)
				e.setRotation(originalRotation)
				e.SetVelocity(originalVelocity)
				tc = e.Behaviour().(*ProjectileBehaviour).PortalTravelComputer()
				if test.teleport != nil {
					tc.Teleport = func(e Traveller, pos mgl64.Vec3, rotation cube.Rotation, velocity mgl64.Vec3) {
						teleportCalls++
						test.teleport(e, pos, rotation, velocity)
					}
				}
				rollbackOrigin = capturePortalTravelOrigin(e)
				tc.beginTravel()
				tx.RemoveEntity(e)
			})

			// Synchronous owners isolate the rollback transition from autonomous
			// projectile ticks, which may destroy a correctly restored pearl.
			tc.transfer(handle, source, destination, rollbackOrigin, translatePortalPosition(cube.PosFromVec3(origin), world.Overworld, world.Nether), world.Overworld, world.Nether)
			if !entityInWorld(handle, source) {
				t.Fatal("entity was not restored to the source after destination panic")
			}
			if entityInWorld(handle, destination) {
				t.Fatal("entity remained destination-bound after destination panic")
			}
			wantTeleportCalls := 0
			if test.teleport != nil {
				wantTeleportCalls = 1
			}
			if teleportCalls != wantTeleportCalls {
				t.Fatalf("Teleport callback called %d times, want %d", teleportCalls, wantTeleportCalls)
			}
			mustDo(t, source, func(tx *world.Tx) {
				e, ok := handle.Entity(tx)
				if !ok {
					t.Fatal("restored handle was not accessible from the source transaction")
				}
				if got := e.Position(); !got.ApproxEqual(origin) {
					t.Fatalf("restored entity position = %v, want %v", got, origin)
				}
				if got := e.Rotation(); got != originalRotation {
					t.Fatalf("restored entity rotation = %v, want %v", got, originalRotation)
				}
				if got := e.(interface{ Velocity() mgl64.Vec3 }).Velocity(); got != originalVelocity {
					t.Fatalf("restored entity velocity = %v, want %v", got, originalVelocity)
				}
				if e.(*Ent).Behaviour().(*ProjectileBehaviour).PortalTravel() {
					t.Fatal("projectile was marked as having completed portal travel")
				}
			})
			tc.mu.Lock()
			travelling, timedOut, pending := tc.travelling, tc.timedOut, tc.pending
			tc.mu.Unlock()
			if travelling || timedOut || pending != nil {
				t.Fatalf("travel state stuck after rollback: travelling=%t timedOut=%t pending=%v", travelling, timedOut, pending)
			}
		})
	}
}

func TestPortalDestinationNetherSearchBoundary(t *testing.T) {
	for _, test := range []struct {
		x     int
		found bool
	}{{-17, false}, {-16, true}, {16, true}, {17, false}} {
		w := world.Config{Dim: world.Nether, Synchronous: true}.New()
		mustDo(t, w, func(tx *world.Tx) {
			buildActivePortal(tx, cube.Pos{test.x, 64, 0})
			_, found := NewPortalTravelComputer().destinationSpawn(tx, world.Overworld, cube.Pos{0, 64, 0})
			if found != test.found {
				t.Errorf("portal at x=%d found=%t, want %t", test.x, found, test.found)
			}
		})
		_ = w.Close()
	}
}

func TestPortalTravelRegistersEntityInDestinationChunk(t *testing.T) {
	source := world.Config{Synchronous: true}.New()
	destination := world.Config{Dim: world.Nether, Synchronous: true}.New()
	t.Cleanup(func() {
		_ = source.Close()
		_ = destination.Close()
	})
	targetPortal := cube.Pos{160, 64, 160}
	tc := NewPortalTravelComputer()
	var spawn mgl64.Vec3
	mustDo(t, destination, func(tx *world.Tx) {
		buildActivePortal(tx, targetPortal)
		var ok bool
		spawn, ok = tc.destinationSpawn(tx, world.Overworld, targetPortal)
		if !ok {
			t.Fatal("destination portal was not found")
		}
	})

	origin := mgl64.Vec3{0.5, 64, 0.5}
	handle := world.EntitySpawnOpts{Position: origin}.New(testMovingEntType{}, testMoveConfig{})
	var originState portalTravelOrigin
	mustDo(t, source, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		originState = capturePortalTravelOrigin(e.(Traveller))
		if tx.RemoveEntity(e) != handle {
			t.Fatal("failed to remove entity from source world")
		}
	})
	tc.transfer(handle, source, destination, originState, targetPortal, world.Overworld, world.Nether)

	mustDo(t, destination, func(tx *world.Tx) {
		found := false
		for e := range tx.EntitiesWithin(cube.Box(spawn[0]-1, spawn[1]-1, spawn[2]-1, spawn[0]+1, spawn[1]+1, spawn[2]+1)) {
			found = found || e.H() == handle
		}
		if !found {
			t.Fatal("transferred entity was not indexed in its destination chunk")
		}
	})
}

func TestFallingBlockDoesNotTravelThroughPortal(t *testing.T) {
	overworld, nether := portalWorlds(t)

	spawnRecorder := &entitySpawnRecorder{}
	nether.Handle(spawnRecorder)

	targetPortal := cube.Pos{10, 64, 10}
	mustDo(t, nether, func(tx *world.Tx) {
		buildActivePortal(tx, targetPortal)
	})

	sourcePos := mgl64.Vec3{80.5, 64, 80.5}
	handle := NewFallingBlock(world.EntitySpawnOpts{Position: sourcePos}, block.Sand{})
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		(block.Portal{Axis: cube.Z}).EntityInside(cube.PosFromVec3(sourcePos), tx, e)
	})

	// Portal travel finishes asynchronously, so give it time to wrongly happen before asserting nothing moved.
	time.Sleep(100 * time.Millisecond)
	if !entityInWorld(handle, overworld) {
		t.Fatal("falling block left the source world through a portal")
	}
	if spawnRecorder.called {
		t.Fatal("falling block was spawned in the destination world")
	}
}

func TestEntPortalTravelCreatesPortal(t *testing.T) {
	overworld, nether := portalWorlds(t)

	sourcePos := mgl64.Vec3{80.5, 64, 80.5}
	handle := world.EntitySpawnOpts{Position: sourcePos}.New(testMovingEntType{}, testPortalCreatorConfig{})
	mustDo(t, overworld, func(tx *world.Tx) {
		e := tx.AddEntity(handle)
		(block.Portal{Axis: cube.Z}).EntityInside(cube.PosFromVec3(sourcePos), tx, e)
	})

	waitForEntityWorld(t, handle, nether)
	mustDo(t, nether, func(tx *world.Tx) {
		if _, ok := portal.FindNetherPortal(tx, cube.Pos{10, 64, 10}, 16); !ok {
			t.Fatal("no portal was created in the destination world for a portal-creating entity")
		}
	})
}

// portalWorlds returns an Overworld and Nether world linked to each other through portals.
func portalWorlds(t *testing.T) (overworld, nether *world.World) {
	t.Helper()
	overworld = world.Config{PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return nether
		}
		return nil
	}}.New()
	nether = world.Config{Dim: world.Nether, PortalDestination: func(dim world.Dimension) *world.World {
		if dim == world.Nether {
			return overworld
		}
		return nil
	}}.New()
	t.Cleanup(func() {
		_ = overworld.Close()
		_ = nether.Close()
	})
	return overworld, nether
}

func mustDo(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	if err := w.Do(f).Wait(context.Background()); err != nil {
		t.Fatalf("world task failed: %v", err)
	}
}

// testPortalCreatorConfig configures a test entity that may create destination portals, like a player.
type testPortalCreatorConfig struct{}

func (testPortalCreatorConfig) Apply(data *world.EntityData) {
	data.Data = &testMoveBehaviour{BaseBehaviour: BaseBehaviour{portalTravel: &PortalTravelComputer{CreatePortal: true}}}
}

func waitForEntityWorld(t *testing.T, handle *world.EntityHandle, w *world.World) {
	t.Helper()
	inWorld, err := world.CallEntity(context.Background(), handle, func(tx *world.Tx, _ world.Entity) (bool, error) {
		return tx.World() == w, nil
	})
	if err != nil || !inWorld {
		t.Fatalf("completed portal transfer: expected world=%p, inWorld=%v, err=%v", w, inWorld, err)
	}
}

func entityInWorld(handle *world.EntityHandle, w *world.World) bool {
	inWorld, err := world.CallEntity(context.Background(), handle, func(tx *world.Tx, _ world.Entity) (bool, error) {
		return tx.World() == w, nil
	})
	return err == nil && inWorld
}

func buildActivePortal(tx *world.Tx, origin cube.Pos) {
	for x := range 2 {
		p := origin.Add(cube.Pos{0, 0, x})
		tx.SetBlock(p.Side(cube.FaceDown), block.Obsidian{}, nil)
		tx.SetBlock(p.Add(cube.Pos{0, 3}), block.Obsidian{}, nil)
	}
	for y := range 3 {
		p := origin.Add(cube.Pos{0, y})
		tx.SetBlock(p.Side(cube.FaceNorth), block.Obsidian{}, nil)
		tx.SetBlock(p.Add(cube.Pos{0, 0, 2}), block.Obsidian{}, nil)
		for x := range 2 {
			tx.SetBlock(p.Add(cube.Pos{0, 0, x}), block.Portal{Axis: cube.Z}, nil)
		}
	}
}

type entitySpawnSignal struct {
	world.NopHandler
	handle  *world.EntityHandle
	spawned chan struct{}
}

func (s entitySpawnSignal) HandleEntitySpawn(_ *world.Tx, e world.Entity) {
	if e.H() == s.handle {
		close(s.spawned)
	}
}

type entitySpawnRecorder struct {
	world.NopHandler

	called bool
	pos    mgl64.Vec3
}

type panicSpawnHandler struct{ world.NopHandler }

func (panicSpawnHandler) HandleEntitySpawn(*world.Tx, world.Entity) { panic("spawn panic") }

func (r *entitySpawnRecorder) HandleEntitySpawn(_ *world.Tx, e world.Entity) {
	r.called = true
	r.pos = e.Position()
}

type testMoveConfig struct {
	delta   mgl64.Vec3
	release func()
}

func (c testMoveConfig) Apply(data *world.EntityData) {
	data.Data = &testMoveBehaviour{BaseBehaviour: NewBaseBehaviour(), delta: c.delta, release: c.release}
}

type testMoveBehaviour struct {
	BaseBehaviour

	delta   mgl64.Vec3
	release func()
}

func (b *testMoveBehaviour) Tick(e *Ent, _ *world.Tx) *Movement {
	e.data.Pos = e.data.Pos.Add(b.delta)
	if b.release != nil {
		return &Movement{release: b.release}
	}
	return nil
}

type testMovingEntType struct{}

func (testMovingEntType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (testMovingEntType) EncodeEntity() string { return "minecraft:test_moving_ent" }
func (testMovingEntType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}
func (testMovingEntType) DecodeNBT(map[string]any, *world.EntityData) {}
func (testMovingEntType) EncodeNBT(*world.EntityData) map[string]any  { return nil }
