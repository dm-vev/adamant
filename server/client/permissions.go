package client

import (
	"fmt"
	"strings"
)

// Role is what a player may do in the world, as Bedrock Dedicated Server's
// permissions.json gives it: the abilities of the axis of roles, apart from
// the level of the commands the player may run.
type Role uint8

const (
	// RoleMember builds, mines, uses doors and switches, opens containers
	// and attacks: what a player who is not an operator does by default.
	RoleMember Role = iota
	// RoleVisitor walks and looks around: it builds, mines, opens and
	// attacks nothing.
	RoleVisitor
	// RoleOperator is the role of an operator: every ability.
	RoleOperator
	// RoleCustom has the abilities its entry lists.
	RoleCustom
)

// String returns the role as permissions.json writes it.
func (r Role) String() string {
	switch r {
	case RoleVisitor:
		return "visitor"
	case RoleOperator:
		return "operator"
	case RoleCustom:
		return "custom"
	default:
		return "member"
	}
}

// ParseRole parses a role as permissions.json writes it, in any case.
func ParseRole(s string) (Role, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "member":
		return RoleMember, nil
	case "visitor":
		return RoleVisitor, nil
	case "operator":
		return RoleOperator, nil
	case "custom":
		return RoleCustom, nil
	}
	return 0, fmt.Errorf(`unknown role %q: want "visitor", "member", "operator" or "custom"`, s)
}

// Abilities is a set of the abilities of a player in the world.
type Abilities uint16

const (
	// AbilityBuild places blocks and uses items on them.
	AbilityBuild Abilities = 1 << iota
	// AbilityMine breaks blocks.
	AbilityMine
	// AbilityDoorsAndSwitches uses doors, trapdoors, fence gates, buttons
	// and levers.
	AbilityDoorsAndSwitches
	// AbilityOpenContainers opens chests and the other blocks and entities
	// that hold items or open a window.
	AbilityOpenContainers
	// AbilityAttackPlayers attacks other players.
	AbilityAttackPlayers
	// AbilityAttackMobs attacks the entities that are not players.
	AbilityAttackMobs
	// AbilityOperatorCommands is the bit of the client that shows the
	// commands of operators. The commands a player may run follow its
	// level, not this bit.
	AbilityOperatorCommands
	// AbilityTeleport is the bit of the client that allows teleporting
	// from its menus.
	AbilityTeleport
)

// AbilitiesOfMembers are the abilities of RoleMember.
const AbilitiesOfMembers = AbilityBuild | AbilityMine | AbilityDoorsAndSwitches | AbilityOpenContainers |
	AbilityAttackPlayers | AbilityAttackMobs

// AbilitiesOfOperators are the abilities of RoleOperator.
const AbilitiesOfOperators = AbilitiesOfMembers | AbilityOperatorCommands | AbilityTeleport

// abilityNames are the names of the abilities, as permissions.json writes
// them, in the order of their bits.
var abilityNames = []string{"build", "mine", "doors_and_switches", "open_containers", "attack_players",
	"attack_mobs", "operator_commands", "teleport"}

// Has reports whether the set holds every ability of a.
func (a Abilities) Has(of Abilities) bool { return a&of == of }

// Names returns the names of the abilities of the set, as permissions.json
// writes them, in the order of their bits.
func (a Abilities) Names() []string {
	var out []string
	for i, name := range abilityNames {
		if a&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	return out
}

// ParseAbilities parses the names of abilities, as permissions.json writes
// them, in any case.
func ParseAbilities(names []string) (Abilities, error) {
	var a Abilities
	for _, name := range names {
		i := -1
		for j, n := range abilityNames {
			if strings.EqualFold(strings.TrimSpace(name), n) {
				i = j
			}
		}
		if i < 0 {
			return 0, fmt.Errorf("unknown ability %q: want one of %s", name, strings.Join(abilityNames, ", "))
		}
		a |= 1 << i
	}
	return a, nil
}

// Permissions are what a player online may do: the level of the commands
// it runs, 0 to 4 as Java numbers them, its role and the abilities the
// role gives it.
type Permissions struct {
	// Level is the level of the commands of the player.
	Level uint8
	// Operator reports whether the player is an operator, of any level.
	Operator bool
	// Role is the role of the player.
	Role Role
	// Abilities are the abilities of the role of the player.
	Abilities Abilities
}

// Allows reports whether the player has the abilities passed.
func (p Permissions) Allows(a Abilities) bool { return p.Abilities.Has(a) }

// MemberPermissions returns the permissions of a member of level 0, which
// a player made without a server has.
func MemberPermissions() Permissions {
	return Permissions{Role: RoleMember, Abilities: AbilitiesOfMembers}
}
