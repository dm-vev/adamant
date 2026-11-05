package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

func patrolGround(tx *world.Tx) {
	for x := -20; x <= 20; x++ {
		for z := -20; z <= 20; z++ {
			tx.SetBlock(cube.Pos{x, 63, z}, block.Stone{}, nil)
		}
	}
}

func droppedItems(tx *world.Tx) map[string]int {
	out := map[string]int{}
	for e := range tx.Entities() {
		if ent, ok := e.(*Ent); ok && e.H().Type() == ItemType {
			stack := ent.Behaviour().(*ItemBehaviour).Item()
			name, _ := stack.Item().EncodeItem()
			out[name] += stack.Count()
		}
	}
	return out
}

// patrolWorld makes a world with a patrol of a leader and a member, and runs
// it for the 300 ticks it takes the leader to share a leg. It returns the
// world, the handles of the two and whether the member got the leg.
//
// A patrol only holds while its members are within 16 blocks of each other
// (LongDistancePatrolGoal.findPatrolCompanions). The first path of mobs fresh
// from spawning fails and the patrol waits ten seconds, in which both stroll
// up to 8 blocks each, so in about one patrol in a thousand they end up in
// opposite directions more than 16 blocks apart and the leader ends the
// patrol, as in Java. That is not a failure to share a leg, and the caller
// tries another patrol.
func patrolWorld(t *testing.T) (w *world.World, leaderH, memberH *world.EntityHandle, shared, ended bool) {
	t.Helper()
	w = world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		patrolGround(tx)
		if !spawnPatrolMember(tx, cube.Pos{0, 64, 0}, true, levelRandom(w)) || !spawnPatrolMember(tx, cube.Pos{2, 64, 0}, false, levelRandom(w)) {
			t.Fatal("patrol members did not spawn on open ground")
		}
		var leader, member *Pillager
		for e := range tx.Entities() {
			if m, ok := e.(*Pillager); ok {
				if m.state().patrolLeader {
					leader = m
				} else {
					member = m
				}
			}
		}
		if leader == nil || member == nil {
			t.Fatal("expected one leader and one member")
		}
		if !leader.IsCaptain() || member.IsCaptain() {
			t.Fatalf("captain flags leader=%v member=%v", leader.IsCaptain(), member.IsCaptain())
		}
		if !leader.state().patrolling || !leader.state().hasPatrolTarget || !member.state().patrolling {
			t.Fatal("patrol spawns must be patrolling with a leader target")
		}
		leaderH, memberH = leader.H(), member.H()
	})
	// The leader walks a leg toward its target and hands it to the member.
	// Mobs fresh from spawning are not on the ground yet, so their first path
	// fails and the patrol waits ten seconds, as in Java.
	for range 300 {
		if err := w.StepTicks(1); err != nil {
			t.Fatal(err)
		}
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := memberH.Entity(tx); ok {
				shared = shared || e.(*Pillager).state().patrolTarget != (cube.Pos{})
			}
			if e, ok := leaderH.Entity(tx); ok {
				ended = ended || !e.(*Pillager).state().patrolling
			}
		})
	}
	return w, leaderH, memberH, shared, ended
}

func TestPatrolLeaderIsCaptainAndDropsOminousBottle(t *testing.T) {
	t.Parallel()
	var (
		w                *world.World
		leaderH, memberH *world.EntityHandle
	)
	// Up to eight patrols, which are not all ended by their members
	// straying apart: that has to be a rare chance, and a leader that holds
	// its patrol but never shares a leg fails at once.
	for attempt := 1; ; attempt++ {
		var shared, ended bool
		w, leaderH, memberH, shared, ended = patrolWorld(t)
		if shared {
			break
		}
		if !ended {
			t.Fatal("leader did not share its patrol leg with the member")
		}
		if attempt == 8 {
			t.Fatalf("the members strayed more than 16 blocks apart in %d patrols in a row", attempt)
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		leaderE, _ := leaderH.Entity(tx)
		memberE, _ := memberH.Entity(tx)
		leader, member := leaderE.(*Pillager), memberE.(*Pillager)
		for e := range tx.Entities() {
			if e.H().Type() == ItemType {
				_ = e.(*Ent).CloseIn(tx)
			}
		}
		cow := tx.AddEntity(NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{5, 64, 5}}))
		member.Hurt(1000, AttackDamageSource{Attacker: cow})
		if got := droppedItems(tx)["minecraft:ominous_bottle"]; got != 0 {
			t.Fatalf("non-captain dropped %d ominous bottles", got)
		}
		leader.Hurt(1000, AttackDamageSource{Attacker: cow})
		drops := droppedItems(tx)
		if drops["minecraft:ominous_bottle"] != 1 || drops["minecraft:banner"] != 1 {
			t.Fatalf("captain drops = %v, want an ominous bottle and the banner", drops)
		}
	})
}
