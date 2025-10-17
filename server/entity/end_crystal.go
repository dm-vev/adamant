package entity

import (
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// EndCrystalType is a world.EntityType implementation for End Crystals.
var EndCrystalType endCrystalType

type endCrystalType struct{}

func (endCrystalType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &EndCrystal{Ent: &Ent{tx: tx, handle: handle, data: data}}
}

func (endCrystalType) EncodeEntity() string { return "minecraft:end_crystal" }

func (endCrystalType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.5, 0, -0.5, 0.5, 2, 0.5)
}

func (endCrystalType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := endCrystalConf
	if _, ok := m["ShowBottom"]; ok {
		conf.ShowBase = nbtconv.Bool(m, "ShowBottom")
	}
	if _, ok := m["BeamTargetX"]; ok {
		target := mgl64.Vec3{
			float64(nbtconv.Int32(m, "BeamTargetX")),
			float64(nbtconv.Int32(m, "BeamTargetY")),
			float64(nbtconv.Int32(m, "BeamTargetZ")),
		}
		conf.BeamTarget = &target
	}
	data.Data = conf.New()
}

func (endCrystalType) EncodeNBT(data *world.EntityData) map[string]any {
	behaviour := data.Data.(*EndCrystalBehaviour)
	m := map[string]any{
		"ShowBottom": uint8(0),
	}
	if behaviour.ShowBase() {
		m["ShowBottom"] = uint8(1)
	}
	if target, ok := behaviour.BeamTarget(); ok {
		m["BeamTargetX"] = int32(target[0])
		m["BeamTargetY"] = int32(target[1])
		m["BeamTargetZ"] = int32(target[2])
	}
	return m
}

// EndCrystal is an entity representing an End crystal.
type EndCrystal struct {
	*Ent
}

// Destroy destroys the end crystal, triggering its explosion behaviour.
func (e *EndCrystal) Destroy(tx *world.Tx, src world.DamageSource, _ world.Entity) bool {
	return e.behaviour().Destroy(e.Ent, tx)
}

// Explode removes the end crystal when an explosion impacts it.
func (e *EndCrystal) Explode(_ mgl64.Vec3, impact float64, _ block.ExplosionConfig) {
	if impact <= 0 {
		return
	}
	_ = e.Destroy(e.tx, world.DamageSource(ExplosionDamageSource{}), nil)
}

// ShowBase reports if the crystal should render its bedrock base.
func (e *EndCrystal) ShowBase() bool {
	return e.behaviour().ShowBase()
}

// BeamTarget returns the target that the end crystal's beam should connect to.
func (e *EndCrystal) BeamTarget() (mgl64.Vec3, bool) {
	return e.behaviour().BeamTarget()
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
	Stationary    StationaryBehaviourConfig
	ExplosionSize float64
	ShowBase      bool
	BeamTarget    *mgl64.Vec3
}

// Apply applies the configuration to the entity data.
func (conf EndCrystalBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates a new EndCrystalBehaviour from the config.
func (conf EndCrystalBehaviourConfig) New() *EndCrystalBehaviour {
	stationary := conf.Stationary.New()
	size := conf.ExplosionSize
	if size == 0 {
		size = 6
	}
	behaviour := &EndCrystalBehaviour{
		stationary:    stationary,
		explosionSize: size,
		showBase:      conf.ShowBase,
	}
	if conf.BeamTarget != nil {
		behaviour.beamTarget = *conf.BeamTarget
		behaviour.hasBeamTarget = true
	}
	return behaviour
}

// EndCrystalBehaviour implements the behaviour of an end crystal entity.
type EndCrystalBehaviour struct {
	stationary    *StationaryBehaviour
	explosionSize float64
	showBase      bool
	beamTarget    mgl64.Vec3
	hasBeamTarget bool
	exploded      bool
}

// Tick ticks the underlying stationary behaviour.
func (b *EndCrystalBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	return b.stationary.Tick(e, tx)
}

// Destroy destroys the end crystal if it hasn't been destroyed yet.
func (b *EndCrystalBehaviour) Destroy(e *Ent, tx *world.Tx) bool {
	if b.exploded {
		return false
	}
	b.exploded = true
	block.ExplosionConfig{Size: b.explosionSize, SpawnFire: true, ItemDropChance: 1}.Explode(tx, e.Position())
	_ = e.CloseIn(tx)
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
	return b.beamTarget, b.hasBeamTarget
}

// SetBeamTarget updates the beam target of the end crystal.
func (b *EndCrystalBehaviour) SetBeamTarget(pos mgl64.Vec3) {
	b.beamTarget, b.hasBeamTarget = pos, true
}
