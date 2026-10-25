package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// killDamageSource is the Java generic-kill source: it bypasses invulnerability,
// armour, resistance and totems, while retaining normal entity death handling.
type killDamageSource struct{}

func (killDamageSource) ReducedByArmour() bool         { return false }
func (killDamageSource) ReducedByResistance() bool     { return false }
func (killDamageSource) Fire() bool                    { return false }
func (killDamageSource) IgnoreTotem() bool             { return true }
func (killDamageSource) BypassesInvulnerability() bool { return true }

type killCommand struct {
	opGate
	Targets cmd.Optional[[]cmd.Target] `cmd:"targets"`
}

func newKillCommand() cmd.Command {
	return newCommand("kill", nil, killCommand{})
}

func (k killCommand) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	targets, ok := k.Targets.Load()
	if !ok {
		if src == nil {
			o.Errort(cmd.MessageNoTargets)
			return
		}
		targets = []cmd.Target{src}
	}
	count := 0
	for _, target := range targets {
		living, ok := target.(entity.Living)
		if !ok || living.Dead() {
			continue
		}
		living.Hurt(living.Health()+1, killDamageSource{})
		count++
	}
	if count == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	o.Printf("Killed %d entities.", count)
}

// damageCommand ports Java DamageCommand:
// damage <targets> <amount> [<type>] [at <location> | by <entity> [from <cause>]].
type damageCommand struct {
	opGate
	Targets []cmd.Target         `cmd:"targets"`
	Amount  float64              `cmd:"amount"`
	Type    cmd.Optional[string] `cmd:"damageType"`
}
type damageAtCommand struct {
	opGate
	Targets  []cmd.Target   `cmd:"targets"`
	Amount   float64        `cmd:"amount"`
	Type     string         `cmd:"damageType"`
	At       cmd.SubCommand `cmd:"at"`
	Location mgl64.Vec3     `cmd:"location"`
}
type damageByCommand struct {
	opGate
	Targets []cmd.Target   `cmd:"targets"`
	Amount  float64        `cmd:"amount"`
	Type    string         `cmd:"damageType"`
	By      cmd.SubCommand `cmd:"by"`
	Entity  []cmd.Target   `cmd:"entity"`
}
type damageByFromCommand struct {
	opGate
	Targets []cmd.Target   `cmd:"targets"`
	Amount  float64        `cmd:"amount"`
	Type    string         `cmd:"damageType"`
	By      cmd.SubCommand `cmd:"by"`
	Entity  []cmd.Target   `cmd:"entity"`
	From    cmd.SubCommand `cmd:"from"`
	Cause   []cmd.Target   `cmd:"cause"`
}

type genericDamageSource struct{}

func (genericDamageSource) ReducedByArmour() bool     { return true }
func (genericDamageSource) ReducedByResistance() bool { return true }
func (genericDamageSource) Fire() bool                { return false }
func (genericDamageSource) IgnoreTotem() bool         { return false }

type fixedDamageSource struct{ fire, armour, resistance, ignoreTotem bool }

func (s fixedDamageSource) ReducedByArmour() bool     { return s.armour }
func (s fixedDamageSource) ReducedByResistance() bool { return s.resistance }
func (s fixedDamageSource) Fire() bool                { return s.fire }
func (s fixedDamageSource) IgnoreTotem() bool         { return s.ignoreTotem }

func damageSource(name string, attacker world.Entity) (world.DamageSource, bool) {
	if attacker != nil {
		return entity.AttackDamageSource{Attacker: attacker}, true
	}
	switch name {
	case "generic", "mob_attack", "player_attack":
		return genericDamageSource{}, true
	case "generic_kill":
		return killDamageSource{}, true
	case "out_of_world":
		return entity.VoidDamageSource{}, true
	case "in_wall":
		return entity.SuffocationDamageSource{}, true
	case "drown":
		return entity.DrowningDamageSource{}, true
	case "fall":
		return entity.FallDamageSource{}, true
	case "fly_into_wall":
		return entity.GlideDamageSource{}, true
	case "lightning_bolt":
		return entity.LightningDamageSource{}, true
	case "explosion", "player_explosion", "bad_respawn_point":
		return entity.ExplosionDamageSource{}, true
	case "fire", "on_fire", "lava", "hot_floor", "fireball", "in_fire":
		return block.FireDamageSource{}, true
	case "magic":
		return effect.InstantDamageSource{}, true
	case "wither":
		return effect.WitherDamageSource{}, true
	case "starve":
		return fixedDamageSource{armour: false, resistance: false}, true
	case "cactus", "sweet_berry_bush", "stalagmite":
		return fixedDamageSource{armour: false, resistance: true}, true
	default:
		return nil, false
	}
}

func newDamageCommand() cmd.Command {
	return newCommand("damage", nil, damageCommand{}, damageAtCommand{}, damageByCommand{}, damageByFromCommand{})
}

func (d damageCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	applyCommandDamage(d.Targets, d.Amount, d.Type.LoadOr("generic"), nil, nil, o)
}

// The Bedrock damage API carries no positional origin, so `at` only selects
// the damage type, as Java does for types without position-scaled effects.
func (d damageAtCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	applyCommandDamage(d.Targets, d.Amount, d.Type, nil, nil, o)
}

func (d damageByCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	attacker, ok := singleTarget(d.Entity, o)
	if !ok {
		return
	}
	applyCommandDamage(d.Targets, d.Amount, d.Type, attacker, nil, o)
}

func (d damageByFromCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	direct, ok := singleTarget(d.Entity, o)
	if !ok {
		return
	}
	cause, ok := singleTarget(d.Cause, o)
	if !ok {
		return
	}
	applyCommandDamage(d.Targets, d.Amount, d.Type, direct, cause, o)
}

// applyCommandDamage resolves the source like Java DamageSource construction:
// a direct entity with a different causing entity is a projectile-style source.
func applyCommandDamage(targets []cmd.Target, amount float64, name string, direct, cause cmd.Target, o *cmd.Output) {
	if amount <= 0 {
		o.Errort(cmd.MessageParameterInvalid, amount)
		return
	}
	name = strings.TrimPrefix(strings.ToLower(name), "minecraft:")
	source, ok := damageSource(name, nil)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, name)
		return
	}
	if attacker, isEntity := direct.(world.Entity); isEntity {
		source = entity.AttackDamageSource{Attacker: attacker}
		if owner, isEntity := cause.(world.Entity); isEntity && owner != attacker {
			source = entity.ProjectileDamageSource{Projectile: attacker, Owner: owner}
		}
	}
	count := 0
	for _, target := range targets {
		living, ok := target.(entity.Living)
		if !ok || living.Dead() {
			continue
		}
		if _, vulnerable := living.Hurt(amount, source); vulnerable {
			count++
		}
	}
	if count == 0 {
		o.Errort(cmd.MessageNoTargets)
		return
	}
	o.Printf("Applied %g damage to %d entities.", amount, count)
}

var _ cmd.Target = (*player.Player)(nil)

func registerPlayerKillCommands(srv serverAdapter) {
	cmd.Register(newKillCommand())
	cmd.Register(newDamageCommand())
}
