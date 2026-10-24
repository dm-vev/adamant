package entity

import (
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndCrystalType is a world.EntityType implementation for End Crystals.

// NewEndCrystal creates a worldless end crystal handle with the supplied state.
func NewEndCrystal(opts world.EntitySpawnOpts, conf EndCrystalBehaviourConfig) *world.EntityHandle {
	return opts.New(EndCrystalType, conf)
}

var EndCrystalType endCrystalType

type endCrystalType struct{}

func (endCrystalType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &EndCrystal{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (endCrystalType) EncodeEntity() string { return "minecraft:ender_crystal" }

// NetworkEncodeEntity returns the Bedrock network identifier for the end crystal.
// Older builds used "minecraft:crystal", but Bedrock currently expects "minecraft:ender_crystal".
func (endCrystalType) NetworkEncodeEntity() string { return "minecraft:ender_crystal" }

func (endCrystalType) BBox(world.Entity) cube.BBox {
	return cube.Box(-1, 0, -1, 1, 2, 1)
}

func (endCrystalType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := endCrystalConf
	if _, ok := m["ShowBottom"]; ok {
		conf.ShowBase = nbtconv.Bool(m, "ShowBottom")
	}
	conf.ExplosionSize = nbtconv.Float64(m, "ExplosionSize")
	conf.PermanentlyInvulnerable = nbtconv.Bool(m, "Invulnerable")
	decodeTarget := func(prefix string) (mgl64.Vec3, bool) {
		if _, ok := m[prefix+"X"]; !ok {
			return mgl64.Vec3{}, false
		}
		if _, ok := m[prefix+"Y"]; !ok {
			return mgl64.Vec3{}, false
		}
		if _, ok := m[prefix+"Z"]; !ok {
			return mgl64.Vec3{}, false
		}
		return mgl64.Vec3{float64(nbtconv.Int32(m, prefix+"X")), float64(nbtconv.Int32(m, prefix+"Y")), float64(nbtconv.Int32(m, prefix+"Z"))}, true
	}
	target, ok := decodeTarget("BlockTarget")
	if !ok {
		target, ok = decodeTarget("BeamTarget")
	}
	if ok {
		conf.BeamTarget = &target
	}
	data.Data = conf.New()
}

func (endCrystalType) EncodeNBT(data *world.EntityData) map[string]any {
	behaviour := data.Data.(*EndCrystalBehaviour)
	m := map[string]any{
		"ShowBottom":    boolByte(behaviour.ShowBase()),
		"ExplosionSize": behaviour.explosionSize,
		"Invulnerable":  boolByte(behaviour.permanentlyInvulnerable),
	}
	if target, ok := behaviour.beamTargetPos(); ok {
		m["BlockTargetX"] = int32(target[0])
		m["BlockTargetY"] = int32(target[1])
		m["BlockTargetZ"] = int32(target[2])
	}
	return m
}

// EndCrystal is an entity representing an End crystal.
type EndCrystal struct {
	*Ent
}

// Destroy destroys the end crystal, omitting a second explosion for void and explosion damage.
func (e *EndCrystal) Destroy(tx *world.Tx, src world.DamageSource, _ world.Entity) bool {
	return e.behaviour().Destroy(e.Ent, tx, src)
}

// Explode removes the end crystal when an explosion impacts it.
func (e *EndCrystal) Explode(src world.ExplosionSource, impact float64) {
	if impact <= 0 {
		return
	}
	_ = e.Destroy(e.tx, ExplosionDamageSource{Source: src}, nil)
}

// ShowBase reports if the crystal should render its bedrock base.
func (e *EndCrystal) ShowBase() bool {
	return e.behaviour().ShowBase()
}

// BeamTarget returns the target that the end crystal's beam should connect to.
func (e *EndCrystal) BeamTarget() (mgl64.Vec3, bool) {
	return e.behaviour().BeamTarget()
}

// SetBeamTarget updates or clears the crystal beam and publishes its metadata.
func (e *EndCrystal) SetBeamTarget(pos *cube.Pos) {
	b := e.behaviour()
	if pos == nil {
		if !b.hasBeamTarget {
			return
		}
		b.hasBeamTarget = false
	} else {
		if b.hasBeamTarget && b.beamTarget == *pos {
			return
		}
		b.beamTarget, b.hasBeamTarget = *pos, true
	}
	e.updateState()
}

// SetPermanentlyInvulnerable controls the protection used during dragon respawn.
func (e *EndCrystal) SetPermanentlyInvulnerable(invulnerable bool) {
	e.behaviour().permanentlyInvulnerable = invulnerable
}

func (e *EndCrystal) behaviour() *EndCrystalBehaviour {
	return e.Behaviour().(*EndCrystalBehaviour)
}

var endCrystalConf = EndCrystalBehaviourConfig{
	Stationary:    StationaryBehaviourConfig{},
	ExplosionSize: 6,
	ShowBase:      true,
}

// EndCrystalBehaviourConfig holds configuration for the end crystal behaviour.
type EndCrystalBehaviourConfig struct {
	Stationary              StationaryBehaviourConfig
	ExplosionSize           float64
	ShowBase                bool
	BeamTarget              *mgl64.Vec3
	PermanentlyInvulnerable bool
}

// Apply applies the configuration to the entity data.
func (conf EndCrystalBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new EndCrystalBehaviour from the config.
func (conf EndCrystalBehaviourConfig) New() *EndCrystalBehaviour {
	if conf.ExplosionSize == 0 {
		conf.ExplosionSize = 6
	}
	behaviour := &EndCrystalBehaviour{
		stationary:              conf.Stationary.New(),
		explosionSize:           conf.ExplosionSize,
		showBase:                conf.ShowBase,
		permanentlyInvulnerable: conf.PermanentlyInvulnerable,
	}
	if conf.BeamTarget != nil {
		behaviour.beamTarget = cube.PosFromVec3(*conf.BeamTarget)
		behaviour.hasBeamTarget = true
	}
	return behaviour
}

// EndCrystalBehaviour implements the behaviour of an end crystal entity.
type EndCrystalBehaviour struct {
	stationary              *StationaryBehaviour
	explosionSize           float64
	showBase                bool
	permanentlyInvulnerable bool
	beamTarget              cube.Pos
	hasBeamTarget           bool
	exploded                bool
}

// Tick creates the source fire block beneath End crystals while a fight exists.
func (b *EndCrystalBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if tx.DragonFight() != nil {
		pos := cube.PosFromVec3(e.Position())
		if _, air := tx.Block(pos).(block.Air); air {
			fire := block.Fire{}
			tx.SetBlock(pos, fire, nil)
			tx.ScheduleBlockUpdate(pos, fire, time.Duration(30+rand.IntN(10))*time.Second/20)
		}
	}
	return b.stationary.Tick(e, tx)
}

// Destroy destroys the end crystal if it hasn't been destroyed yet.
func (b *EndCrystalBehaviour) Destroy(e *Ent, tx *world.Tx, src world.DamageSource) bool {
	if b.exploded {
		return false
	}
	// Entity.isInvulnerableToBase: the void and creative players get
	// through the invulnerability of the crystals of a respawn.
	if b.permanentlyInvulnerable && !creativeDamage(src) {
		if _, void := src.(VoidDamageSource); !void {
			return false
		}
	}
	switch source := src.(type) {
	case AttackDamageSource:
		if _, dragon := source.Attacker.(*EnderDragon); dragon {
			return false
		}
	case ProjectileDamageSource:
		if _, dragon := source.Owner.(*EnderDragon); dragon {
			return false
		}
	}
	b.exploded = true
	crystal := &EndCrystal{Ent: e}
	explosion := world.NewEntityExplosionSource(crystal, b.explosionSize)
	_ = e.CloseIn(tx)
	_, void := src.(VoidDamageSource)
	_, alreadyExploding := src.(ExplosionDamageSource)
	if !void && !alreadyExploding {
		block.ExplosionConfig{}.Explode(tx, explosion)
	}
	if fight := tx.DragonFight(); fight != nil {
		fight.OnCrystalDestroyed(tx, crystal, cube.PosFromVec3(e.Position()), src)
	}
	return true
}

// ShowBase reports whether the base of the end crystal should be visible.
func (b *EndCrystalBehaviour) ShowBase() bool {
	return b.showBase
}

// SetShowBase updates whether the base of the end crystal should be visible.
func (b *EndCrystalBehaviour) SetShowBase(show bool) {
	b.showBase = show
}

// BeamTarget returns the beam target of the end crystal, if any.
func (b *EndCrystalBehaviour) BeamTarget() (mgl64.Vec3, bool) {
	if !b.hasBeamTarget {
		return mgl64.Vec3{}, false
	}
	return b.beamTarget.Vec3(), true
}

func (b *EndCrystalBehaviour) beamTargetPos() (cube.Pos, bool) {
	return b.beamTarget, b.hasBeamTarget
}

// SetBeamTarget updates the beam target of the end crystal.
func (b *EndCrystalBehaviour) SetBeamTarget(pos mgl64.Vec3) {
	b.beamTarget, b.hasBeamTarget = cube.PosFromVec3(pos), true
}

// creativeDamage ports DamageSource.isCreativePlayer.
func creativeDamage(src world.DamageSource) bool {
	var attacker world.Entity
	switch source := src.(type) {
	case AttackDamageSource:
		attacker = source.Attacker
	case ProjectileDamageSource:
		attacker = source.Owner
	}
	g, ok := attacker.(interface{ GameMode() world.GameMode })
	return ok && g.GameMode() == world.GameModeCreative
}
