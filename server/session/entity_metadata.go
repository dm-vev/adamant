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

	setActorFlag(m, protocol.EntityDataFlagHasGravity)
	setActorFlag(m, protocol.EntityDataFlagClimb)
	writeActorShape(e, networkID(e.H().Type()), m)
	if g, ok := e.H().Type().(glint); ok && g.Glint() {
		setActorFlag(m, protocol.EntityDataFlagEnchanted)
	}
	if e.H().Type() == entity.LingeringPotionType {
		setActorFlag(m, protocol.EntityDataFlagLingering)
	}
	addAmbientSoundMetadata(e, m)
	var seat world.RiderSeat
	if r, ok := e.(riderSeat); ok {
		seat = r.RiderSeat()
	}
	writeSeatMetadata(m, seat)
	s.addSpecificMetadata(e, m)
	if beh, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		s.addSpecificMetadata(beh.Behaviour(), m)
	}
	s.writeActorAppearance(e, m)
	return m
}

func (s *Session) addSpecificMetadata(e any, m protocol.EntityMetadata) {
	if sheep, ok := e.(*entity.Sheep); ok {
		m[protocol.EntityDataKeyColorIndex] = byte(sheep.WoolColor())
		if sheep.Sheared() {
			setActorFlag(m, protocol.EntityDataFlagSheared)
		}
	}
	if shulker, ok := e.(*entity.Shulker); ok {
		// BDS uses Variant for shell colour and separate attachment metadata.
		m[protocol.EntityDataKeyVariant] = int32(shulker.Color())
		m[protocol.EntityDataKeyPeekID] = int32(shulker.Peek())
		m[protocol.EntityDataKeyAttachFace] = byte(shulker.AttachFace())
		m[protocol.EntityDataKeyAttached] = int16(1)
		pos := cube.PosFromVec3(shulker.Position())
		m[protocol.EntityDataKeyAttachedPosition] = protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	}
	if parrot, ok := e.(*entity.Parrot); ok {
		m[protocol.EntityDataKeyVariant] = int32(parrot.Variant())
		if parrot.Tamed() {
			setActorFlag(m, protocol.EntityDataFlagTamed)
		}
		if parrot.Party() {
			setActorFlag(m, protocol.EntityDataFlagDancing)
		}
		if parrot.Sitting() {
			setActorFlag(m, protocol.EntityDataFlagSitting)
		}
	}
	if sn, ok := e.(sneaker); ok && sn.Sneaking() {
		setActorFlag(m, protocol.EntityDataFlagSneaking)
		if b, ok := e.(blocker); ok {
			if _, blocking := b.Blocking(); blocking {
				m.SetFlag(protocol.EntityDataKeyFlagsTwo, protocol.EntityDataFlagBlocking&63)
			}
		}
	}
	if sp, ok := e.(sprinter); ok && sp.Sprinting() {
		setActorFlag(m, protocol.EntityDataFlagSprinting)
	}
	if sw, ok := e.(swimmer); ok && sw.Swimming() {
		setActorFlag(m, protocol.EntityDataFlagSwimming)
	}
	if cr, ok := e.(crawler); ok && cr.Crawling() {
		m.SetFlag(protocol.EntityDataKeyFlagsTwo, protocol.EntityDataFlagCrawling&63)
	}
	if gl, ok := e.(glider); ok && gl.Gliding() {
		setActorFlag(m, protocol.EntityDataFlagGliding)
	}
	if bb, ok := e.(baby); ok && bb.Baby() {
		setActorFlag(m, protocol.EntityDataFlagBaby)
	}
	if l, ok := e.(interface{ InLove() bool }); ok && l.InLove() {
		setActorFlag(m, protocol.EntityDataFlagInLove)
	}
	if b, ok := e.(breather); ok {
		m[protocol.EntityDataKeyAirSupply] = int16(b.AirSupply().Milliseconds() / 50)
		m[protocol.EntityDataKeyAirSupplyMax] = int16(b.MaxAirSupply().Milliseconds() / 50)
		if b.Breathing() {
			setActorFlag(m, protocol.EntityDataFlagBreathing)
		}
	}
	if i, ok := e.(invisible); ok {
		if i.Invisible() {
			setActorFlag(m, protocol.EntityDataFlagInvisible)
		}
	} else if eff, ok := e.(effectBearer); ok && slices.ContainsFunc(eff.Effects(), func(ef effect.Effect) bool { return ef.Type() == effect.Invisibility }) {
		setActorFlag(m, protocol.EntityDataFlagInvisible)
	}
	if i, ok := e.(immobile); ok && i.Immobile() {
		setActorFlag(m, protocol.EntityDataFlagNoAI)
	}
	if o, ok := e.(onFire); ok && o.OnFireDuration() > 0 {
		setActorFlag(m, protocol.EntityDataFlagOnFire)
	}
	if f, ok := e.(interface{ FreezingStrength() float32 }); ok {
		m[protocol.EntityDataKeyFreezingEffectStrength] = f.FreezingStrength()
	}
	if sh, ok := e.(interface{ Shaking() bool }); ok && sh.Shaking() {
		setActorFlag(m, protocol.EntityDataFlagShaking)
	}
	if r, ok := e.(interface{ RamAttacking() bool }); ok && r.RamAttacking() {
		setActorFlag(m, protocol.EntityDataFlagRamAttack)
	}
	if sh, ok := e.(interface{ Shooting() bool }); ok && sh.Shooting() {
		// A breeze shooting faces its target to attack it from range.
		setActorFlag(m, protocol.EntityDataFlagFacingTargetToRangeAttack)
	}
	if j, ok := e.(interface{ JumpGoalJumping() bool }); ok && j.JumpGoalJumping() {
		setActorFlag(m, protocol.EntityDataFlagJumpGoal)
	}
	if sn, ok := e.(interface{ SnifferState() string }); ok {
		if flag, ok := snifferStateFlags[sn.SnifferState()]; ok {
			setActorFlag(m, flag)
		}
	}
	if p, ok := e.(interface{ PlayingDead() bool }); ok && p.PlayingDead() {
		setActorFlag(m, protocol.EntityDataFlagPlayingDead)
	}
	if c, ok := e.(interface{ Croaking() bool }); ok && c.Croaking() {
		setActorFlag(m, protocol.EntityDataFlagCroaking)
	}
	if c, ok := e.(interface{ EatingMob() bool }); ok && c.EatingMob() {
		// Bedrock's EAT_MOB flag shows a frog's tongue.
		setActorFlag(m, protocol.EntityDataFlagDigestMob)
	}
	if r, ok := e.(interface{ Resting() bool }); ok && r.Resting() {
		// Bats hang upside down while resting.
		setActorFlag(m, protocol.EntityDataFlagResting)
	}
	if db, ok := e.(doorBreaker); ok && db.CanBreakDoors() {
		setActorFlag(m, protocol.EntityDataFlagDoorBreaker)
	}
	if br, ok := e.(breakingObstruction); ok && br.BreakingObstruction() {
		setActorFlag(m, protocol.EntityDataFlagBreakingObstruction)
	}
	if ar, ok := e.(armsRaised); ok && ar.ArmsRaised() {
		setActorFlag(m, protocol.EntityDataFlagCharging)
	}
	if u, ok := e.(using); ok && u.UsingItem() {
		setActorFlag(m, protocol.EntityDataFlagUsingItem)
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[protocol.EntityDataKeyBedPosition] = protocol.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(protocol.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(arrow); ok && c.Critical() {
		setActorFlag(m, protocol.EntityDataFlagCritical)
	}
	if g, ok := e.(gameMode); ok {
		if g.GameMode().HasCollision() {
			setActorFlag(m, protocol.EntityDataFlagHasCollision)
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
		setActorFlag(m, protocol.EntityDataFlagIgnited)
	}
	if i, ok := e.(igniteable); ok && i.Ignited() {
		m[protocol.EntityDataKeyFuseTime] = int32(i.FuseTicks())
		setActorFlag(m, protocol.EntityDataFlagIgnited)
	}
	if p, ok := e.(interface{ Powered() bool }); ok && p.Powered() {
		setActorFlag(m, protocol.EntityDataFlagPowered)
	}
	if c, ok := e.(interface{ WallClimbing() bool }); ok && c.WallClimbing() {
		setActorFlag(m, protocol.EntityDataFlagWallClimbing)
	}
	// A blaze charging its fireballs burns, as Blaze.isOnFire shows it.
	if b, ok := e.(interface{ Burning() bool }); ok && b.Burning() {
		setActorFlag(m, protocol.EntityDataFlagOnFire)
	}
	if c, ok := e.(interface{ Charging() bool }); ok && c.Charging() {
		setActorFlag(m, protocol.EntityDataFlagCharging)
	}
	if c, ok := e.(interface{ CrossbowCharged() bool }); ok && c.CrossbowCharged() {
		setActorFlag(m, protocol.EntityDataFlagCharged)
	}
	if c, ok := e.(interface{ Celebrating() bool }); ok && c.Celebrating() {
		setActorFlag(m, protocol.EntityDataFlagCelebrating)
	}
	if c, ok := e.(interface{ CelebratingSpecial() bool }); ok && c.CelebratingSpecial() {
		setActorFlag(m, protocol.EntityDataFlagCelebratingSpecial)
	}
	if a, ok := e.(interface{ Admiring() bool }); ok && a.Admiring() {
		setActorFlag(m, protocol.EntityDataFlagAdmiring)
	}
	if c, ok := e.(interface {
		Casting() bool
		SpellColour() [3]float64
	}); ok && c.Casting() {
		setActorFlag(m, protocol.EntityDataFlagCasting)
		colour := c.SpellColour()
		m[protocol.EntityDataKeyDataSpellCastingColor] = int32(-1<<24 | int32(colour[0]*255)<<16 | int32(colour[1]*255)<<8 | int32(colour[2]*255))
	}
	if g, ok := e.(interface{ BeamTargetHandle() *world.EntityHandle }); ok {
		if h := g.BeamTargetHandle(); h != nil {
			m[protocol.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if g, ok := e.(interface{ ShootTargetHandle() *world.EntityHandle }); ok {
		m[protocol.EntityDataKeyTarget] = int64(0)
		if h := g.ShootTargetHandle(); h != nil {
			m[protocol.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if f, ok := e.(interface{ TongueTargetHandle() *world.EntityHandle }); ok {
		// The target is cleared once the tongue is back, as Frog.eraseTongueTarget.
		m[protocol.EntityDataKeyTarget] = int64(0)
		if h := f.TongueTargetHandle(); h != nil {
			m[protocol.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if r, ok := e.(interface {
		Stunned() bool
		Roaring() bool
	}); ok {
		if r.Stunned() {
			setActorFlag(m, protocol.EntityDataFlagStunned)
		}
		if r.Roaring() {
			setActorFlag(m, protocol.EntityDataFlagRoaring)
		}
	}
	if d, ok := e.(displayTile); ok {
		var display int32
		if b, ok := d.DisplayBlock(); ok {
			display = int32(networkBlockID(s.br, b))
		}
		m[protocol.EntityDataKeyDisplayBlockState] = display
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
		// BDS sends the damage of a vehicle as an int.
		m[protocol.EntityDataKeyStructuralIntegrity] = int32(h.Damage())
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
		m[protocol.EntityDataKeyControllingSeatIndex] = byte(c.ControllingSeatIndex())
	}
	if t, ok := e.(interactText); ok {
		if text := t.InteractText(); text != "" {
			m[protocol.EntityDataKeyInteractText] = text
		}
	}
	if r, ok := e.(rideTarget); ok {
		m[protocol.EntityDataKeyCanRideTarget] = byte(boolByte(r.CanRideTarget()))
	}
	if w, ok := e.(witherMeta); ok {
		// The heads of a wither turn to their targets, and it glows blue
		// while it is summoned.
		for i, h := range w.HeadTargets() {
			var id int64
			if h != nil {
				id = int64(s.handleRuntimeID(h))
			}
			m[[3]uint32{protocol.EntityDataKeyTargetA, protocol.EntityDataKeyTargetB, protocol.EntityDataKeyTargetC}[i]] = id
		}
		m[protocol.EntityDataKeyInvulnerableTicks] = int32(w.InvulnerableTicks())
	}
	if ec, ok := e.(endCrystalMeta); ok {
		if ec.ShowBase() {
			setActorFlag(m, protocol.EntityDataFlagShowBottom)
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
		m[protocol.EntityDataKeyHurt] = int32(iv.HurtDuration().Milliseconds() / 50)
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
			setActorFlag(m, flag)
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
		setActorFlag(m, protocol.EntityDataFlagShowName)
	} else {
		m.UnsetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagShowName)
	}
	if always {
		setActorFlag(m, protocol.EntityDataFlagAlwaysShowName)
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

// witherMeta is implemented by withers, whose heads turn to their targets.
type witherMeta interface {
	HeadTargets() [3]*world.EntityHandle
	InvulnerableTicks() int
}

type displayTile interface {
	DisplayBlock() (world.Block, bool)
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

type riderSeat interface {
	RiderSeat() world.RiderSeat
}

type rideTarget interface {
	CanRideTarget() bool
}

// writeSeatMetadata writes the seat an entity rides, or last rode, in. BDS
// sends these keys for every entity and sets them on riders, never on
// vehicles, and marks riding only through entity links. It leaves out the
// seat position of an entity that has not sat anywhere yet.
func writeSeatMetadata(m protocol.EntityMetadata, seat world.RiderSeat) {
	if seat.Position != (mgl64.Vec3{}) {
		m[protocol.EntityDataKeySeatOffset] = vec64To32(seat.Position)
	}
	m[protocol.EntityDataKeySeatLockPassengerRotation] = byte(boolByte(seat.LockRotation))
	m[protocol.EntityDataKeySeatLockPassengerRotationDegrees] = float32(seat.LockRotationDegrees)
	m[protocol.EntityDataKeySeatRotationOffset] = byte(boolByte(seat.RotateRider))
	m[protocol.EntityDataKeySeatRotationOffsetDegrees] = float32(seat.RotateRiderDegrees)
}

// snifferStateFlags holds the flag Bedrock shows for each state of a sniffer.
var snifferStateFlags = map[string]uint8{
	"feeling_happy": protocol.EntityDataFlagFeelingHappy,
	"scenting":      protocol.EntityDataFlagScenting,
	"sniffing":      protocol.EntityDataFlagSniffing,
	"searching":     protocol.EntityDataFlagSearching,
	"digging":       protocol.EntityDataFlagDigging,
	"rising":        protocol.EntityDataFlagRising,
}
