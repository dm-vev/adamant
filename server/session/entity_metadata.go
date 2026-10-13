package session

import (
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

// parseEntityMetadata returns an entity metadata object with default values. It is equivalent to setting
// all properties to their default values and disabling all flags.
func (s *Session) parseEntityMetadata(e world.Entity) protocol.EntityMetadata {
	bb := e.H().Type().BBox(e)
	m := protocol.NewEntityMetadata()

	m[protocol.EntityDataKeyWidth] = float32(bb.Width())
	m[protocol.EntityDataKeyHeight] = float32(bb.Height())
	m[protocol.EntityDataKeyEffectColor] = int32(0)
	m[protocol.EntityDataKeyEffectAmbience] = byte(0)
	m[protocol.EntityDataKeyColorIndex] = byte(0)

	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasGravity)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagClimb)
	if g, ok := e.H().Type().(glint); ok && g.Glint() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagEnchanted)
	}
	if e.H().Type() == entity.LingeringPotionType {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagLingering)
	}
	addAmbientSoundMetadata(e, m)
	s.addSpecificMetadata(e, m)
	if beh, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		s.addSpecificMetadata(beh.Behaviour(), m)
	}
	return m
}

type creakingState interface {
	CreakingState() (state int32, swayTicks int32)
}

func entityProperties(e world.Entity) protocol.EntityProperties {
	state, swayTicks, ok := creakingProperties(e)
	if !ok {
		if beh, hasBehaviour := e.(interface{ Behaviour() entity.Behaviour }); hasBehaviour {
			state, swayTicks, ok = creakingProperties(beh.Behaviour())
		}
	}
	if !ok {
		return protocol.EntityProperties{}
	}
	return protocol.EntityProperties{
		IntegerProperties: []protocol.IntegerEntityProperty{
			{Index: 0, Value: state},
			{Index: 1, Value: swayTicks},
		},
	}
}

func creakingProperties(e any) (state, swayTicks int32, ok bool) {
	c, ok := e.(creakingState)
	if !ok {
		return 0, 0, false
	}
	state, swayTicks = c.CreakingState()
	return state, swayTicks, true
}

func (s *Session) addSpecificMetadata(e any, m protocol.EntityMetadata) {
	if sheep, ok := e.(*entity.Sheep); ok {
		m[protocol.EntityDataKeyColorIndex] = byte(sheep.WoolColor())
		if sheep.Sheared() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSheared)
		}
	}
	if parrot, ok := e.(*entity.Parrot); ok {
		m[protocol.EntityDataKeyVariant] = int32(parrot.Variant())
		if parrot.Tamed() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagTamed)
		}
		if parrot.Party() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagDancing)
		}
		if parrot.Sitting() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSitting)
		}
	}
	if sn, ok := e.(sneaker); ok && sn.Sneaking() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSneaking)
		if b, ok := e.(blocker); ok {
			if _, blocking := b.Blocking(); blocking {
				m.SetFlag(protocol.EntityDataKeyFlagsTwo, protocol.EntityDataFlagBlocking&63)
			}
		}
	}
	if sp, ok := e.(sprinter); ok && sp.Sprinting() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSprinting)
	}
	if sw, ok := e.(swimmer); ok && sw.Swimming() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSwimming)
	}
	if cr, ok := e.(crawler); ok && cr.Crawling() {
		m.SetFlag(protocol.EntityDataKeyFlagsTwo, protocol.EntityDataFlagCrawling&63)
	}
	if gl, ok := e.(glider); ok && gl.Gliding() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagGliding)
	}
	if bb, ok := e.(baby); ok && bb.Baby() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagBaby)
	}
	if l, ok := e.(interface{ InLove() bool }); ok && l.InLove() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagInLove)
	}
	if b, ok := e.(breather); ok {
		m[protocol.EntityDataKeyAirSupply] = int16(b.AirSupply().Milliseconds() / 50)
		m[protocol.EntityDataKeyAirSupplyMax] = int16(b.MaxAirSupply().Milliseconds() / 50)
		if b.Breathing() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagBreathing)
		}
	}
	if i, ok := e.(invisible); ok {
		if i.Invisible() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagInvisible)
		}
	} else if eff, ok := e.(effectBearer); ok && slices.ContainsFunc(eff.Effects(), func(ef effect.Effect) bool { return ef.Type() == effect.Invisibility }) {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagInvisible)
	}
	if i, ok := e.(immobile); ok && i.Immobile() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagNoAI)
	}
	if o, ok := e.(onFire); ok && o.OnFireDuration() > 0 {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagOnFire)
	}
	if f, ok := e.(interface{ FreezingStrength() float32 }); ok {
		m[protocol.EntityDataKeyFreezingEffectStrength] = f.FreezingStrength()
	}
	if sh, ok := e.(interface{ Shaking() bool }); ok && sh.Shaking() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagShaking)
	}
	if db, ok := e.(doorBreaker); ok && db.CanBreakDoors() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagDoorBreaker)
	}
	if br, ok := e.(breakingObstruction); ok && br.BreakingObstruction() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagBreakingObstruction)
	}
	if ar, ok := e.(armsRaised); ok && ar.ArmsRaised() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagCharging)
	}
	if u, ok := e.(using); ok && u.UsingItem() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagUsingItem)
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[protocol.EntityDataKeyBedPosition] = protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(protocol.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(arrow); ok && c.Critical() {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagCritical)
	}
	if g, ok := e.(gameMode); ok {
		if g.GameMode().HasCollision() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasCollision)
		}
	}
	if o, ok := e.(orb); ok {
		m[protocol.EntityDataKeyValue] = int32(o.Experience())
	}
	if f, ok := e.(firework); ok {
		m[protocol.EntityDataKeyDisplayFirework] = item.WriteNBT(item.NewStack(f.Firework(), 1), false)
		if o, ok := e.(owned); ok && f.Attached() && o.Owner() != nil {
			m[protocol.EntityDataKeyCustomDisplay] = s.handleUniqueID(o.Owner())
		}
	} else if o, ok := e.(owned); ok && o.Owner() != nil {
		m[protocol.EntityDataKeyOwner] = s.handleUniqueID(o.Owner())
	}
	if l, ok := e.(leashed); ok {
		if id := l.HolderLeashID(); id != 0 {
			if s.ent != nil && id == s.ent.UniqueID() {
				id = int64(selfEntityRuntimeID)
			}
			m[protocol.EntityDataKeyLeashHolder] = id
		}
	}
	if sc, ok := e.(scaled); ok {
		m[protocol.EntityDataKeyScale] = float32(sc.Scale())
	}
	if t, ok := e.(tnt); ok {
		m[protocol.EntityDataKeyFuseTime] = int32(t.Fuse().Milliseconds() / 50)
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagIgnited)
	}
	if i, ok := e.(igniteable); ok && i.Ignited() {
		m[protocol.EntityDataKeyFuseTime] = int32(i.FuseTicks())
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagIgnited)
	}
	if d, ok := e.(displayTile); ok {
		m[protocol.EntityDataKeyDisplayBlockState] = d.DisplayTile()
		m[protocol.EntityDataKeyDisplayOffset] = int32(d.DisplayOffset())
		m[protocol.EntityDataKeyCustomDisplay] = byte(boolByte(d.CustomDisplay()))
	}
	if c, ok := e.(containerMeta); ok {
		// The dedicated server sends the container type as a byte.
		m[protocol.EntityDataKeyContainerType] = byte(c.ContainerType())
		m[protocol.EntityDataKeyContainerSize] = c.ContainerSize()
		m[protocol.EntityDataKeyContainerStrengthModifier] = c.ContainerStrengthModifier()
	}
	if h, ok := e.(minecartDamage); ok {
		m[protocol.EntityDataKeyHurt] = int32(h.RollingAmplitude())
		m[protocol.EntityDataKeyHurtDirection] = int32(h.RollingDirection())
		m[protocol.EntityDataKeyStructuralIntegrity] = float32(h.Damage())
	}
	if r, ok := e.(rowTime); ok {
		m[protocol.EntityDataKeyRowTimeLeft] = r.RowTimeLeft()
		m[protocol.EntityDataKeyRowTimeRight] = r.RowTimeRight()
	}
	if b, ok := e.(buoyantMeta); ok {
		m[protocol.EntityDataKeyIsBuoyant] = byte(boolByte(b.Buoyant()))
		if data := b.BuoyancyData(); data != "" {
			m[protocol.EntityDataKeyBuoyancyData] = data
		}
	}
	if c, ok := e.(controllingSeat); ok {
		m[protocol.EntityDataKeyControllingSeatIndex] = c.ControllingSeatIndex()
	}
	if t, ok := e.(interactText); ok {
		if text := t.InteractText(); text != "" {
			m[protocol.EntityDataKeyInteractText] = text
		}
	}
	if r, ok := e.(rider); ok && r.Riding() != nil {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagRiding)
	}
	if r, ok := e.(rideTarget); ok {
		m[protocol.EntityDataKeyCanRideTarget] = byte(boolByte(r.CanRideTarget()))
	}
	if s, ok := e.(seatOffset); ok {
		m[protocol.EntityDataKeySeatOffset] = s.SeatOffset()
	}
	if ec, ok := e.(endCrystalMeta); ok {
		if ec.ShowBase() {
			m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagShowBottom)
		}
		if target, ok := ec.BeamTarget(); ok {
			m[protocol.EntityDataKeyBlockTarget] = protocol.BlockPos{
				int32(math.Floor(target[0])),
				int32(math.Floor(target[1])),
				int32(math.Floor(target[2])),
			}
		}
	}
	if nameTag, alwaysShow, ok := nameTagState(e); ok {
		writeNameTagMetadata(m, nameTag, alwaysShow)
	}
	if sc, ok := e.(scoreTag); ok {
		m[protocol.EntityDataKeyScore] = sc.ScoreTag()
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[protocol.EntityDataKeyBedPosition] = protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(protocol.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(areaEffectCloud); ok {
		m[protocol.EntityDataKeyDataRadius] = float32(c.Radius())

		// We purposely fill these in with invalid values to disable the client-sided shrinking of the cloud.
		m[protocol.EntityDataKeyDataDuration] = int32(math.MaxInt32)
		m[protocol.EntityDataKeyDataChangeOnPickup] = float32(math.SmallestNonzeroFloat32)
		m[protocol.EntityDataKeyDataChangeRate] = float32(math.SmallestNonzeroFloat32)

		colour, am := effect.ResultingColour(c.Effects())
		m[protocol.EntityDataKeyEffectColor] = nbtconv.Int32FromRGBA(colour)
		if am {
			m[protocol.EntityDataKeyEffectAmbience] = byte(1)
		} else {
			m[protocol.EntityDataKeyEffectAmbience] = byte(0)
		}
	}

	if l, ok := e.(living); ok && s.ent != nil && s.ent.UUID() == l.UUID() {
		deathPos, deathDimension, died := l.DeathPosition()
		if died {
			dim, _ := world.DimensionID(deathDimension)
			m[protocol.EntityDataKeyPlayerLastDeathPosition] = vec64To32(deathPos)
			m[protocol.EntityDataKeyPlayerLastDeathDimension] = int32(dim)
		}
		m[protocol.EntityDataKeyPlayerHasDied] = boolByte(died)
	}
	if p, ok := e.(splash); ok {
		m[protocol.EntityDataKeyAuxValueData] = int16(p.Potion().Uint8())
		if tip := p.Potion().Uint8(); tip > 4 {
			m[protocol.EntityDataKeyCustomDisplay] = tip + 1
		}
	}
	if eff, ok := e.(effectBearer); ok {
		var packedEffects int64

		for _, ef := range eff.Effects() {
			if !ef.ParticlesHidden() {
				id, found := effect.ID(ef.Type())
				if !found {
					continue
				}
				packedEffects = (packedEffects << 7) | int64(id<<1)
				if ef.Ambient() {
					packedEffects |= 1
				}
			}
		}
		m[protocol.EntityDataKeyVisibleMobEffects] = packedEffects
	}
	if v, ok := e.(variable); ok {
		m[protocol.EntityDataKeyVariant] = v.Variant()
	}
	if mv, ok := e.(markVariable); ok {
		m[protocol.EntityDataKeyMarkVariant] = mv.MarkVariant()
	}
	if mount, ok := e.(interface{ MountState() entity.MountState }); ok {
		addMountMetadata(mount.MountState(), m)
	}
	if iv, ok := e.(hurtDuration); ok {
		m[protocol.EntityDataKeyHurt] = uint8(iv.HurtDuration().Milliseconds() / 50)
	}
	if pi, ok := e.(armourStand); ok {
		m[protocol.EntityDataKeyPoseIndex] = int32(pi.PoseIndex())
	}
}

// addMountMetadata writes the flags and data Bedrock clients show of horses,
// llamas and camels, as the dedicated server sends them.
func addMountMetadata(s entity.MountState, m protocol.EntityMetadata) {
	for flag, set := range map[uint8]bool{protocol.EntityDataFlagSaddled: s.Saddled, protocol.EntityDataFlagTamed: s.Tamed, protocol.EntityDataFlagChested: s.Chested, protocol.EntityDataFlagSitting: s.Sitting,
		protocol.EntityDataFlagKeyboardControlled: s.Controlled, protocol.EntityDataFlagPowerJump: s.PowerJump, protocol.EntityDataFlagDash: s.Dash} {
		if set {
			m.SetFlag(protocol.EntityDataKeyFlags, flag)
		}
	}
	if s.Rearing {
		m[protocol.EntityDataKeyHorseFlags] = s.HorseFlags
	}
	if s.ContainerSize > 0 {
		m[protocol.EntityDataKeyContainerType] = byte(protocol.ContainerTypeHorse)
		m[protocol.EntityDataKeyContainerSize] = s.ContainerSize
		m[protocol.EntityDataKeyContainerStrengthModifier] = s.StrengthModifier
	}
	if s.MaxStrength > 0 {
		m[protocol.EntityDataKeyStrength] = s.Strength
		m[protocol.EntityDataKeyStrengthMax] = s.MaxStrength
	}
}

// nameTagState returns the public name tag of an entity, whether that name tag is shown at all distances
// and whether the entity has a name tag at all. Entities that do not report an always show state show
// their name tag at all distances.
func nameTagState(e any) (string, bool, bool) {
	alwaysShow := true
	if a, ok := e.(alwaysShowNameTag); ok {
		alwaysShow = a.AlwaysShowNameTag()
	}
	n, ok := e.(named)
	if !ok {
		return "", alwaysShow, false
	}
	return n.NameTag(), alwaysShow, true
}

// writeNameTagMetadata writes a name tag and its related visibility properties to metadata.
func writeNameTagMetadata(m protocol.EntityMetadata, nameTag string, alwaysShow bool) {
	show := nameTag != ""
	always := show && alwaysShow

	m[protocol.EntityDataKeyName] = nameTag
	m[protocol.EntityDataKeyAlwaysShowNameTag] = boolByte(always)
	if show {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagShowName)
	} else {
		m.UnsetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagShowName)
	}
	if always {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagAlwaysShowName)
	} else {
		m.UnsetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagAlwaysShowName)
	}
}

type sneaker interface {
	Sneaking() bool
}

type sprinter interface {
	Sprinting() bool
}

type swimmer interface {
	Swimming() bool
}

type crawler interface {
	Crawling() bool
}

type glider interface {
	Gliding() bool
}

type blocker interface {
	Blocking() (bool, bool)
}

type breather interface {
	Breathing() bool
	AirSupply() time.Duration
	MaxAirSupply() time.Duration
}

type immobile interface {
	Immobile() bool
}

type invisible interface {
	Invisible() bool
}

type scaled interface {
	Scale() float64
}

type owned interface {
	Owner() *world.EntityHandle
}

// leashed is an entity held by a lead; HolderLeashID is the holder's unique ID.
type leashed interface {
	HolderLeashID() int64
}

type named interface {
	NameTag() string
}

type alwaysShowNameTag interface {
	AlwaysShowNameTag() bool
}

type scoreTag interface {
	ScoreTag() string
}

type splash interface {
	Potion() potion.Potion
}

type glint interface {
	Glint() bool
}

type areaEffectCloud interface {
	effectBearer
	Radius() float64
}

type onFire interface {
	OnFireDuration() time.Duration
}

type baby interface {
	Baby() bool
}

type doorBreaker interface {
	CanBreakDoors() bool
}

type breakingObstruction interface {
	BreakingObstruction() bool
}

type armsRaised interface {
	ArmsRaised() bool
}

type effectBearer interface {
	Effects() []effect.Effect
}

type using interface {
	UsingItem() bool
}

type sleeper interface {
	Sleeping() (cube.Pos, bool)
}

type arrow interface {
	Critical() bool
}

type orb interface {
	Experience() int
}

type firework interface {
	Firework() item.Firework
	Attached() bool
}

type gameMode interface {
	GameMode() world.GameMode
}

type tnt interface {
	Fuse() time.Duration
}

type igniteable interface {
	Ignited() bool
	FuseTicks() int
}

type living interface {
	UUID() uuid.UUID
	DeathPosition() (mgl64.Vec3, world.Dimension, bool)
}

type variable interface {
	Variant() int32
}

type markVariable interface {
	MarkVariant() int32
}

type armourStand interface {
	PoseIndex() int
}

type hurtDuration interface {
	HurtDuration() time.Duration
}

type endCrystalMeta interface {
	ShowBase() bool
	BeamTarget() (mgl64.Vec3, bool)
}

type displayTile interface {
	DisplayTile() int32
	DisplayOffset() int
	CustomDisplay() bool
}

type containerMeta interface {
	ContainerType() int32
	ContainerSize() int32
	ContainerStrengthModifier() int32
}

type minecartDamage interface {
	RollingAmplitude() int
	RollingDirection() int
	Damage() float64
}

type rowTime interface {
	RowTimeLeft() float32
	RowTimeRight() float32
}

type buoyantMeta interface {
	Buoyant() bool
	BuoyancyData() string
}

type controllingSeat interface {
	ControllingSeatIndex() int32
}

type interactText interface {
	InteractText() string
}

type rider interface {
	Riding() *world.EntityHandle
}

type rideTarget interface {
	CanRideTarget() bool
}

type seatOffset interface {
	SeatOffset() mgl32.Vec3
}
