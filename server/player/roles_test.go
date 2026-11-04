package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// roleOutcome is what a player of a role managed to do in the world.
type roleOutcome struct {
	placed, broke, opened, attacked bool
	// chest reports whether the role lets the player open a chest.
	chest bool
}

// actAs places, breaks, opens a fence gate, asks to open a chest and
// attacks a cow as a player of the permissions passed, and returns what it
// managed.
func actAs(t *testing.T, perms client.Permissions) roleOutcome {
	t.Helper()
	w := entityItemWorld(t)
	var out roleOutcome
	<-w.Exec(func(tx *world.Tx) {
		floor, mined, gate, chest := cube.Pos{8, 63, 8}, cube.Pos{10, 64, 9}, cube.Pos{9, 64, 8}, cube.Pos{8, 64, 10}
		tx.SetBlock(floor, block.Stone{}, nil)
		tx.SetBlock(mined, block.Stone{}, nil)
		tx.SetBlock(gate, block.WoodFenceGate{Wood: block.OakWood()}, nil)
		tx.SetBlock(chest, block.NewChest(), nil)
		start := mgl64.Vec3{9.5, 64, 10.5}
		conf := Config{Position: start, GameMode: world.GameModeSurvival, Permissions: func(*Player) client.Permissions { return perms }}
		p := tx.AddEntity(world.EntitySpawnOpts{Position: start}.New(Type, conf)).(*Player)

		p.SetHeldItems(item.NewStack(block.Stone{}, 2), item.Stack{})
		p.UseItemOnBlock(floor, cube.FaceUp, mgl64.Vec3{0.5, 1, 0.5})
		_, out.placed = tx.Block(floor.Side(cube.FaceUp)).(block.Stone)

		p.BreakBlock(mined)
		_, out.broke = tx.Block(mined).(block.Air)

		p.SetHeldItems(item.Stack{}, item.Stack{})
		p.UseItemOnBlock(gate, cube.FaceNorth, mgl64.Vec3{0.5, 0.5, 0})
		out.opened = tx.Block(gate).(block.WoodFenceGate).Open
		out.chest = !p.roleRefusesUse(chest)

		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{9.5, 64, 11.5}})).(*entity.Cow)
		before := cow.Health()
		p.AttackEntity(cow)
		out.attacked = cow.Health() < before
	})
	return out
}

// TestVisitorCannotActOnTheWorld checks that the game holds a player to the
// abilities of its role, whatever its client shows: a visitor places,
// breaks, opens, uses and attacks nothing; a member does all; a custom role
// does what its abilities list.
func TestVisitorCannotActOnTheWorld(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		perms client.Permissions
		want  roleOutcome
	}{
		{"visitor", client.Permissions{Role: client.RoleVisitor}, roleOutcome{}},
		{"member", client.MemberPermissions(), roleOutcome{placed: true, broke: true, opened: true, attacked: true, chest: true}},
		{"no server", client.Permissions{}, roleOutcome{}},
		{"custom doors and mobs", client.Permissions{Role: client.RoleCustom, Abilities: client.AbilityDoorsAndSwitches | client.AbilityAttackMobs},
			roleOutcome{opened: true, attacked: true}},
		{"custom build, mine and containers", client.Permissions{Role: client.RoleCustom, Abilities: client.AbilityBuild | client.AbilityMine | client.AbilityOpenContainers},
			roleOutcome{placed: true, broke: true, chest: true}},
	} {
		if got := actAs(t, tc.perms); got != tc.want {
			t.Errorf("%s: %+v, want %+v", tc.name, got, tc.want)
		}
	}
	// A player made with no Permissions is a member.
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, Config{})).(*Player)
		if got := p.Permissions(); got != client.MemberPermissions() {
			t.Errorf("a player of no server: %+v", got)
		}
	})
}

// TestRolesOfAttacks checks that attacking a player needs the ability to
// attack players, and attacking a mob the ability to attack mobs.
func TestRolesOfAttacks(t *testing.T) {
	t.Parallel()
	w := entityItemWorld(t)
	<-w.Exec(func(tx *world.Tx) {
		perms := client.Permissions{Role: client.RoleCustom, Abilities: client.AbilityAttackMobs}
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(Type, Config{Permissions: func(*Player) client.Permissions { return perms }})).(*Player)
		other := tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{1, 0, 0}}.New(Type, Config{Position: mgl64.Vec3{1, 0, 0}})).(*Player)
		cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: mgl64.Vec3{0, 0, 1}})).(*entity.Cow)
		if !p.roleRefusesAttack(other) || p.roleRefusesAttack(cow) {
			t.Error("a role that attacks mobs alone attacks players, or no mobs")
		}
		perms.Abilities = client.AbilityAttackPlayers
		if p.roleRefusesAttack(other) || !p.roleRefusesAttack(cow) {
			t.Error("a role that attacks players alone attacks mobs, or no players")
		}
	})
}
