package session

import (
	"cmp"
	"math"
	"reflect"
	"slices"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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

// addSpecificMetadata writes the metadata of e that depends on what it is: the
// writers of metadataWriters that apply to its dynamic type, in their order.
// The first time it sees a type it runs all of them, which tells which apply,
// and only those after.
func (s *Session) addSpecificMetadata(e any, m mc.Metadata) {
	t := reflect.TypeOf(e)
	if ws, ok := metadataWritersOf.Load(t); ok {
		for _, w := range ws.([]metadataWriter) {
			w(s, e, m)
		}
		return
	}
	var ws []metadataWriter
	for _, w := range metadataWriters {
		if w(s, e, m) {
			ws = append(ws, w)
		}
	}
	metadataWritersOf.Store(t, ws)
}

// metadataWritersOf holds the writers of metadataWriters that apply to each
// dynamic type addSpecificMetadata saw, by its reflect.Type. They depend on
// the type alone, so all sessions share it. Sessions write metadata on the
// goroutine of the world they view, which changes with the world of the
// player, so the cache is a sync.Map: once a type is in, reading it takes no
// lock.
var metadataWritersOf sync.Map

// A metadataWriter writes the metadata of e of one kind of entity, if e is of
// that kind, and reports whether it is: whether the type assertions it makes
// of e hold. That depends on the dynamic type of e alone, and a writer that
// reports false writes nothing.
type metadataWriter func(s *Session, e any, m mc.Metadata) bool

// metadataWriters are the writers of the metadata of each kind of entity, in
// the order addSpecificMetadata runs them: a later one may overwrite what an
// earlier one wrote.
var metadataWriters = [...]metadataWriter{
	func(_ *Session, e any, m mc.Metadata) bool {
		sheep, ok := e.(*entity.Sheep)
		if ok {
			m[mc.EntityDataKeyColorIndex] = byte(sheep.WoolColor())
			if sheep.Sheared() {
				setActorFlag(m, mc.EntityDataFlagSheared)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		shulker, ok := e.(*entity.Shulker)
		if ok {
			// BDS uses Variant for shell colour and separate attachment metadata.
			m[mc.EntityDataKeyVariant] = int32(shulker.Color())
			m[mc.EntityDataKeyPeekID] = int32(shulker.Peek())
			m[mc.EntityDataKeyAttachFace] = byte(shulker.AttachFace())
			m[mc.EntityDataKeyAttached] = int16(1)
			pos := cube.PosFromVec3(shulker.Position())
			m[mc.EntityDataKeyAttachedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		parrot, ok := e.(*entity.Parrot)
		if ok {
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
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sn, ok := e.(sneaker)
		if ok && sn.Sneaking() {
			setActorFlag(m, mc.EntityDataFlagSneaking)
			if b, ok := e.(blocker); ok {
				if _, blocking := b.Blocking(); blocking {
					m.SetFlag(mc.EntityDataKeyFlagsTwo, mc.EntityDataFlagBlocking&63)
				}
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sp, ok := e.(sprinter)
		if ok && sp.Sprinting() {
			setActorFlag(m, mc.EntityDataFlagSprinting)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sw, ok := e.(swimmer)
		if ok && sw.Swimming() {
			setActorFlag(m, mc.EntityDataFlagSwimming)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		cr, ok := e.(crawler)
		if ok && cr.Crawling() {
			m.SetFlag(mc.EntityDataKeyFlagsTwo, mc.EntityDataFlagCrawling&63)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		gl, ok := e.(glider)
		if ok && gl.Gliding() {
			setActorFlag(m, mc.EntityDataFlagGliding)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		bb, ok := e.(baby)
		if ok && bb.Baby() {
			setActorFlag(m, mc.EntityDataFlagBaby)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		l, ok := e.(interface{ InLove() bool })
		if ok && l.InLove() {
			setActorFlag(m, mc.EntityDataFlagInLove)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		b, ok := e.(breather)
		if ok {
			m[mc.EntityDataKeyAirSupply] = int16(b.AirSupply().Milliseconds() / 50)
			m[mc.EntityDataKeyAirSupplyMax] = int16(b.MaxAirSupply().Milliseconds() / 50)
			if b.Breathing() {
				setActorFlag(m, mc.EntityDataFlagBreathing)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		i, ok := e.(invisible)
		eff, bearer := e.(effectBearer)
		if ok {
			if i.Invisible() {
				setActorFlag(m, mc.EntityDataFlagInvisible)
			}
		} else if bearer && slices.ContainsFunc(eff.Effects(), func(ef effect.Effect) bool { return ef.Type() == effect.Invisibility }) {
			setActorFlag(m, mc.EntityDataFlagInvisible)
		}
		return ok || bearer
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		i, ok := e.(immobile)
		if ok && i.Immobile() {
			setActorFlag(m, mc.EntityDataFlagNoAI)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		o, ok := e.(onFire)
		if ok && o.OnFireDuration() > 0 {
			setActorFlag(m, mc.EntityDataFlagOnFire)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		f, ok := e.(interface{ FreezingStrength() float32 })
		if ok {
			m[mc.EntityDataKeyFreezingEffectStrength] = f.FreezingStrength()
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sh, ok := e.(interface{ Shaking() bool })
		if ok && sh.Shaking() {
			setActorFlag(m, mc.EntityDataFlagShaking)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		r, ok := e.(interface{ RamAttacking() bool })
		if ok && r.RamAttacking() {
			setActorFlag(m, mc.EntityDataFlagRamAttack)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		w, ok := e.(interface {
			WardenPose() (emerging, digging, roaring, sniffing, sonicBoom bool)
			HeartbeatInterval() int32
		})
		if ok {
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
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sh, ok := e.(interface{ Shooting() bool })
		if ok && sh.Shooting() {
			// A breeze shooting faces its target to attack it from range.
			setActorFlag(m, mc.EntityDataFlagFacingTargetToRangeAttack)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		j, ok := e.(interface{ JumpGoalJumping() bool })
		if ok && j.JumpGoalJumping() {
			setActorFlag(m, mc.EntityDataFlagJumpGoal)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sn, ok := e.(interface{ SnifferState() string })
		if ok {
			if flag, ok := snifferStateFlags[sn.SnifferState()]; ok {
				setActorFlag(m, flag)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		p, ok := e.(interface{ PlayingDead() bool })
		if ok && p.PlayingDead() {
			setActorFlag(m, mc.EntityDataFlagPlayingDead)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ Croaking() bool })
		if ok && c.Croaking() {
			setActorFlag(m, mc.EntityDataFlagCroaking)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ EatingMob() bool })
		if ok && c.EatingMob() {
			// Bedrock's EAT_MOB flag shows a frog's tongue.
			setActorFlag(m, mc.EntityDataFlagDigestMob)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		r, ok := e.(interface{ Resting() bool })
		if ok && r.Resting() {
			// Bats hang upside down while resting.
			setActorFlag(m, mc.EntityDataFlagResting)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		db, ok := e.(doorBreaker)
		if ok && db.CanBreakDoors() {
			setActorFlag(m, mc.EntityDataFlagDoorBreaker)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		br, ok := e.(breakingObstruction)
		if ok && br.BreakingObstruction() {
			setActorFlag(m, mc.EntityDataFlagBreakingObstruction)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		ar, ok := e.(armsRaised)
		if ok && ar.ArmsRaised() {
			setActorFlag(m, mc.EntityDataFlagCharging)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		u, ok := e.(using)
		if ok && u.UsingItem() {
			setActorFlag(m, mc.EntityDataFlagUsingItem)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sl, ok := e.(sleeper)
		if ok {
			if pos, ok := sl.Sleeping(); ok {
				m[mc.EntityDataKeyBedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

				// For some reason there is no such flag in gophertunnel.
				m.SetFlag(mc.EntityDataKeyPlayerFlags, 1)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(arrow)
		if ok && c.Critical() {
			setActorFlag(m, mc.EntityDataFlagCritical)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		g, ok := e.(gameMode)
		if ok {
			if g.GameMode().HasCollision() {
				setActorFlag(m, mc.EntityDataFlagHasCollision)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		o, ok := e.(orb)
		if ok {
			m[mc.EntityDataKeyValue] = int32(o.Experience())
		}
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		f, ok := e.(firework)
		o, isOwned := e.(owned)
		if ok {
			m[mc.EntityDataKeyDisplayFirework] = networkItemNBT(item.NewStack(f.Firework(), 1))
			if isOwned && f.Attached() && o.Owner() != nil {
				m[mc.EntityDataKeyCustomDisplay] = s.handleUniqueID(o.Owner())
			}
		} else if isOwned && o.Owner() != nil {
			m[mc.EntityDataKeyOwner] = s.handleUniqueID(o.Owner())
		}
		return ok || isOwned
	},
	func(s *Session, e any, m mc.Metadata) bool {
		l, ok := e.(leashed)
		if ok {
			if id := l.HolderLeashID(); id != 0 {
				if s.ent != nil && id == s.ent.UniqueID() {
					id = int64(selfEntityRuntimeID)
				}
				m[mc.EntityDataKeyLeashHolder] = id
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sc, ok := e.(scaled)
		if ok {
			m[mc.EntityDataKeyScale] = float32(sc.Scale())
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		t, ok := e.(tnt)
		if ok {
			m[mc.EntityDataKeyFuseTime] = int32(t.Fuse().Milliseconds() / 50)
			setActorFlag(m, mc.EntityDataFlagIgnited)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		i, ok := e.(igniteable)
		if ok && i.Ignited() {
			m[mc.EntityDataKeyFuseTime] = int32(i.FuseTicks())
			setActorFlag(m, mc.EntityDataFlagIgnited)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		p, ok := e.(interface{ Powered() bool })
		if ok && p.Powered() {
			setActorFlag(m, mc.EntityDataFlagPowered)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ WallClimbing() bool })
		if ok && c.WallClimbing() {
			setActorFlag(m, mc.EntityDataFlagWallClimbing)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		// A blaze charging its fireballs burns, as Blaze.isOnFire shows it.
		b, ok := e.(interface{ Burning() bool })
		if ok && b.Burning() {
			setActorFlag(m, mc.EntityDataFlagOnFire)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ Charging() bool })
		if ok && c.Charging() {
			setActorFlag(m, mc.EntityDataFlagCharging)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ CrossbowCharged() bool })
		if ok && c.CrossbowCharged() {
			setActorFlag(m, mc.EntityDataFlagCharged)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ Celebrating() bool })
		if ok && c.Celebrating() {
			setActorFlag(m, mc.EntityDataFlagCelebrating)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface{ CelebratingSpecial() bool })
		if ok && c.CelebratingSpecial() {
			setActorFlag(m, mc.EntityDataFlagCelebratingSpecial)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		a, ok := e.(interface{ Admiring() bool })
		if ok && a.Admiring() {
			setActorFlag(m, mc.EntityDataFlagAdmiring)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(interface {
			Casting() bool
			SpellColour() [3]float64
		})
		if ok && c.Casting() {
			setActorFlag(m, mc.EntityDataFlagCasting)
			colour := c.SpellColour()
			m[mc.EntityDataKeyDataSpellCastingColor] = int32(-1<<24 | int32(colour[0]*255)<<16 | int32(colour[1]*255)<<8 | int32(colour[2]*255))
		}
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		g, ok := e.(interface{ BeamTargetHandle() *world.EntityHandle })
		if ok {
			if h := g.BeamTargetHandle(); h != nil {
				m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
			}
		}
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		g, ok := e.(interface{ ShootTargetHandle() *world.EntityHandle })
		if ok {
			m[mc.EntityDataKeyTarget] = int64(0)
			if h := g.ShootTargetHandle(); h != nil {
				m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
			}
		}
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		f, ok := e.(interface{ TongueTargetHandle() *world.EntityHandle })
		if ok {
			// The target is cleared once the tongue is back, as Frog.eraseTongueTarget.
			m[mc.EntityDataKeyTarget] = int64(0)
			if h := f.TongueTargetHandle(); h != nil {
				m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		r, ok := e.(interface {
			Stunned() bool
			Roaring() bool
		})
		if ok {
			if r.Stunned() {
				setActorFlag(m, mc.EntityDataFlagStunned)
			}
			if r.Roaring() {
				setActorFlag(m, mc.EntityDataFlagRoaring)
			}
		}
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		d, ok := e.(displayTile)
		if ok {
			var display int32
			if b, ok := d.DisplayBlock(); ok {
				display = int32(s.blockHash(b))
			}
			m[mc.EntityDataKeyDisplayBlockState] = display
			m[mc.EntityDataKeyDisplayOffset] = int32(d.DisplayOffset())
			m[mc.EntityDataKeyCustomDisplay] = byte(boolByte(d.CustomDisplay()))
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(containerMeta)
		if ok {
			if typ, ok := actorContainerType(c.ContainerKind()); ok {
				// The dedicated server sends the container type as a byte.
				m[mc.EntityDataKeyContainerType] = typ
				m[mc.EntityDataKeyContainerSize] = c.ContainerSize()
				m[mc.EntityDataKeyContainerStrengthModifier] = c.ContainerStrengthModifier()
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		h, ok := e.(minecartDamage)
		if ok {
			m[mc.EntityDataKeyHurt] = int32(h.RollingAmplitude())
			m[mc.EntityDataKeyHurtDirection] = int32(h.RollingDirection())
			// BDS sends the damage of a vehicle as an int.
			m[mc.EntityDataKeyStructuralIntegrity] = int32(h.Damage())
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		r, ok := e.(rowTime)
		if ok {
			m[mc.EntityDataKeyRowTimeLeft] = r.RowTimeLeft()
			m[mc.EntityDataKeyRowTimeRight] = r.RowTimeRight()
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		b, ok := e.(buoyantMeta)
		if ok {
			m[mc.EntityDataKeyIsBuoyant] = byte(boolByte(b.Buoyant()))
			if b.Buoyant() {
				m[mc.EntityDataKeyBuoyancyData] = buoyancyData
				// The driver of a buoyant vehicle sits in its first seat.
				m[mc.EntityDataKeyControllingSeatIndex] = byte(0)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		text, ok := interactText(e)
		if text != "" {
			m[mc.EntityDataKeyInteractText] = text
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		r, ok := e.(rideTarget)
		if ok {
			m[mc.EntityDataKeyCanRideTarget] = byte(boolByte(r.CanRideTarget()))
		}
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		w, ok := e.(witherMeta)
		if ok {
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
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		ec, ok := e.(endCrystalMeta)
		if ok {
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
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		nameTag, alwaysShow, ok := nameTagState(e)
		if ok {
			writeNameTagMetadata(m, nameTag, alwaysShow)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sc, ok := e.(scoreTag)
		if ok {
			m[mc.EntityDataKeyScore] = sc.ScoreTag()
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		sl, ok := e.(sleeper)
		if ok {
			if pos, ok := sl.Sleeping(); ok {
				m[mc.EntityDataKeyBedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

				// For some reason there is no such flag in gophertunnel.
				m.SetFlag(mc.EntityDataKeyPlayerFlags, 1)
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		c, ok := e.(areaEffectCloud)
		if ok {
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
		return ok
	},
	func(s *Session, e any, m mc.Metadata) bool {
		l, ok := e.(living)
		if ok && s.ent != nil && s.ent.UUID() == l.UUID() {
			deathPos, deathDimension, died := l.DeathPosition()
			if died {
				dim, _ := world.DimensionID(deathDimension)
				m[mc.EntityDataKeyPlayerLastDeathPosition] = vec64To32(deathPos)
				m[mc.EntityDataKeyPlayerLastDeathDimension] = int32(dim)
			}
			m[mc.EntityDataKeyPlayerHasDied] = boolByte(died)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		p, ok := e.(splash)
		if ok {
			m[mc.EntityDataKeyAuxValueData] = int16(p.Potion().Uint8())
			if tip := p.Potion().Uint8(); tip > 4 {
				m[mc.EntityDataKeyCustomDisplay] = tip + 1
			}
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		eff, ok := e.(effectBearer)
		if ok {
			m[mc.EntityDataKeyVisibleMobEffects] = packVisibleEffects(eff.Effects())
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		v, ok := e.(variable)
		if ok {
			m[mc.EntityDataKeyVariant] = v.Variant()
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		mv, ok := e.(markVariable)
		if ok {
			m[mc.EntityDataKeyMarkVariant] = mv.MarkVariant()
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		mount, ok := e.(interface{ MountState() entity.MountState })
		if ok {
			addMountMetadata(mount.MountState(), m)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		iv, ok := e.(hurtDuration)
		if ok {
			m[mc.EntityDataKeyHurt] = int32(iv.HurtDuration().Milliseconds() / 50)
		}
		return ok
	},
	func(_ *Session, e any, m mc.Metadata) bool {
		pi, ok := e.(armourStand)
		if ok {
			m[mc.EntityDataKeyPoseIndex] = int32(pi.PoseIndex())
		}
		return ok
	},
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
// passengers, and none otherwise. It reports whether e is of a kind that may
// have a prompt, which depends on its dynamic type alone.
func interactText(e any) (string, bool) {
	if _, ok := e.(containerMeta); ok {
		return "action.interact.opencontainer", true
	}
	switch v := e.(type) {
	case interface{ Full() bool }:
		if !v.Full() {
			return "action.interact.ride.boat", true
		}
		return "", true
	case interface {
		Rideable() bool
		Passenger() *world.EntityHandle
	}:
		if v.Rideable() && v.Passenger() == nil {
			return "action.interact.ride.minecart", true
		}
		return "", true
	}
	return "", false
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

// packVisibleEffects packs the effects with particles shown seven bits each,
// the ID shifted left once and ambience in the lowest bit, as clients show
// them over a mob. BDS packs them in order of their ID, the lowest in the
// highest bits, however they were added.
func packVisibleEffects(effects []effect.Effect) int64 {
	type visible struct {
		id      int
		ambient bool
	}
	shown := make([]visible, 0, len(effects))
	for _, ef := range effects {
		if id, found := effect.ID(ef.Type()); found && !ef.ParticlesHidden() {
			shown = append(shown, visible{id: id, ambient: ef.Ambient()})
		}
	}
	slices.SortFunc(shown, func(a, b visible) int { return cmp.Compare(a.id, b.id) })
	var packed int64
	for _, v := range shown {
		packed = packed<<7 | int64(v.id<<1)
		if v.ambient {
			packed |= 1
		}
	}
	return packed
}
