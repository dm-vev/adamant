package session

import (
	"math"
	"slices"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// parseEntityMetadata returns an entity metadata object with default values. It is equivalent to setting
// all properties to their default values and disabling all flags.
func (s *Session) parseEntityMetadata(e world.Entity) mc.Metadata {
	bb := e.H().Type().BBox(e)
	m := mc.NewMetadata()

	m[mc.EntityDataKeyWidth] = float32(bb.Width())
	m[mc.EntityDataKeyHeight] = float32(bb.Height())
	m[mc.EntityDataKeyEffectColor] = int32(0)
	m[mc.EntityDataKeyEffectAmbience] = byte(0)
	m[mc.EntityDataKeyColorIndex] = byte(0)

	setActorFlag(m, mc.EntityDataFlagHasGravity)
	setActorFlag(m, mc.EntityDataFlagClimb)
	writeActorShape(e, networkID(e.H().Type()), m)
	if g, ok := e.H().Type().(glint); ok && g.Glint() {
		setActorFlag(m, mc.EntityDataFlagEnchanted)
	}
	if e.H().Type() == entity.LingeringPotionType {
		setActorFlag(m, mc.EntityDataFlagLingering)
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

func (s *Session) addSpecificMetadata(e any, m mc.Metadata) {
	if sheep, ok := e.(*entity.Sheep); ok {
		m[mc.EntityDataKeyColorIndex] = byte(sheep.WoolColor())
		if sheep.Sheared() {
			setActorFlag(m, mc.EntityDataFlagSheared)
		}
	}
	if shulker, ok := e.(*entity.Shulker); ok {
		// BDS uses Variant for shell colour and separate attachment metadata.
		m[mc.EntityDataKeyVariant] = int32(shulker.Color())
		m[mc.EntityDataKeyPeekID] = int32(shulker.Peek())
		m[mc.EntityDataKeyAttachFace] = byte(shulker.AttachFace())
		m[mc.EntityDataKeyAttached] = int16(1)
		pos := cube.PosFromVec3(shulker.Position())
		m[mc.EntityDataKeyAttachedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	}
	if parrot, ok := e.(*entity.Parrot); ok {
		m[mc.EntityDataKeyVariant] = int32(parrot.Variant())
		if parrot.Tamed() {
			setActorFlag(m, mc.EntityDataFlagTamed)
		}
		if parrot.Party() {
			setActorFlag(m, mc.EntityDataFlagDancing)
		}
		if parrot.Sitting() {
			setActorFlag(m, mc.EntityDataFlagSitting)
		}
	}
	if sn, ok := e.(sneaker); ok && sn.Sneaking() {
		setActorFlag(m, mc.EntityDataFlagSneaking)
		if b, ok := e.(blocker); ok {
			if _, blocking := b.Blocking(); blocking {
				m.SetFlag(mc.EntityDataKeyFlagsTwo, mc.EntityDataFlagBlocking&63)
			}
		}
	}
	if sp, ok := e.(sprinter); ok && sp.Sprinting() {
		setActorFlag(m, mc.EntityDataFlagSprinting)
	}
	if sw, ok := e.(swimmer); ok && sw.Swimming() {
		setActorFlag(m, mc.EntityDataFlagSwimming)
	}
	if cr, ok := e.(crawler); ok && cr.Crawling() {
		m.SetFlag(mc.EntityDataKeyFlagsTwo, mc.EntityDataFlagCrawling&63)
	}
	if gl, ok := e.(glider); ok && gl.Gliding() {
		setActorFlag(m, mc.EntityDataFlagGliding)
	}
	if bb, ok := e.(baby); ok && bb.Baby() {
		setActorFlag(m, mc.EntityDataFlagBaby)
	}
	if l, ok := e.(interface{ InLove() bool }); ok && l.InLove() {
		setActorFlag(m, mc.EntityDataFlagInLove)
	}
	if b, ok := e.(breather); ok {
		m[mc.EntityDataKeyAirSupply] = int16(b.AirSupply().Milliseconds() / 50)
		m[mc.EntityDataKeyAirSupplyMax] = int16(b.MaxAirSupply().Milliseconds() / 50)
		if b.Breathing() {
			setActorFlag(m, mc.EntityDataFlagBreathing)
		}
	}
	if i, ok := e.(invisible); ok {
		if i.Invisible() {
			setActorFlag(m, mc.EntityDataFlagInvisible)
		}
	} else if eff, ok := e.(effectBearer); ok && slices.ContainsFunc(eff.Effects(), func(ef effect.Effect) bool { return ef.Type() == effect.Invisibility }) {
		setActorFlag(m, mc.EntityDataFlagInvisible)
	}
	if i, ok := e.(immobile); ok && i.Immobile() {
		setActorFlag(m, mc.EntityDataFlagNoAI)
	}
	if o, ok := e.(onFire); ok && o.OnFireDuration() > 0 {
		setActorFlag(m, mc.EntityDataFlagOnFire)
	}
	if f, ok := e.(interface{ FreezingStrength() float32 }); ok {
		m[mc.EntityDataKeyFreezingEffectStrength] = f.FreezingStrength()
	}
	if sh, ok := e.(interface{ Shaking() bool }); ok && sh.Shaking() {
		setActorFlag(m, mc.EntityDataFlagShaking)
	}
	if r, ok := e.(interface{ RamAttacking() bool }); ok && r.RamAttacking() {
		setActorFlag(m, mc.EntityDataFlagRamAttack)
	}
	if w, ok := e.(interface {
		WardenPose() (emerging, digging, roaring, sniffing, sonicBoom bool)
		HeartbeatInterval() int32
	}); ok {
		emerging, digging, roaring, sniffing, sonicBoom := w.WardenPose()
		for flag, set := range map[uint8]bool{mc.EntityDataFlagEmerging: emerging, mc.EntityDataFlagDigging: digging,
			mc.EntityDataFlagRoaring: roaring, mc.EntityDataFlagSniffing: sniffing, mc.EntityDataFlagSonicBoom: sonicBoom} {
			if set {
				setActorFlag(m, flag)
			}
		}
		m[mc.EntityDataKeyHeartbeatIntervalTicks] = w.HeartbeatInterval()
		// LevelSoundEvent heartbeat.
		m[mc.EntityDataKeyHeartbeatSoundEvent] = int32(376)
	}
	if sh, ok := e.(interface{ Shooting() bool }); ok && sh.Shooting() {
		// A breeze shooting faces its target to attack it from range.
		setActorFlag(m, mc.EntityDataFlagFacingTargetToRangeAttack)
	}
	if j, ok := e.(interface{ JumpGoalJumping() bool }); ok && j.JumpGoalJumping() {
		setActorFlag(m, mc.EntityDataFlagJumpGoal)
	}
	if sn, ok := e.(interface{ SnifferState() string }); ok {
		if flag, ok := snifferStateFlags[sn.SnifferState()]; ok {
			setActorFlag(m, flag)
		}
	}
	if p, ok := e.(interface{ PlayingDead() bool }); ok && p.PlayingDead() {
		setActorFlag(m, mc.EntityDataFlagPlayingDead)
	}
	if c, ok := e.(interface{ Croaking() bool }); ok && c.Croaking() {
		setActorFlag(m, mc.EntityDataFlagCroaking)
	}
	if c, ok := e.(interface{ EatingMob() bool }); ok && c.EatingMob() {
		// Bedrock's EAT_MOB flag shows a frog's tongue.
		setActorFlag(m, mc.EntityDataFlagDigestMob)
	}
	if r, ok := e.(interface{ Resting() bool }); ok && r.Resting() {
		// Bats hang upside down while resting.
		setActorFlag(m, mc.EntityDataFlagResting)
	}
	if db, ok := e.(doorBreaker); ok && db.CanBreakDoors() {
		setActorFlag(m, mc.EntityDataFlagDoorBreaker)
	}
	if br, ok := e.(breakingObstruction); ok && br.BreakingObstruction() {
		setActorFlag(m, mc.EntityDataFlagBreakingObstruction)
	}
	if ar, ok := e.(armsRaised); ok && ar.ArmsRaised() {
		setActorFlag(m, mc.EntityDataFlagCharging)
	}
	if u, ok := e.(using); ok && u.UsingItem() {
		setActorFlag(m, mc.EntityDataFlagUsingItem)
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[mc.EntityDataKeyBedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(mc.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(arrow); ok && c.Critical() {
		setActorFlag(m, mc.EntityDataFlagCritical)
	}
	if g, ok := e.(gameMode); ok {
		if g.GameMode().HasCollision() {
			setActorFlag(m, mc.EntityDataFlagHasCollision)
		}
	}
	if o, ok := e.(orb); ok {
		m[mc.EntityDataKeyValue] = int32(o.Experience())
	}
	if f, ok := e.(firework); ok {
		m[mc.EntityDataKeyDisplayFirework] = networkItemNBT(item.NewStack(f.Firework(), 1))
		if o, ok := e.(owned); ok && f.Attached() && o.Owner() != nil {
			m[mc.EntityDataKeyCustomDisplay] = s.handleUniqueID(o.Owner())
		}
	} else if o, ok := e.(owned); ok && o.Owner() != nil {
		m[mc.EntityDataKeyOwner] = s.handleUniqueID(o.Owner())
	}
	if l, ok := e.(leashed); ok {
		if id := l.HolderLeashID(); id != 0 {
			if s.ent != nil && id == s.ent.UniqueID() {
				id = int64(selfEntityRuntimeID)
			}
			m[mc.EntityDataKeyLeashHolder] = id
		}
	}
	if sc, ok := e.(scaled); ok {
		m[mc.EntityDataKeyScale] = float32(sc.Scale())
	}
	if t, ok := e.(tnt); ok {
		m[mc.EntityDataKeyFuseTime] = int32(t.Fuse().Milliseconds() / 50)
		setActorFlag(m, mc.EntityDataFlagIgnited)
	}
	if i, ok := e.(igniteable); ok && i.Ignited() {
		m[mc.EntityDataKeyFuseTime] = int32(i.FuseTicks())
		setActorFlag(m, mc.EntityDataFlagIgnited)
	}
	if p, ok := e.(interface{ Powered() bool }); ok && p.Powered() {
		setActorFlag(m, mc.EntityDataFlagPowered)
	}
	if c, ok := e.(interface{ WallClimbing() bool }); ok && c.WallClimbing() {
		setActorFlag(m, mc.EntityDataFlagWallClimbing)
	}
	// A blaze charging its fireballs burns, as Blaze.isOnFire shows it.
	if b, ok := e.(interface{ Burning() bool }); ok && b.Burning() {
		setActorFlag(m, mc.EntityDataFlagOnFire)
	}
	if c, ok := e.(interface{ Charging() bool }); ok && c.Charging() {
		setActorFlag(m, mc.EntityDataFlagCharging)
	}
	if c, ok := e.(interface{ CrossbowCharged() bool }); ok && c.CrossbowCharged() {
		setActorFlag(m, mc.EntityDataFlagCharged)
	}
	if c, ok := e.(interface{ Celebrating() bool }); ok && c.Celebrating() {
		setActorFlag(m, mc.EntityDataFlagCelebrating)
	}
	if c, ok := e.(interface{ CelebratingSpecial() bool }); ok && c.CelebratingSpecial() {
		setActorFlag(m, mc.EntityDataFlagCelebratingSpecial)
	}
	if a, ok := e.(interface{ Admiring() bool }); ok && a.Admiring() {
		setActorFlag(m, mc.EntityDataFlagAdmiring)
	}
	if c, ok := e.(interface {
		Casting() bool
		SpellColour() [3]float64
	}); ok && c.Casting() {
		setActorFlag(m, mc.EntityDataFlagCasting)
		colour := c.SpellColour()
		m[mc.EntityDataKeyDataSpellCastingColor] = int32(-1<<24 | int32(colour[0]*255)<<16 | int32(colour[1]*255)<<8 | int32(colour[2]*255))
	}
	if g, ok := e.(interface{ BeamTargetHandle() *world.EntityHandle }); ok {
		if h := g.BeamTargetHandle(); h != nil {
			m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if g, ok := e.(interface{ ShootTargetHandle() *world.EntityHandle }); ok {
		m[mc.EntityDataKeyTarget] = int64(0)
		if h := g.ShootTargetHandle(); h != nil {
			m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if f, ok := e.(interface{ TongueTargetHandle() *world.EntityHandle }); ok {
		// The target is cleared once the tongue is back, as Frog.eraseTongueTarget.
		m[mc.EntityDataKeyTarget] = int64(0)
		if h := f.TongueTargetHandle(); h != nil {
			m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if r, ok := e.(interface {
		Stunned() bool
		Roaring() bool
	}); ok {
		if r.Stunned() {
			setActorFlag(m, mc.EntityDataFlagStunned)
		}
		if r.Roaring() {
			setActorFlag(m, mc.EntityDataFlagRoaring)
		}
	}
	if d, ok := e.(displayTile); ok {
		var display int32
		if b, ok := d.DisplayBlock(); ok {
			display = int32(s.blockHash(b))
		}
		m[mc.EntityDataKeyDisplayBlockState] = display
		m[mc.EntityDataKeyDisplayOffset] = int32(d.DisplayOffset())
		m[mc.EntityDataKeyCustomDisplay] = byte(boolByte(d.CustomDisplay()))
	}
	if c, ok := e.(containerMeta); ok {
		if typ, ok := actorContainerType(c.ContainerKind()); ok {
			// The dedicated server sends the container type as a byte.
			m[mc.EntityDataKeyContainerType] = typ
			m[mc.EntityDataKeyContainerSize] = c.ContainerSize()
			m[mc.EntityDataKeyContainerStrengthModifier] = c.ContainerStrengthModifier()
		}
	}
	if h, ok := e.(minecartDamage); ok {
		m[mc.EntityDataKeyHurt] = int32(h.RollingAmplitude())
		m[mc.EntityDataKeyHurtDirection] = int32(h.RollingDirection())
		// BDS sends the damage of a vehicle as an int.
		m[mc.EntityDataKeyStructuralIntegrity] = int32(h.Damage())
	}
	if r, ok := e.(rowTime); ok {
		m[mc.EntityDataKeyRowTimeLeft] = r.RowTimeLeft()
		m[mc.EntityDataKeyRowTimeRight] = r.RowTimeRight()
	}
	if b, ok := e.(buoyantMeta); ok {
		m[mc.EntityDataKeyIsBuoyant] = byte(boolByte(b.Buoyant()))
		if b.Buoyant() {
			m[mc.EntityDataKeyBuoyancyData] = buoyancyData
			// The driver of a buoyant vehicle sits in its first seat.
			m[mc.EntityDataKeyControllingSeatIndex] = byte(0)
		}
	}
	if text := interactText(e); text != "" {
		m[mc.EntityDataKeyInteractText] = text
	}
	if r, ok := e.(rideTarget); ok {
		m[mc.EntityDataKeyCanRideTarget] = byte(boolByte(r.CanRideTarget()))
	}
	if w, ok := e.(witherMeta); ok {
		// The heads of a wither turn to their targets, and it glows blue
		// while it is summoned.
		for i, h := range w.HeadTargets() {
			var id int64
			if h != nil {
				id = int64(s.handleRuntimeID(h))
			}
			m[[3]uint32{mc.EntityDataKeyTargetA, mc.EntityDataKeyTargetB, mc.EntityDataKeyTargetC}[i]] = id
		}
		m[mc.EntityDataKeyInvulnerableTicks] = int32(w.InvulnerableTicks())
	}
	if ec, ok := e.(endCrystalMeta); ok {
		if ec.ShowBase() {
			setActorFlag(m, mc.EntityDataFlagShowBottom)
		}
		if target, ok := ec.BeamTarget(); ok {
			m[mc.EntityDataKeyBlockTarget] = mc.BlockPos{
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
		m[mc.EntityDataKeyScore] = sc.ScoreTag()
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[mc.EntityDataKeyBedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(mc.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(areaEffectCloud); ok {
		m[mc.EntityDataKeyDataRadius] = float32(c.Radius())

		// We purposely fill these in with invalid values to disable the client-sided shrinking of the cloud.
		m[mc.EntityDataKeyDataDuration] = int32(math.MaxInt32)
		m[mc.EntityDataKeyDataChangeOnPickup] = float32(math.SmallestNonzeroFloat32)
		m[mc.EntityDataKeyDataChangeRate] = float32(math.SmallestNonzeroFloat32)
		if p, ok := e.(interface{ Particle() entity.CloudParticle }); ok {
			if particle, ok := cloudParticleType(p.Particle()); ok {
				m[mc.EntityDataKeyDataParticle] = particle
			}
		}

		colour, am := effect.ResultingColour(c.Effects())
		m[mc.EntityDataKeyEffectColor] = int32FromRGBA(colour)
		if am {
			m[mc.EntityDataKeyEffectAmbience] = byte(1)
		} else {
			m[mc.EntityDataKeyEffectAmbience] = byte(0)
		}
	}

	if l, ok := e.(living); ok && s.ent != nil && s.ent.UUID() == l.UUID() {
		deathPos, deathDimension, died := l.DeathPosition()
		if died {
			dim, _ := world.DimensionID(deathDimension)
			m[mc.EntityDataKeyPlayerLastDeathPosition] = vec64To32(deathPos)
			m[mc.EntityDataKeyPlayerLastDeathDimension] = int32(dim)
		}
		m[mc.EntityDataKeyPlayerHasDied] = boolByte(died)
	}
	if p, ok := e.(splash); ok {
		m[mc.EntityDataKeyAuxValueData] = int16(p.Potion().Uint8())
		if tip := p.Potion().Uint8(); tip > 4 {
			m[mc.EntityDataKeyCustomDisplay] = tip + 1
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
		m[mc.EntityDataKeyVisibleMobEffects] = packedEffects
	}
	if v, ok := e.(variable); ok {
		m[mc.EntityDataKeyVariant] = v.Variant()
	}
	if mv, ok := e.(markVariable); ok {
		m[mc.EntityDataKeyMarkVariant] = mv.MarkVariant()
	}
	if mount, ok := e.(interface{ MountState() entity.MountState }); ok {
		addMountMetadata(mount.MountState(), m)
	}
	if iv, ok := e.(hurtDuration); ok {
		m[mc.EntityDataKeyHurt] = int32(iv.HurtDuration().Milliseconds() / 50)
	}
	if pi, ok := e.(armourStand); ok {
		m[mc.EntityDataKeyPoseIndex] = int32(pi.PoseIndex())
	}
}

// The bits of the horse flags of the actor data of mounts.
const (
	horseFlagEating   = 32
	horseFlagStanding = 128
)

// addMountMetadata writes the flags and data Bedrock clients show of horses,
// llamas and camels, as the dedicated server sends them.
func addMountMetadata(s entity.MountState, m mc.Metadata) {
	for flag, set := range map[uint8]bool{mc.EntityDataFlagSaddled: s.Saddled, mc.EntityDataFlagTamed: s.Tamed, mc.EntityDataFlagChested: s.Chested, mc.EntityDataFlagSitting: s.Sitting,
		mc.EntityDataFlagKeyboardControlled: s.Controlled, mc.EntityDataFlagPowerJump: s.PowerJump, mc.EntityDataFlagDash: s.Dash} {
		if set {
			setActorFlag(m, flag)
		}
	}
	if s.Rearing {
		var flags int64
		if s.Eating {
			flags |= horseFlagEating
		}
		if s.Standing {
			flags |= horseFlagStanding
		}
		m[mc.EntityDataKeyHorseFlags] = flags
	}
	if s.ContainerSize > 0 {
		m[mc.EntityDataKeyContainerType] = byte(mc.ContainerTypeHorse)
		m[mc.EntityDataKeyContainerSize] = s.ContainerSize
		// BDS sends a strength modifier of 3 for tamed llamas, the only
		// mounts with a strength, and 0 for the others.
		var modifier int32
		if s.MaxStrength != 0 {
			modifier = 3
		}
		m[mc.EntityDataKeyContainerStrengthModifier] = modifier
	}
	if s.MaxStrength > 0 {
		m[mc.EntityDataKeyStrength] = s.Strength
		m[mc.EntityDataKeyStrengthMax] = s.MaxStrength
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
func writeNameTagMetadata(m mc.Metadata, nameTag string, alwaysShow bool) {
	show := nameTag != ""
	always := show && alwaysShow

	m[mc.EntityDataKeyName] = nameTag
	m[mc.EntityDataKeyAlwaysShowNameTag] = boolByte(always)
	if show {
		setActorFlag(m, mc.EntityDataFlagShowName)
	} else {
		m.UnsetFlag(mc.EntityDataKeyFlags, mc.EntityDataFlagShowName)
	}
	if always {
		setActorFlag(m, mc.EntityDataFlagAlwaysShowName)
	} else {
		m.UnsetFlag(mc.EntityDataKeyFlags, mc.EntityDataFlagAlwaysShowName)
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
	ContainerKind() entity.ContainerKind
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
}

// buoyancyData is the buoyancy data BDS sends of boats, the buoyant
// vehicles.
const buoyancyData = "{\"apply_gravity\":true,\"base_buoyancy\":1.0,\"big_wave_probability\":0.02999999932944775,\"big_wave_speed\":10.0,\"drag_down_on_buoyancy_removed\":0.0,\"liquid_blocks\":[\"minecraft:water\",\"minecraft:flowing_water\"],\"simulate_waves\":true}"

// interactText returns the prompt clients show when they look at a
// vehicle: to open the container of a chest boat or container minecart, to
// ride a boat with a free seat or an empty minecart that carries
// passengers, and none otherwise.
func interactText(e any) string {
	if _, ok := e.(containerMeta); ok {
		return "action.interact.opencontainer"
	}
	switch v := e.(type) {
	case interface{ Full() bool }:
		if !v.Full() {
			return "action.interact.ride.boat"
		}
	case interface {
		Rideable() bool
		Passenger() *world.EntityHandle
	}:
		if v.Rideable() && v.Passenger() == nil {
			return "action.interact.ride.minecart"
		}
	}
	return ""
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
func writeSeatMetadata(m mc.Metadata, seat world.RiderSeat) {
	if seat.Position != (mgl64.Vec3{}) {
		m[mc.EntityDataKeySeatOffset] = vec64To32(seat.Position)
	}
	m[mc.EntityDataKeySeatLockPassengerRotation] = byte(boolByte(seat.LockRotation))
	m[mc.EntityDataKeySeatLockPassengerRotationDegrees] = float32(seat.LockRotationDegrees)
	m[mc.EntityDataKeySeatRotationOffset] = byte(boolByte(seat.RotateRider))
	m[mc.EntityDataKeySeatRotationOffsetDegrees] = float32(seat.RotateRiderDegrees)
}

// snifferStateFlags holds the flag Bedrock shows for each state of a sniffer.
var snifferStateFlags = map[string]uint8{
	"feeling_happy": mc.EntityDataFlagFeelingHappy,
	"scenting":      mc.EntityDataFlagScenting,
	"sniffing":      mc.EntityDataFlagSniffing,
	"searching":     mc.EntityDataFlagSearching,
	"digging":       mc.EntityDataFlagDigging,
	"rising":        mc.EntityDataFlagRising,
}
