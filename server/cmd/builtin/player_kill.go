package builtin

import (
	"strings"

	"github.com/dm-vev/adamant/server/block"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
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
	gameMaster
	Targets cmd.Optional[cmd.Entities] `cmd:"targets"`
}

func newKillCommand() cmd.Command {
	return newCommand("kill", nil, killCommand{})
}

func (k killCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	cmd.ForEach(src, o, tx, optionalEntities(k.Targets, src),
		func(_ *world.Tx, t cmd.Target) int {
			living, ok := t.(entity.Living)
			if !ok || living.Dead() {
				return 0
			}
			living.Hurt(living.Health()+1, killDamageSource{})
			return 1
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Killed %d entities.", tally.NonZero)
		},
	)
}

// damageCommand ports Java DamageCommand:
// damage <targets> <amount> [<type>] [at <location> | by <entity> [from <cause>]].
type damageCommand struct {
	gameMaster
	Targets cmd.Entities         `cmd:"targets"`
	Amount  float64              `cmd:"amount"`
	Type    cmd.Optional[string] `cmd:"damageType"`
}
type damageAtCommand struct {
	gameMaster
	Targets  cmd.Entities   `cmd:"targets"`
	Amount   float64        `cmd:"amount"`
	Type     string         `cmd:"damageType"`
	At       cmd.SubCommand `cmd:"at"`
	Location mgl64.Vec3     `cmd:"location"`
}
type damageByCommand struct {
	gameMaster
	Targets cmd.Entities   `cmd:"targets"`
	Amount  float64        `cmd:"amount"`
	Type    string         `cmd:"damageType"`
	By      cmd.SubCommand `cmd:"by"`
	Entity  cmd.OneEntity  `cmd:"entity"`
}
type damageByFromCommand struct {
	gameMaster
	Targets cmd.Entities   `cmd:"targets"`
	Amount  float64        `cmd:"amount"`
	Type    string         `cmd:"damageType"`
	By      cmd.SubCommand `cmd:"by"`
	Entity  cmd.OneEntity  `cmd:"entity"`
	From    cmd.SubCommand `cmd:"from"`
	Cause   cmd.OneEntity  `cmd:"cause"`
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

func (d damageCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	applyCommandDamage(src, o, tx, d.Targets, d.Amount, d.Type.LoadOr("generic"), nil, nil)
}

// The Bedrock damage API carries no positional origin, so `at` only selects
// the damage type, as Java does for types without position-scaled effects.
func (d damageAtCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	applyCommandDamage(src, o, tx, d.Targets, d.Amount, d.Type, nil, nil)
}

func (d damageByCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	targets, amount, damageType := d.Targets, d.Amount, d.Type
	cmd.ForEach(src, o, tx, d.Entity,
		func(_ *world.Tx, _ cmd.Target) struct{} { return struct{}{} },
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[struct{}]) {
			if len(rs) == 0 || rs[0].Handle == nil {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			applyCommandDamage(src, o, tx, targets, amount, damageType, rs[0].Handle, nil)
		},
	)
}

func (d damageByFromCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	targets, amount, damageType, causeSel := d.Targets, d.Amount, d.Type, d.Cause
	cmd.ForEach(src, o, tx, d.Entity,
		func(_ *world.Tx, _ cmd.Target) struct{} { return struct{}{} },
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[struct{}]) {
			if len(rs) == 0 || rs[0].Handle == nil {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			direct := rs[0].Handle
			cmd.ForEach(src, o, tx, causeSel,
				func(_ *world.Tx, _ cmd.Target) struct{} { return struct{}{} },
				func(src cmd.Source, o *cmd.Output, tx *world.Tx, crs []cmd.Result[struct{}]) {
					if len(crs) == 0 || crs[0].Handle == nil {
						o.Error(cmd.ErrNoEntityFound)
						return
					}
					applyCommandDamage(src, o, tx, targets, amount, damageType, direct, crs[0].Handle)
				},
			)
		},
	)
}

// applyCommandDamage resolves the source like Java DamageSource construction:
// a direct entity with a different causing entity is a projectile-style source.
func applyCommandDamage(src cmd.Source, o *cmd.Output, tx *world.Tx, targets cmd.Entities, amount float64, name string, direct, cause *world.EntityHandle) {
	if amount <= 0 {
		o.Errort(cmd.MessageParameterInvalid, amount)
		return
	}
	name = strings.TrimPrefix(strings.ToLower(name), "minecraft:")
	baseSource, ok := damageSource(name, nil)
	if !ok {
		o.Errort(cmd.MessageParameterInvalid, name)
		return
	}
	cmd.ForEach(src, o, tx, targets,
		func(rtx *world.Tx, target cmd.Target) int {
			living, ok := target.(entity.Living)
			if !ok || living.Dead() {
				return 0
			}
			source := baseSource
			if direct != nil && rtx != nil && direct.World() == rtx.World() {
				if attacker, ok := direct.Entity(rtx); ok {
					source = entity.AttackDamageSource{Attacker: attacker}
					if cause != nil && cause != direct && cause.World() == rtx.World() {
						if owner, ok := cause.Entity(rtx); ok {
							source = entity.ProjectileDamageSource{Projectile: attacker, Owner: owner}
						}
					}
				}
			}
			if _, vulnerable := living.Hurt(amount, source); vulnerable {
				return 1
			}
			return 0
		},
		func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[int]) {
			tally := cmd.TallyOf(rs)
			if tally.NonZero == 0 {
				o.Error(cmd.ErrNoEntityFound)
				return
			}
			o.SetResult(tally.Total)
			o.Printf("Applied %g damage to %d entities.", amount, tally.NonZero)
		},
	)
}

var _ cmd.Target = (*player.Player)(nil)

func registerPlayerKillCommands(srv serverAdapter) {
	cmd.Register(newKillCommand())
	cmd.Register(newDamageCommand())
}
