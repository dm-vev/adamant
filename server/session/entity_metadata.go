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
	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// parseEntityMetadata returns an entity metadata object with default values. It is equivalent to setting
// all properties to their default values and disabling all flags.
func (s *Session) parseEntityMetadata(e world.Entity) v2193.Metadata {
	bb := e.H().Type().BBox(e)
	m := v2193.NewMetadata()

	m[v2193.EntityDataKeyWidth] = float32(bb.Width())
	m[v2193.EntityDataKeyHeight] = float32(bb.Height())
	m[v2193.EntityDataKeyEffectColor] = int32(0)
	m[v2193.EntityDataKeyEffectAmbience] = byte(0)
	m[v2193.EntityDataKeyColorIndex] = byte(0)

	setActorFlag(m, v2193.EntityDataFlagHasGravity)
	setActorFlag(m, v2193.EntityDataFlagClimb)
	writeActorShape(e, networkID(e.H().Type()), m)
	if g, ok := e.H().Type().(glint); ok && g.Glint() {
		setActorFlag(m, v2193.EntityDataFlagEnchanted)
	}
	if e.H().Type() == entity.LingeringPotionType {
		setActorFlag(m, v2193.EntityDataFlagLingering)
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

func (s *Session) addSpecificMetadata(e any, m v2193.Metadata) {
	if sheep, ok := e.(*entity.Sheep); ok {
		m[v2193.EntityDataKeyColorIndex] = byte(sheep.WoolColor())
		if sheep.Sheared() {
			setActorFlag(m, v2193.EntityDataFlagSheared)
		}
	}
	if shulker, ok := e.(*entity.Shulker); ok {
		// BDS uses Variant for shell colour and separate attachment metadata.
		m[v2193.EntityDataKeyVariant] = int32(shulker.Color())
		m[v2193.EntityDataKeyPeekID] = int32(shulker.Peek())
		m[v2193.EntityDataKeyAttachFace] = byte(shulker.AttachFace())
		m[v2193.EntityDataKeyAttached] = int16(1)
		pos := cube.PosFromVec3(shulker.Position())
		m[v2193.EntityDataKeyAttachedPosition] = v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	}
	if parrot, ok := e.(*entity.Parrot); ok {
		m[v2193.EntityDataKeyVariant] = int32(parrot.Variant())
		if parrot.Tamed() {
			setActorFlag(m, v2193.EntityDataFlagTamed)
		}
		if parrot.Party() {
			setActorFlag(m, v2193.EntityDataFlagDancing)
		}
		if parrot.Sitting() {
			setActorFlag(m, v2193.EntityDataFlagSitting)
		}
	}
	if sn, ok := e.(sneaker); ok && sn.Sneaking() {
		setActorFlag(m, v2193.EntityDataFlagSneaking)
		if b, ok := e.(blocker); ok {
			if _, blocking := b.Blocking(); blocking {
				m.SetFlag(v2193.EntityDataKeyFlagsTwo, v2193.EntityDataFlagBlocking&63)
			}
		}
	}
	if sp, ok := e.(sprinter); ok && sp.Sprinting() {
		setActorFlag(m, v2193.EntityDataFlagSprinting)
	}
	if sw, ok := e.(swimmer); ok && sw.Swimming() {
		setActorFlag(m, v2193.EntityDataFlagSwimming)
	}
	if cr, ok := e.(crawler); ok && cr.Crawling() {
		m.SetFlag(v2193.EntityDataKeyFlagsTwo, v2193.EntityDataFlagCrawling&63)
	}
	if gl, ok := e.(glider); ok && gl.Gliding() {
		setActorFlag(m, v2193.EntityDataFlagGliding)
	}
	if bb, ok := e.(baby); ok && bb.Baby() {
		setActorFlag(m, v2193.EntityDataFlagBaby)
	}
	if l, ok := e.(interface{ InLove() bool }); ok && l.InLove() {
		setActorFlag(m, v2193.EntityDataFlagInLove)
	}
	if b, ok := e.(breather); ok {
		m[v2193.EntityDataKeyAirSupply] = int16(b.AirSupply().Milliseconds() / 50)
		m[v2193.EntityDataKeyAirSupplyMax] = int16(b.MaxAirSupply().Milliseconds() / 50)
		if b.Breathing() {
			setActorFlag(m, v2193.EntityDataFlagBreathing)
		}
	}
	if i, ok := e.(invisible); ok {
		if i.Invisible() {
			setActorFlag(m, v2193.EntityDataFlagInvisible)
		}
	} else if eff, ok := e.(effectBearer); ok && slices.ContainsFunc(eff.Effects(), func(ef effect.Effect) bool { return ef.Type() == effect.Invisibility }) {
		setActorFlag(m, v2193.EntityDataFlagInvisible)
	}
	if i, ok := e.(immobile); ok && i.Immobile() {
		setActorFlag(m, v2193.EntityDataFlagNoAI)
	}
	if o, ok := e.(onFire); ok && o.OnFireDuration() > 0 {
		setActorFlag(m, v2193.EntityDataFlagOnFire)
	}
	if f, ok := e.(interface{ FreezingStrength() float32 }); ok {
		m[v2193.EntityDataKeyFreezingEffectStrength] = f.FreezingStrength()
	}
	if sh, ok := e.(interface{ Shaking() bool }); ok && sh.Shaking() {
		setActorFlag(m, v2193.EntityDataFlagShaking)
	}
	if r, ok := e.(interface{ RamAttacking() bool }); ok && r.RamAttacking() {
		setActorFlag(m, v2193.EntityDataFlagRamAttack)
	}
	if w, ok := e.(interface {
		WardenPose() (emerging, digging, roaring, sniffing, sonicBoom bool)
		HeartbeatInterval() int32
	}); ok {
		emerging, digging, roaring, sniffing, sonicBoom := w.WardenPose()
		for flag, set := range map[uint8]bool{v2193.EntityDataFlagEmerging: emerging, v2193.EntityDataFlagDigging: digging,
			v2193.EntityDataFlagRoaring: roaring, v2193.EntityDataFlagSniffing: sniffing, v2193.EntityDataFlagSonicBoom: sonicBoom} {
			if set {
				setActorFlag(m, flag)
			}
		}
		m[v2193.EntityDataKeyHeartbeatIntervalTicks] = w.HeartbeatInterval()
		// LevelSoundEvent heartbeat.
		m[v2193.EntityDataKeyHeartbeatSoundEvent] = int32(376)
	}
	if sh, ok := e.(interface{ Shooting() bool }); ok && sh.Shooting() {
		// A breeze shooting faces its target to attack it from range.
		setActorFlag(m, v2193.EntityDataFlagFacingTargetToRangeAttack)
	}
	if j, ok := e.(interface{ JumpGoalJumping() bool }); ok && j.JumpGoalJumping() {
		setActorFlag(m, v2193.EntityDataFlagJumpGoal)
	}
	if sn, ok := e.(interface{ SnifferState() string }); ok {
		if flag, ok := snifferStateFlags[sn.SnifferState()]; ok {
			setActorFlag(m, flag)
		}
	}
	if p, ok := e.(interface{ PlayingDead() bool }); ok && p.PlayingDead() {
		setActorFlag(m, v2193.EntityDataFlagPlayingDead)
	}
	if c, ok := e.(interface{ Croaking() bool }); ok && c.Croaking() {
		setActorFlag(m, v2193.EntityDataFlagCroaking)
	}
	if c, ok := e.(interface{ EatingMob() bool }); ok && c.EatingMob() {
		// Bedrock's EAT_MOB flag shows a frog's tongue.
		setActorFlag(m, v2193.EntityDataFlagDigestMob)
	}
	if r, ok := e.(interface{ Resting() bool }); ok && r.Resting() {
		// Bats hang upside down while resting.
		setActorFlag(m, v2193.EntityDataFlagResting)
	}
	if db, ok := e.(doorBreaker); ok && db.CanBreakDoors() {
		setActorFlag(m, v2193.EntityDataFlagDoorBreaker)
	}
	if br, ok := e.(breakingObstruction); ok && br.BreakingObstruction() {
		setActorFlag(m, v2193.EntityDataFlagBreakingObstruction)
	}
	if ar, ok := e.(armsRaised); ok && ar.ArmsRaised() {
		setActorFlag(m, v2193.EntityDataFlagCharging)
	}
	if u, ok := e.(using); ok && u.UsingItem() {
		setActorFlag(m, v2193.EntityDataFlagUsingItem)
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[v2193.EntityDataKeyBedPosition] = v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(v2193.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(arrow); ok && c.Critical() {
		setActorFlag(m, v2193.EntityDataFlagCritical)
	}
	if g, ok := e.(gameMode); ok {
		if g.GameMode().HasCollision() {
			setActorFlag(m, v2193.EntityDataFlagHasCollision)
		}
	}
	if o, ok := e.(orb); ok {
		m[v2193.EntityDataKeyValue] = int32(o.Experience())
	}
	if f, ok := e.(firework); ok {
		m[v2193.EntityDataKeyDisplayFirework] = item.WriteNBT(item.NewStack(f.Firework(), 1), false)
		if o, ok := e.(owned); ok && f.Attached() && o.Owner() != nil {
			m[v2193.EntityDataKeyCustomDisplay] = s.handleUniqueID(o.Owner())
		}
	} else if o, ok := e.(owned); ok && o.Owner() != nil {
		m[v2193.EntityDataKeyOwner] = s.handleUniqueID(o.Owner())
	}
	if l, ok := e.(leashed); ok {
		if id := l.HolderLeashID(); id != 0 {
			if s.ent != nil && id == s.ent.UniqueID() {
				id = int64(selfEntityRuntimeID)
			}
			m[v2193.EntityDataKeyLeashHolder] = id
		}
	}
	if sc, ok := e.(scaled); ok {
		m[v2193.EntityDataKeyScale] = float32(sc.Scale())
	}
	if t, ok := e.(tnt); ok {
		m[v2193.EntityDataKeyFuseTime] = int32(t.Fuse().Milliseconds() / 50)
		setActorFlag(m, v2193.EntityDataFlagIgnited)
	}
	if i, ok := e.(igniteable); ok && i.Ignited() {
		m[v2193.EntityDataKeyFuseTime] = int32(i.FuseTicks())
		setActorFlag(m, v2193.EntityDataFlagIgnited)
	}
	if p, ok := e.(interface{ Powered() bool }); ok && p.Powered() {
		setActorFlag(m, v2193.EntityDataFlagPowered)
	}
	if c, ok := e.(interface{ WallClimbing() bool }); ok && c.WallClimbing() {
		setActorFlag(m, v2193.EntityDataFlagWallClimbing)
	}
	// A blaze charging its fireballs burns, as Blaze.isOnFire shows it.
	if b, ok := e.(interface{ Burning() bool }); ok && b.Burning() {
		setActorFlag(m, v2193.EntityDataFlagOnFire)
	}
	if c, ok := e.(interface{ Charging() bool }); ok && c.Charging() {
		setActorFlag(m, v2193.EntityDataFlagCharging)
	}
	if c, ok := e.(interface{ CrossbowCharged() bool }); ok && c.CrossbowCharged() {
		setActorFlag(m, v2193.EntityDataFlagCharged)
	}
	if c, ok := e.(interface{ Celebrating() bool }); ok && c.Celebrating() {
		setActorFlag(m, v2193.EntityDataFlagCelebrating)
	}
	if c, ok := e.(interface{ CelebratingSpecial() bool }); ok && c.CelebratingSpecial() {
		setActorFlag(m, v2193.EntityDataFlagCelebratingSpecial)
	}
	if a, ok := e.(interface{ Admiring() bool }); ok && a.Admiring() {
		setActorFlag(m, v2193.EntityDataFlagAdmiring)
	}
	if c, ok := e.(interface {
		Casting() bool
		SpellColour() [3]float64
	}); ok && c.Casting() {
		setActorFlag(m, v2193.EntityDataFlagCasting)
		colour := c.SpellColour()
		m[v2193.EntityDataKeyDataSpellCastingColor] = int32(-1<<24 | int32(colour[0]*255)<<16 | int32(colour[1]*255)<<8 | int32(colour[2]*255))
	}
	if g, ok := e.(interface{ BeamTargetHandle() *world.EntityHandle }); ok {
		if h := g.BeamTargetHandle(); h != nil {
			m[v2193.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if g, ok := e.(interface{ ShootTargetHandle() *world.EntityHandle }); ok {
		m[v2193.EntityDataKeyTarget] = int64(0)
		if h := g.ShootTargetHandle(); h != nil {
			m[v2193.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if f, ok := e.(interface{ TongueTargetHandle() *world.EntityHandle }); ok {
		// The target is cleared once the tongue is back, as Frog.eraseTongueTarget.
		m[v2193.EntityDataKeyTarget] = int64(0)
		if h := f.TongueTargetHandle(); h != nil {
			m[v2193.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if r, ok := e.(interface {
		Stunned() bool
		Roaring() bool
	}); ok {
		if r.Stunned() {
			setActorFlag(m, v2193.EntityDataFlagStunned)
		}
		if r.Roaring() {
			setActorFlag(m, v2193.EntityDataFlagRoaring)
		}
	}
	if d, ok := e.(displayTile); ok {
		var display int32
		if b, ok := d.DisplayBlock(); ok {
			display = int32(networkBlockID(s.br, b))
		}
		m[v2193.EntityDataKeyDisplayBlockState] = display
		m[v2193.EntityDataKeyDisplayOffset] = int32(d.DisplayOffset())
		m[v2193.EntityDataKeyCustomDisplay] = byte(boolByte(d.CustomDisplay()))
	}
	if c, ok := e.(containerMeta); ok {
		// The dedicated server sends the container type as a byte.
		m[v2193.EntityDataKeyContainerType] = byte(c.ContainerType())
		m[v2193.EntityDataKeyContainerSize] = c.ContainerSize()
		m[v2193.EntityDataKeyContainerStrengthModifier] = c.ContainerStrengthModifier()
	}
	if h, ok := e.(minecartDamage); ok {
		m[v2193.EntityDataKeyHurt] = int32(h.RollingAmplitude())
		m[v2193.EntityDataKeyHurtDirection] = int32(h.RollingDirection())
		// BDS sends the damage of a vehicle as an int.
		m[v2193.EntityDataKeyStructuralIntegrity] = int32(h.Damage())
	}
	if r, ok := e.(rowTime); ok {
		m[v2193.EntityDataKeyRowTimeLeft] = r.RowTimeLeft()
		m[v2193.EntityDataKeyRowTimeRight] = r.RowTimeRight()
	}
	if b, ok := e.(buoyantMeta); ok {
		m[v2193.EntityDataKeyIsBuoyant] = byte(boolByte(b.Buoyant()))
		if data := b.BuoyancyData(); data != "" {
			m[v2193.EntityDataKeyBuoyancyData] = data
		}
	}
	if c, ok := e.(controllingSeat); ok {
		m[v2193.EntityDataKeyControllingSeatIndex] = byte(c.ControllingSeatIndex())
	}
	if t, ok := e.(interactText); ok {
		if text := t.InteractText(); text != "" {
			m[v2193.EntityDataKeyInteractText] = text
		}
	}
	if r, ok := e.(rideTarget); ok {
		m[v2193.EntityDataKeyCanRideTarget] = byte(boolByte(r.CanRideTarget()))
	}
	if w, ok := e.(witherMeta); ok {
		// The heads of a wither turn to their targets, and it glows blue
		// while it is summoned.
		for i, h := range w.HeadTargets() {
			var id int64
			if h != nil {
				id = int64(s.handleRuntimeID(h))
			}
			m[[3]uint32{v2193.EntityDataKeyTargetA, v2193.EntityDataKeyTargetB, v2193.EntityDataKeyTargetC}[i]] = id
		}
		m[v2193.EntityDataKeyInvulnerableTicks] = int32(w.InvulnerableTicks())
	}
	if ec, ok := e.(endCrystalMeta); ok {
		if ec.ShowBase() {
			setActorFlag(m, v2193.EntityDataFlagShowBottom)
		}
		if target, ok := ec.BeamTarget(); ok {
			m[v2193.EntityDataKeyBlockTarget] = v2193.BlockPos{
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
		m[v2193.EntityDataKeyScore] = sc.ScoreTag()
	}
	if sl, ok := e.(sleeper); ok {
		if pos, ok := sl.Sleeping(); ok {
			m[v2193.EntityDataKeyBedPosition] = v2193.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

			// For some reason there is no such flag in gophertunnel.
			m.SetFlag(v2193.EntityDataKeyPlayerFlags, 1)
		}
	}
	if c, ok := e.(areaEffectCloud); ok {
		m[v2193.EntityDataKeyDataRadius] = float32(c.Radius())

		// We purposely fill these in with invalid values to disable the client-sided shrinking of the cloud.
		m[v2193.EntityDataKeyDataDuration] = int32(math.MaxInt32)
		m[v2193.EntityDataKeyDataChangeOnPickup] = float32(math.SmallestNonzeroFloat32)
		m[v2193.EntityDataKeyDataChangeRate] = float32(math.SmallestNonzeroFloat32)
		if p, ok := e.(interface{ Particle() int32 }); ok && p.Particle() != 0 {
			m[v2193.EntityDataKeyDataParticle] = p.Particle()
		}

		colour, am := effect.ResultingColour(c.Effects())
		m[v2193.EntityDataKeyEffectColor] = nbtconv.Int32FromRGBA(colour)
		if am {
			m[v2193.EntityDataKeyEffectAmbience] = byte(1)
		} else {
			m[v2193.EntityDataKeyEffectAmbience] = byte(0)
		}
	}

	if l, ok := e.(living); ok && s.ent != nil && s.ent.UUID() == l.UUID() {
		deathPos, deathDimension, died := l.DeathPosition()
		if died {
			dim, _ := world.DimensionID(deathDimension)
			m[v2193.EntityDataKeyPlayerLastDeathPosition] = vec64To32(deathPos)
			m[v2193.EntityDataKeyPlayerLastDeathDimension] = int32(dim)
		}
		m[v2193.EntityDataKeyPlayerHasDied] = boolByte(died)
	}
	if p, ok := e.(splash); ok {
		m[v2193.EntityDataKeyAuxValueData] = int16(p.Potion().Uint8())
		if tip := p.Potion().Uint8(); tip > 4 {
			m[v2193.EntityDataKeyCustomDisplay] = tip + 1
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
		m[v2193.EntityDataKeyVisibleMobEffects] = packedEffects
	}
	if v, ok := e.(variable); ok {
		m[v2193.EntityDataKeyVariant] = v.Variant()
	}
	if mv, ok := e.(markVariable); ok {
		m[v2193.EntityDataKeyMarkVariant] = mv.MarkVariant()
	}
	if mount, ok := e.(interface{ MountState() entity.MountState }); ok {
		addMountMetadata(mount.MountState(), m)
	}
	if iv, ok := e.(hurtDuration); ok {
		m[v2193.EntityDataKeyHurt] = int32(iv.HurtDuration().Milliseconds() / 50)
	}
	if pi, ok := e.(armourStand); ok {
		m[v2193.EntityDataKeyPoseIndex] = int32(pi.PoseIndex())
	}
}

// addMountMetadata writes the flags and data Bedrock clients show of horses,
// llamas and camels, as the dedicated server sends them.
func addMountMetadata(s entity.MountState, m v2193.Metadata) {
	for flag, set := range map[uint8]bool{v2193.EntityDataFlagSaddled: s.Saddled, v2193.EntityDataFlagTamed: s.Tamed, v2193.EntityDataFlagChested: s.Chested, v2193.EntityDataFlagSitting: s.Sitting,
		v2193.EntityDataFlagKeyboardControlled: s.Controlled, v2193.EntityDataFlagPowerJump: s.PowerJump, v2193.EntityDataFlagDash: s.Dash} {
		if set {
			setActorFlag(m, flag)
		}
	}
	if s.Rearing {
		m[v2193.EntityDataKeyHorseFlags] = s.HorseFlags
	}
	if s.ContainerSize > 0 {
		m[v2193.EntityDataKeyContainerType] = byte(v2193.ContainerTypeHorse)
		m[v2193.EntityDataKeyContainerSize] = s.ContainerSize
		m[v2193.EntityDataKeyContainerStrengthModifier] = s.StrengthModifier
	}
	if s.MaxStrength > 0 {
		m[v2193.EntityDataKeyStrength] = s.Strength
		m[v2193.EntityDataKeyStrengthMax] = s.MaxStrength
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
func writeNameTagMetadata(m v2193.Metadata, nameTag string, alwaysShow bool) {
	show := nameTag != ""
	always := show && alwaysShow

	m[v2193.EntityDataKeyName] = nameTag
	m[v2193.EntityDataKeyAlwaysShowNameTag] = boolByte(always)
	if show {
		setActorFlag(m, v2193.EntityDataFlagShowName)
	} else {
		m.UnsetFlag(v2193.EntityDataKeyFlags, v2193.EntityDataFlagShowName)
	}
	if always {
		setActorFlag(m, v2193.EntityDataFlagAlwaysShowName)
	} else {
		m.UnsetFlag(v2193.EntityDataKeyFlags, v2193.EntityDataFlagAlwaysShowName)
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
func writeSeatMetadata(m v2193.Metadata, seat world.RiderSeat) {
	if seat.Position != (mgl64.Vec3{}) {
		m[v2193.EntityDataKeySeatOffset] = vec64To32(seat.Position)
	}
	m[v2193.EntityDataKeySeatLockPassengerRotation] = byte(boolByte(seat.LockRotation))
	m[v2193.EntityDataKeySeatLockPassengerRotationDegrees] = float32(seat.LockRotationDegrees)
	m[v2193.EntityDataKeySeatRotationOffset] = byte(boolByte(seat.RotateRider))
	m[v2193.EntityDataKeySeatRotationOffsetDegrees] = float32(seat.RotateRiderDegrees)
}

// snifferStateFlags holds the flag Bedrock shows for each state of a sniffer.
var snifferStateFlags = map[string]uint8{
	"feeling_happy": v2193.EntityDataFlagFeelingHappy,
	"scenting":      v2193.EntityDataFlagScenting,
	"sniffing":      v2193.EntityDataFlagSniffing,
	"searching":     v2193.EntityDataFlagSearching,
	"digging":       v2193.EntityDataFlagDigging,
	"rising":        v2193.EntityDataFlagRising,
}
