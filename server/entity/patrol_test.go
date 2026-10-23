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

func TestPatrolLeaderIsCaptainAndDropsOminousBottle(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1, Entities: DefaultRegistry}.New()
	defer w.Close()
	var leaderH, memberH *world.EntityHandle
	<-w.Exec(func(tx *world.Tx) {
		patrolGround(tx)
		if !spawnPatrolMember(tx, cube.Pos{0, 64, 0}, true) || !spawnPatrolMember(tx, cube.Pos{2, 64, 0}, false) {
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
	shared := false
	for range 300 {
		if err := w.StepTicks(1); err != nil {
			t.Fatal(err)
		}
		w.AdvanceTick()
		<-w.Exec(func(tx *world.Tx) {
			if e, ok := memberH.Entity(tx); ok {
				shared = shared || e.(*Pillager).state().patrolTarget != (cube.Pos{})
			}
		})
	}
	if !shared {
		t.Fatal("leader did not share its patrol leg with the member")
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
