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
	if _, hasX := m["BlockTargetX"]; hasX {
		if _, hasY := m["BlockTargetY"]; hasY {
			if _, hasZ := m["BlockTargetZ"]; hasZ {
				target := mgl64.Vec3{
					float64(nbtconv.Int32(m, "BlockTargetX")),
					float64(nbtconv.Int32(m, "BlockTargetY")),
					float64(nbtconv.Int32(m, "BlockTargetZ")),
				}
				conf.BeamTarget = &target
			}
		}
	}
	data.Data = conf.New()
}

func (endCrystalType) EncodeNBT(data *world.EntityData) map[string]any {
	behaviour := data.Data.(*EndCrystalBehaviour)
	m := map[string]any{
		"ShowBottom":    boolByte(behaviour.ShowBase()),
		"ExplosionSize": behaviour.explosionSize,
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

// Destroy destroys the end crystal, triggering its explosion behaviour unless it was destroyed by the void.
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
	if conf.ExplosionSize == 0 {
		conf.ExplosionSize = 6
	}
	behaviour := &EndCrystalBehaviour{
		stationary:    conf.Stationary.New(),
		explosionSize: conf.ExplosionSize,
		showBase:      conf.ShowBase,
	}
	if conf.BeamTarget != nil {
		behaviour.beamTarget = cube.PosFromVec3(*conf.BeamTarget)
		behaviour.hasBeamTarget = true
	}
	return behaviour
}

// EndCrystalBehaviour implements the behaviour of an end crystal entity.
type EndCrystalBehaviour struct {
	stationary    *StationaryBehaviour
	explosionSize float64
	showBase      bool
	beamTarget    cube.Pos
	hasBeamTarget bool
	exploded      bool
}

// Tick ticks the underlying stationary behaviour and starts fire beneath End crystals in the End.
func (b *EndCrystalBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if tx.World().Dimension() == world.End {
		block.Fire{}.Start(tx, cube.PosFromVec3(e.Position()))
	}
	return b.stationary.Tick(e, tx)
}

// Destroy destroys the end crystal if it hasn't been destroyed yet.
func (b *EndCrystalBehaviour) Destroy(e *Ent, tx *world.Tx, src world.DamageSource) bool {
	if b.exploded {
		return false
	}
	b.exploded = true
	_ = e.CloseIn(tx)
	if _, void := src.(VoidDamageSource); void {
		return true
	}
	block.ExplosionConfig{
		SuppressUnderwaterImpact: true,
	}.Explode(tx, world.EntityExplosionSource{
		Entity:        e,
		ExplosionSize: b.explosionSize,
	})
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
