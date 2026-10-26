package entity

import (
	"hash/fnv"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// NewEnderPearl creates an EnderPearl entity. EnderPearl is a smooth, greenish-
// blue item used to teleport.
func NewEnderPearl(opts world.EntitySpawnOpts, owner world.Entity) *world.EntityHandle {
	conf := enderPearlConf
	conf.Owner = owner.H()
	return opts.New(EnderPearlType, conf)
}

var enderPearlConf = ProjectileBehaviourConfig{
	Gravity:  0.03,
	Drag:     0.01,
	Particle: particle.EndermanTeleport{},
	Sound:    sound.Teleport{},
	Hit:      teleport,
}

// teleporter represents a living entity that can teleport.
type teleporter interface {
	// Teleport teleports the entity to the position given.
	Teleport(pos mgl64.Vec3)
	Living
}

// teleport ports ThrownEnderpearl.onHit: the owner, if alive, in the same
// world and not sleeping, is teleported to where the pearl was when the tick
// began, so not into the block hit, and takes 5 ender pearl damage, a fall;
// a player owner may leave an endermite behind 5% of the time where monsters
// spawn.
func teleport(e *Ent, tx *world.Tx, _ trace.Result) {
	behaviour := e.Behaviour().(*ProjectileBehaviour)
	if behaviour.PortalTravel() {
		return
	}
	owner, _ := behaviour.Owner().Entity(tx)
	user, ok := owner.(teleporter)
	if !ok || user.Dead() {
		return
	}
	if s, ok := owner.(interface{ Sleeping() (cube.Pos, bool) }); ok {
		if _, sleeping := s.Sleeping(); sleeping {
			return
		}
	}
	dest := behaviour.oldPos
	if isPlayerEntity(owner) {
		uuid := e.H().UUID()
		hash := fnv.New64a()
		_, _ = hash.Write(uuid[:])
		random := vanilla.NewLegacyRandom(int64(hash.Sum64()))
		if random.NextFloat() < .05 && tx.World().GameRuleBool("spawn_monsters") && tx.World().Difficulty() != world.DifficultyPeaceful {
			tx.AddEntity(NewEndermite(world.EntitySpawnOpts{Position: owner.Position(), Rotation: owner.Rotation(), SpawnReason: "ender_pearl"}))
		}
	}
	user.Teleport(dest)
	if f, ok := owner.(interface{ ResetFallDistance() }); ok {
		f.ResetFallDistance()
	}
	if isPlayerEntity(owner) && tx.World().FallDamage() {
		user.Hurt(5, FallDamageSource{})
	}
	tx.PlaySound(dest, sound.Teleport{})
}

// EnderPearlType is a world.EntityType implementation for EnderPearl.
var EnderPearlType enderPearlType

type enderPearlType struct{}

func (t enderPearlType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (enderPearlType) EncodeEntity() string { return "minecraft:ender_pearl" }
func (enderPearlType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}
func (enderPearlType) DecodeNBT(_ map[string]any, data *world.EntityData) {
	data.Data = enderPearlConf.New()
}
func (enderPearlType) EncodeNBT(*world.EntityData) map[string]any { return nil }
