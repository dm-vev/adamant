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

var (
	boxedBytes  [256]any
	boxedInt16s [301]any
	boxedInt32s [302]any // index 0 is -1, index 1 is 0, ..., index 301 is 300

	metaFloat32Zero        any = float32(0)
	metaFloat32One         any = float32(1)
	metaFloat32Min         any = float32(math.SmallestNonzeroFloat32)
	metaInt32Max           any = int32(math.MaxInt32)
	metaInt32Heartbeat     any = int32(376)
	metaInt64Zero          any = int64(0)
	metaEmptyStr           any = ""
	metaBuoyancy           any = buoyancyData
	metaSleepWidth         any = float32(sleepColliderWidth)
	metaSleepHeight        any = float32(sleepColliderHeight)
	metaInteractContainer  any = "action.interact.opencontainer"
	metaInteractBoat       any = "action.interact.ride.boat"
	metaInteractMinecart   any = "action.interact.ride.minecart"
	metaSleepSoundInterval any = float32(8)
	metaSleepSoundSpread   any = float32(16)
	metaSleepSoundEvent    any = "sleep"

	witherTargetKeys = [3]uint32{mc.EntityDataKeyTargetA, mc.EntityDataKeyTargetB, mc.EntityDataKeyTargetC}
)

func init() {
	for i := range boxedBytes {
		boxedBytes[i] = byte(i)
	}
	for i := range boxedInt16s {
		boxedInt16s[i] = int16(i)
	}
	for i := range boxedInt32s {
		boxedInt32s[i] = int32(i - 1)
	}
}

func boxByte(b byte) any     { return boxedBytes[b] }
func boxBoolByte(v bool) any { return boxedBytes[boolByte(v)] }
func boxString(s string) any {
	if s == "" {
		return metaEmptyStr
	}
	return s
}

func boxInt16(v int16) any {
	if v >= 0 && v <= 300 {
		return boxedInt16s[v]
	}
	return v
}

func boxInt32(v int32) any {
	if v >= -1 && v <= 300 {
		return boxedInt32s[v+1]
	}
	return v
}

func boxInt64(v int64) any {
	if v == 0 {
		return metaInt64Zero
	}
	return v
}

func boxFloat32(v float32) any {
	switch math.Float32bits(v) {
	case 0:
		return metaFloat32Zero
	case 0x3f800000:
		return metaFloat32One
	default:
		return v
	}
}

// actorFlags accumulates the two 64-bit Bedrock actor flag masks in registers
// so writers avoid repeated map lookups and int64 interface boxing.
type actorFlags struct {
	low, high   int64
	touchedLow  bool
	touchedHigh bool
}

func (f *actorFlags) set(flag uint8) {
	if flag < 64 {
		f.low |= 1 << flag
		f.touchedLow = true
		return
	}
	f.high |= 1 << (flag - 64)
	f.touchedHigh = true
}

func (f *actorFlags) unset(flag uint8) {
	if flag < 64 {
		f.low &^= 1 << flag
		f.touchedLow = true
		return
	}
	f.high &^= 1 << (flag - 64)
	f.touchedHigh = true
}

// entityTypeMeta caches the per-world.EntityType static metadata descriptors so
// parseEntityMetadata and entityProperties avoid repeated table lookups and
// value boxing on every entity state update.
type entityTypeMeta struct {
	netID                    string
	baseFlags0               int64
	baseFlags0Breathing      int64
	baseFlags1               int64
	baseFlags0Boxed          any
	baseFlags0BreathingBoxed any
	baseFlags1Boxed          any

	hasShape     bool
	hasBabyShape bool
	dynamicShape bool
	adultW       any
	adultH       any
	adultS       any
	babyW        any
	babyH        any
	babyS        any

	isPlayer       bool
	isFox          bool
	soundInterval  any
	soundSpread    any
	soundEvent     any
	fallMultiplier any

	props []actorProperty
}

var entityTypeMetaCache sync.Map

func entityTypeMetaOf(t world.EntityType) *entityTypeMeta {
	if t != nil && reflect.TypeOf(t).Comparable() {
		if v, ok := entityTypeMetaCache.Load(t); ok {
			return v.(*entityTypeMeta)
		}
		meta := buildEntityTypeMeta(t)
		entityTypeMetaCache.Store(t, meta)
		return meta
	}
	return buildEntityTypeMeta(t)
}

func buildEntityTypeMeta(t world.EntityType) *entityTypeMeta {
	id := networkID(t)
	meta := &entityTypeMeta{
		netID:    id,
		isPlayer: id == "minecraft:player",
		isFox:    id == "minecraft:fox",
		props:    actorPropertyTypes[id],
	}
	var f actorFlags
	if flags, ok := bedrockActorFlags[id]; ok {
		for _, flag := range flags {
			f.set(flag)
		}
	} else {
		f.set(mc.EntityDataFlagHasGravity)
		f.set(mc.EntityDataFlagClimb)
	}
	if g, ok := t.(glint); ok && g.Glint() {
		f.set(mc.EntityDataFlagEnchanted)
	}
	if t == entity.LingeringPotionType {
		f.set(mc.EntityDataFlagLingering)
	}
	meta.baseFlags0 = f.low
	meta.baseFlags0Breathing = f.low | (1 << mc.EntityDataFlagBreathing)
	meta.baseFlags1 = f.high
	meta.baseFlags0Boxed = boxInt64(f.low)
	meta.baseFlags0BreathingBoxed = boxInt64(meta.baseFlags0Breathing)
	meta.baseFlags1Boxed = boxInt64(f.high)

	switch id {
	case "minecraft:slime", "minecraft:magma_cube", "minecraft:salmon", "minecraft:camel", "minecraft:camel_husk":
		meta.dynamicShape = true
	}
	if shapes, ok := bedrockActorShapes[id]; ok {
		meta.hasShape = true
		meta.adultW = boxFloat32(shapes[0].width)
		meta.adultH = boxFloat32(shapes[0].height)
		meta.adultS = boxFloat32(shapes[0].scale)
		if shapes[1] != (bedrockShape{}) {
			meta.hasBabyShape = true
			meta.babyW = boxFloat32(shapes[1].width)
			meta.babyH = boxFloat32(shapes[1].height)
			meta.babyS = boxFloat32(shapes[1].scale)
		}
	}
	if !meta.isPlayer {
		sound, ok := ambientSounds[id]
		if !ok {
			sound = defaultAmbientSound
		}
		meta.soundInterval = boxFloat32(sound.interval)
		meta.soundSpread = boxFloat32(sound.spread)
		meta.soundEvent = sound.event
		if noFallDamage[id] {
			meta.fallMultiplier = metaFloat32Zero
		} else {
			meta.fallMultiplier = metaFloat32One
		}
	}
	return meta
}

// parseEntityMetadata returns an entity metadata object with default values. It is equivalent to setting
// all properties to their default values and disabling all flags.
func (s *Session) parseEntityMetadata(e world.Entity) mc.Metadata {
	t := e.H().Type()
	if t == nil || !reflect.TypeOf(t).Comparable() {
		return s.parseEntityMetadataSlow(e, t)
	}
	meta := entityTypeMetaOf(t)
	m := make(mc.Metadata, 28)

	m[mc.EntityDataKeyPlayerFlags] = boxedBytes[0]
	m[mc.EntityDataKeyEffectColor] = boxedInt32s[1]
	m[mc.EntityDataKeyEffectAmbience] = boxedBytes[0]
	m[mc.EntityDataKeyColorIndex] = boxedBytes[0]

	switch {
	case meta.hasShape && !meta.dynamicShape:
		if meta.hasBabyShape && actorIsBaby(e) {
			m[mc.EntityDataKeyWidth] = meta.babyW
			m[mc.EntityDataKeyHeight] = meta.babyH
			m[mc.EntityDataKeyScale] = meta.babyS
		} else {
			m[mc.EntityDataKeyWidth] = meta.adultW
			m[mc.EntityDataKeyHeight] = meta.adultH
			m[mc.EntityDataKeyScale] = meta.adultS
		}
	case meta.dynamicShape:
		if shape, ok := actorShape(e, meta.netID); ok {
			m[mc.EntityDataKeyWidth] = boxFloat32(shape.width)
			m[mc.EntityDataKeyHeight] = boxFloat32(shape.height)
			m[mc.EntityDataKeyScale] = boxFloat32(shape.scale)
		} else {
			bb := t.BBox(e)
			m[mc.EntityDataKeyWidth] = boxFloat32(float32(bb.Width()))
			m[mc.EntityDataKeyHeight] = boxFloat32(float32(bb.Height()))
		}
	default:
		bb := t.BBox(e)
		m[mc.EntityDataKeyWidth] = boxFloat32(float32(bb.Width()))
		m[mc.EntityDataKeyHeight] = boxFloat32(float32(bb.Height()))
	}

	if !meta.isPlayer {
		if meta.isFox {
			if sl, ok := e.(interface{ Sleeping() bool }); ok && sl.Sleeping() {
				m[mc.EntityDataKeyAmbientSoundInterval] = metaSleepSoundInterval
				m[mc.EntityDataKeyAmbientSoundIntervalRange] = metaSleepSoundSpread
				m[mc.EntityDataKeyAmbientSoundEventName] = metaSleepSoundEvent
			} else {
				m[mc.EntityDataKeyAmbientSoundInterval] = meta.soundInterval
				m[mc.EntityDataKeyAmbientSoundIntervalRange] = meta.soundSpread
				m[mc.EntityDataKeyAmbientSoundEventName] = meta.soundEvent
			}
		} else {
			m[mc.EntityDataKeyAmbientSoundInterval] = meta.soundInterval
			m[mc.EntityDataKeyAmbientSoundIntervalRange] = meta.soundSpread
			m[mc.EntityDataKeyAmbientSoundEventName] = meta.soundEvent
		}
		m[mc.EntityDataKeyFallDamageMultiplier] = meta.fallMultiplier
	}

	var seat world.RiderSeat
	if r, ok := e.(riderSeat); ok {
		seat = r.RiderSeat()
	}
	writeSeatMetadata(m, seat)

	flags := actorFlags{low: meta.baseFlags0, high: meta.baseFlags1}
	flags = s.addSpecificMetadataFlags(e, m, flags)
	if beh, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
		flags = s.addSpecificMetadataFlags(beh.Behaviour(), m, flags)
	}
	switch flags.low {
	case meta.baseFlags0:
		m[mc.EntityDataKeyFlags] = meta.baseFlags0Boxed
	case meta.baseFlags0Breathing:
		m[mc.EntityDataKeyFlags] = meta.baseFlags0BreathingBoxed
	default:
		m[mc.EntityDataKeyFlags] = boxInt64(flags.low)
	}
	if flags.high == meta.baseFlags1 {
		m[mc.EntityDataKeyFlagsTwo] = meta.baseFlags1Boxed
	} else {
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(flags.high)
	}
	s.writeActorAppearance(e, m)
	return m
}

func (s *Session) parseEntityMetadataSlow(e world.Entity, t world.EntityType) mc.Metadata {
	bb := t.BBox(e)
	m := make(mc.Metadata, 28)
	m[mc.EntityDataKeyFlags] = metaInt64Zero
	m[mc.EntityDataKeyFlagsTwo] = metaInt64Zero
	m[mc.EntityDataKeyPlayerFlags] = boxedBytes[0]
	m[mc.EntityDataKeyWidth] = float32(bb.Width())
	m[mc.EntityDataKeyHeight] = float32(bb.Height())
	m[mc.EntityDataKeyEffectColor] = boxedInt32s[1]
	m[mc.EntityDataKeyEffectAmbience] = boxedBytes[0]
	m[mc.EntityDataKeyColorIndex] = boxedBytes[0]

	setActorFlag(m, mc.EntityDataFlagHasGravity)
	setActorFlag(m, mc.EntityDataFlagClimb)
	writeActorShape(e, networkID(t), m)
	if g, ok := t.(glint); ok && g.Glint() {
		setActorFlag(m, mc.EntityDataFlagEnchanted)
	}
	if t == entity.LingeringPotionType {
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
	f0, has0 := m[mc.EntityDataKeyFlags].(int64)
	f1, has1 := m[mc.EntityDataKeyFlagsTwo].(int64)
	f := s.addSpecificMetadataFlags(e, m, actorFlags{low: f0, high: f1})
	if f.touchedLow {
		if !has0 {
			_ = m[mc.EntityDataKeyFlags].(int64)
		}
		m[mc.EntityDataKeyFlags] = boxInt64(f.low)
	}
	if f.touchedHigh {
		if !has1 {
			_ = m[mc.EntityDataKeyFlagsTwo].(int64)
		}
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(f.high)
	}
}

func (s *Session) addSpecificMetadataFlags(e any, m mc.Metadata, f actorFlags) actorFlags {
	t := reflect.TypeOf(e)
	if ws, ok := metadataWritersOf.Load(t); ok {
		for _, w := range ws.([]metadataWriter) {
			f, _ = w(s, e, m, f)
		}
		return f
	}
	var ws []metadataWriter
	for _, w := range metadataWriters {
		var matched bool
		f, matched = w(s, e, m, f)
		if matched {
			ws = append(ws, w)
		}
	}
	metadataWritersOf.Store(t, ws)
	return f
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
type metadataWriter func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool)

// metadataWriters are the writers of the metadata of each kind of entity, in
// the order addSpecificMetadata runs them: a later one may overwrite what an
// earlier one wrote.
var metadataWriters = [...]metadataWriter{
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		sheep, ok := e.(*entity.Sheep)
		if ok {
			m[mc.EntityDataKeyColorIndex] = boxByte(byte(sheep.WoolColor()))
			if sheep.Sheared() {
				f.set(mc.EntityDataFlagSheared)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		shulker, ok := e.(*entity.Shulker)
		if ok {
			// BDS uses Variant for shell colour and separate attachment metadata.
			m[mc.EntityDataKeyVariant] = boxInt32(int32(shulker.Color()))
			m[mc.EntityDataKeyPeekID] = boxInt32(int32(shulker.Peek()))
			m[mc.EntityDataKeyAttachFace] = boxByte(byte(shulker.AttachFace()))
			m[mc.EntityDataKeyAttached] = boxedInt16s[1]
			pos := cube.PosFromVec3(shulker.Position())
			m[mc.EntityDataKeyAttachedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		parrot, ok := e.(*entity.Parrot)
		if ok {
			m[mc.EntityDataKeyVariant] = boxInt32(int32(parrot.Variant()))
			if parrot.Tamed() {
				f.set(mc.EntityDataFlagTamed)
			}
			if parrot.Party() {
				f.set(mc.EntityDataFlagDancing)
			}
			if parrot.Sitting() {
				f.set(mc.EntityDataFlagSitting)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		sn, ok := e.(sneaker)
		if ok && sn.Sneaking() {
			f.set(mc.EntityDataFlagSneaking)
			if b, ok := e.(blocker); ok {
				if _, blocking := b.Blocking(); blocking {
					f.set(mc.EntityDataFlagBlocking)
				}
			}
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		sp, ok := e.(sprinter)
		if ok && sp.Sprinting() {
			f.set(mc.EntityDataFlagSprinting)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		sw, ok := e.(swimmer)
		if ok && sw.Swimming() {
			f.set(mc.EntityDataFlagSwimming)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		cr, ok := e.(crawler)
		if ok && cr.Crawling() {
			f.set(mc.EntityDataFlagCrawling)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		gl, ok := e.(glider)
		if ok && gl.Gliding() {
			f.set(mc.EntityDataFlagGliding)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		bb, ok := e.(baby)
		if ok && bb.Baby() {
			f.set(mc.EntityDataFlagBaby)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		l, ok := e.(interface{ InLove() bool })
		if ok && l.InLove() {
			f.set(mc.EntityDataFlagInLove)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		b, ok := e.(breather)
		if ok {
			m[mc.EntityDataKeyAirSupply] = boxInt16(int16(b.AirSupply().Milliseconds() / 50))
			m[mc.EntityDataKeyAirSupplyMax] = boxInt16(int16(b.MaxAirSupply().Milliseconds() / 50))
			if b.Breathing() {
				f.set(mc.EntityDataFlagBreathing)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		i, ok := e.(invisible)
		eff, bearer := e.(effectBearer)
		if ok {
			if i.Invisible() {
				f.set(mc.EntityDataFlagInvisible)
			}
		} else if bearer {
			for _, ef := range eff.Effects() {
				if ef.Type() == effect.Invisibility {
					f.set(mc.EntityDataFlagInvisible)
					break
				}
			}
		}
		return f, ok || bearer
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		i, ok := e.(immobile)
		if ok && i.Immobile() {
			f.set(mc.EntityDataFlagNoAI)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		o, ok := e.(onFire)
		if ok && o.OnFireDuration() > 0 {
			f.set(mc.EntityDataFlagOnFire)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		fr, ok := e.(interface{ FreezingStrength() float32 })
		if ok {
			m[mc.EntityDataKeyFreezingEffectStrength] = boxFloat32(fr.FreezingStrength())
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		sh, ok := e.(interface{ Shaking() bool })
		if ok && sh.Shaking() {
			f.set(mc.EntityDataFlagShaking)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		r, ok := e.(interface{ RamAttacking() bool })
		if ok && r.RamAttacking() {
			f.set(mc.EntityDataFlagRamAttack)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		w, ok := e.(interface {
			WardenPose() (emerging, digging, roaring, sniffing, sonicBoom bool)
			HeartbeatInterval() int32
		})
		if ok {
			emerging, digging, roaring, sniffing, sonicBoom := w.WardenPose()
			if emerging {
				f.set(mc.EntityDataFlagEmerging)
			}
			if digging {
				f.set(mc.EntityDataFlagDigging)
			}
			if roaring {
				f.set(mc.EntityDataFlagRoaring)
			}
			if sniffing {
				f.set(mc.EntityDataFlagSniffing)
			}
			if sonicBoom {
				f.set(mc.EntityDataFlagSonicBoom)
			}
			m[mc.EntityDataKeyHeartbeatIntervalTicks] = boxInt32(w.HeartbeatInterval())
			// LevelSoundEvent heartbeat.
			m[mc.EntityDataKeyHeartbeatSoundEvent] = metaInt32Heartbeat
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		sh, ok := e.(interface{ Shooting() bool })
		if ok && sh.Shooting() {
			// A breeze shooting faces its target to attack it from range.
			f.set(mc.EntityDataFlagFacingTargetToRangeAttack)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		j, ok := e.(interface{ JumpGoalJumping() bool })
		if ok && j.JumpGoalJumping() {
			f.set(mc.EntityDataFlagJumpGoal)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		sn, ok := e.(interface{ SnifferState() string })
		if ok {
			if flag, ok := snifferStateFlags[sn.SnifferState()]; ok {
				f.set(flag)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		p, ok := e.(interface{ PlayingDead() bool })
		if ok && p.PlayingDead() {
			f.set(mc.EntityDataFlagPlayingDead)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ Croaking() bool })
		if ok && c.Croaking() {
			f.set(mc.EntityDataFlagCroaking)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ EatingMob() bool })
		if ok && c.EatingMob() {
			// Bedrock's EAT_MOB flag shows a frog's tongue.
			f.set(mc.EntityDataFlagDigestMob)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		r, ok := e.(interface{ Resting() bool })
		if ok && r.Resting() {
			// Bats hang upside down while resting.
			f.set(mc.EntityDataFlagResting)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		db, ok := e.(doorBreaker)
		if ok && db.CanBreakDoors() {
			f.set(mc.EntityDataFlagDoorBreaker)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		br, ok := e.(breakingObstruction)
		if ok && br.BreakingObstruction() {
			f.set(mc.EntityDataFlagBreakingObstruction)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		ar, ok := e.(armsRaised)
		if ok && ar.ArmsRaised() {
			f.set(mc.EntityDataFlagCharging)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		u, ok := e.(using)
		if ok && u.UsingItem() {
			f.set(mc.EntityDataFlagUsingItem)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		if bs, ok := e.(bedSleeper); ok {
			return writeBedSleeperMetadataFlags(bs, m, f), true
		}
		sl, ok := e.(sleeper)
		if ok {
			if pos, ok := sl.Sleeping(); ok {
				m[mc.EntityDataKeyBedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}

				// For some reason there is no such flag in gophertunnel.
				m.SetFlag(mc.EntityDataKeyPlayerFlags, 1)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(arrow)
		if ok && c.Critical() {
			f.set(mc.EntityDataFlagCritical)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		g, ok := e.(gameMode)
		if ok {
			if g.GameMode().HasCollision() {
				f.set(mc.EntityDataFlagHasCollision)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		o, ok := e.(orb)
		if ok {
			m[mc.EntityDataKeyValue] = boxInt32(int32(o.Experience()))
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		fw, ok := e.(firework)
		o, isOwned := e.(owned)
		if ok {
			m[mc.EntityDataKeyDisplayFirework] = networkItemNBT(item.NewStack(fw.Firework(), 1))
			if isOwned && fw.Attached() && o.Owner() != nil {
				m[mc.EntityDataKeyCustomDisplay] = s.handleUniqueID(o.Owner())
			}
		} else if isOwned && o.Owner() != nil {
			m[mc.EntityDataKeyOwner] = s.handleUniqueID(o.Owner())
		}
		return f, ok || isOwned
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		l, ok := e.(leashed)
		if ok {
			if id := l.HolderLeashID(); id != 0 {
				if s.ent != nil && id == s.ent.UniqueID() {
					id = int64(selfEntityRuntimeID)
				}
				m[mc.EntityDataKeyLeashHolder] = id
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		sc, ok := e.(scaled)
		if ok {
			m[mc.EntityDataKeyScale] = boxFloat32(float32(sc.Scale()))
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		t, ok := e.(tnt)
		if ok {
			m[mc.EntityDataKeyFuseTime] = boxInt32(int32(t.Fuse().Milliseconds() / 50))
			f.set(mc.EntityDataFlagIgnited)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		i, ok := e.(igniteable)
		if ok && i.Ignited() {
			m[mc.EntityDataKeyFuseTime] = boxInt32(int32(i.FuseTicks()))
			f.set(mc.EntityDataFlagIgnited)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		p, ok := e.(interface{ Powered() bool })
		if ok && p.Powered() {
			f.set(mc.EntityDataFlagPowered)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ WallClimbing() bool })
		if ok && c.WallClimbing() {
			f.set(mc.EntityDataFlagWallClimbing)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		// A blaze charging its fireballs burns, as Blaze.isOnFire shows it.
		b, ok := e.(interface{ Burning() bool })
		if ok && b.Burning() {
			f.set(mc.EntityDataFlagOnFire)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ Charging() bool })
		if ok && c.Charging() {
			f.set(mc.EntityDataFlagCharging)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ CrossbowCharged() bool })
		if ok && c.CrossbowCharged() {
			f.set(mc.EntityDataFlagCharged)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ Celebrating() bool })
		if ok && c.Celebrating() {
			f.set(mc.EntityDataFlagCelebrating)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface{ CelebratingSpecial() bool })
		if ok && c.CelebratingSpecial() {
			f.set(mc.EntityDataFlagCelebratingSpecial)
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		a, ok := e.(interface{ Admiring() bool })
		if ok && a.Admiring() {
			f.set(mc.EntityDataFlagAdmiring)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(interface {
			Casting() bool
			SpellColour() [3]float64
		})
		if ok && c.Casting() {
			f.set(mc.EntityDataFlagCasting)
			colour := c.SpellColour()
			m[mc.EntityDataKeyDataSpellCastingColor] = int32(-1<<24 | int32(colour[0]*255)<<16 | int32(colour[1]*255)<<8 | int32(colour[2]*255))
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		g, ok := e.(interface{ BeamTargetHandle() *world.EntityHandle })
		if ok {
			if h := g.BeamTargetHandle(); h != nil {
				m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
			}
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		g, ok := e.(interface{ ShootTargetHandle() *world.EntityHandle })
		if ok {
			if h := g.ShootTargetHandle(); h != nil {
				m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
			} else {
				m[mc.EntityDataKeyTarget] = metaInt64Zero
			}
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		fr, ok := e.(interface{ TongueTargetHandle() *world.EntityHandle })
		if ok {
			// The target is cleared once the tongue is back, as Frog.eraseTongueTarget.
			if h := fr.TongueTargetHandle(); h != nil {
				m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
			} else {
				m[mc.EntityDataKeyTarget] = metaInt64Zero
			}
		}
		return f, ok
	},
	func(_ *Session, e any, _ mc.Metadata, f actorFlags) (actorFlags, bool) {
		r, ok := e.(interface {
			Stunned() bool
			Roaring() bool
		})
		if ok {
			if r.Stunned() {
				f.set(mc.EntityDataFlagStunned)
			}
			if r.Roaring() {
				f.set(mc.EntityDataFlagRoaring)
			}
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		d, ok := e.(displayTile)
		if ok {
			var display int32
			if b, ok := d.DisplayBlock(); ok {
				display = int32(s.blockHash(b))
			}
			m[mc.EntityDataKeyDisplayBlockState] = boxInt32(display)
			m[mc.EntityDataKeyDisplayOffset] = boxInt32(int32(d.DisplayOffset()))
			m[mc.EntityDataKeyCustomDisplay] = boxBoolByte(d.CustomDisplay())
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(containerMeta)
		if ok {
			if typ, ok := actorContainerType(c.ContainerKind()); ok {
				// The dedicated server sends the container type as a byte.
				m[mc.EntityDataKeyContainerType] = boxByte(typ)
				m[mc.EntityDataKeyContainerSize] = boxInt32(c.ContainerSize())
				m[mc.EntityDataKeyContainerStrengthModifier] = boxInt32(c.ContainerStrengthModifier())
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		h, ok := e.(minecartDamage)
		if ok {
			m[mc.EntityDataKeyHurt] = boxInt32(int32(h.RollingAmplitude()))
			m[mc.EntityDataKeyHurtDirection] = boxInt32(int32(h.RollingDirection()))
			// BDS sends the damage of a vehicle as an int.
			m[mc.EntityDataKeyStructuralIntegrity] = boxInt32(int32(h.Damage()))
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		r, ok := e.(rowTime)
		if ok {
			m[mc.EntityDataKeyRowTimeLeft] = boxFloat32(r.RowTimeLeft())
			m[mc.EntityDataKeyRowTimeRight] = boxFloat32(r.RowTimeRight())
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		b, ok := e.(buoyantMeta)
		if ok {
			buoyant := b.Buoyant()
			m[mc.EntityDataKeyIsBuoyant] = boxBoolByte(buoyant)
			if buoyant {
				m[mc.EntityDataKeyBuoyancyData] = metaBuoyancy
				// The driver of a buoyant vehicle sits in its first seat.
				m[mc.EntityDataKeyControllingSeatIndex] = boxedBytes[0]
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		text, ok := interactTextBoxed(e)
		if text != nil {
			m[mc.EntityDataKeyInteractText] = text
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		r, ok := e.(rideTarget)
		if ok {
			m[mc.EntityDataKeyCanRideTarget] = boxBoolByte(r.CanRideTarget())
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		w, ok := e.(witherMeta)
		if ok {
			// The heads of a wither turn to their targets, and it glows blue
			// while it is summoned.
			for i, h := range w.HeadTargets() {
				var id int64
				if h != nil {
					id = int64(s.handleRuntimeID(h))
				}
				m[witherTargetKeys[i]] = boxInt64(id)
			}
			m[mc.EntityDataKeyInvulnerableTicks] = boxInt32(int32(w.InvulnerableTicks()))
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		ec, ok := e.(endCrystalMeta)
		if ok {
			if ec.ShowBase() {
				f.set(mc.EntityDataFlagShowBottom)
			}
			if target, ok := ec.BeamTarget(); ok {
				m[mc.EntityDataKeyBlockTarget] = mc.BlockPos{
					int32(math.Floor(target[0])),
					int32(math.Floor(target[1])),
					int32(math.Floor(target[2])),
				}
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		nameTag, alwaysShow, ok := nameTagState(e)
		if ok {
			f = writeNameTagMetadataFlags(m, f, nameTag, alwaysShow)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		sc, ok := e.(scoreTag)
		if ok {
			m[mc.EntityDataKeyScore] = boxString(sc.ScoreTag())
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		c, ok := e.(areaEffectCloud)
		if ok {
			m[mc.EntityDataKeyDataRadius] = boxFloat32(float32(c.Radius()))

			// We purposely fill these in with invalid values to disable the client-sided shrinking of the cloud.
			m[mc.EntityDataKeyDataDuration] = metaInt32Max
			m[mc.EntityDataKeyDataChangeOnPickup] = metaFloat32Min
			m[mc.EntityDataKeyDataChangeRate] = metaFloat32Min
			if p, ok := e.(interface{ Particle() entity.CloudParticle }); ok {
				if particle, ok := cloudParticleType(p.Particle()); ok {
					m[mc.EntityDataKeyDataParticle] = boxInt32(particle)
				}
			}

			colour, am := effect.ResultingColour(c.Effects())
			m[mc.EntityDataKeyEffectColor] = boxInt32(int32FromRGBA(colour))
			m[mc.EntityDataKeyEffectAmbience] = boxBoolByte(am)
		}
		return f, ok
	},
	func(s *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		l, ok := e.(living)
		if ok && s.ent != nil && s.ent.UUID() == l.UUID() {
			deathPos, deathDimension, died := l.DeathPosition()
			if died {
				dim, _ := world.DimensionID(deathDimension)
				m[mc.EntityDataKeyPlayerLastDeathPosition] = vec64To32(deathPos)
				m[mc.EntityDataKeyPlayerLastDeathDimension] = boxInt32(int32(dim))
			}
			m[mc.EntityDataKeyPlayerHasDied] = boxBoolByte(died)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		p, ok := e.(splash)
		if ok {
			tip := p.Potion().Uint8()
			m[mc.EntityDataKeyAuxValueData] = boxInt16(int16(tip))
			if tip > 4 {
				m[mc.EntityDataKeyCustomDisplay] = boxByte(tip + 1)
			}
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		eff, ok := e.(effectBearer)
		if ok {
			m[mc.EntityDataKeyVisibleMobEffects] = boxInt64(packVisibleEffects(eff.Effects()))
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		v, ok := e.(variable)
		if ok {
			m[mc.EntityDataKeyVariant] = boxInt32(v.Variant())
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		mv, ok := e.(markVariable)
		if ok {
			m[mc.EntityDataKeyMarkVariant] = boxInt32(mv.MarkVariant())
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		mount, ok := e.(interface{ MountState() entity.MountState })
		if ok {
			f = addMountMetadataFlags(mount.MountState(), m, f)
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		iv, ok := e.(hurtDuration)
		if ok {
			m[mc.EntityDataKeyHurt] = boxInt32(int32(iv.HurtDuration().Milliseconds() / 50))
		}
		return f, ok
	},
	func(_ *Session, e any, m mc.Metadata, f actorFlags) (actorFlags, bool) {
		pi, ok := e.(armourStand)
		if ok {
			m[mc.EntityDataKeyPoseIndex] = boxInt32(int32(pi.PoseIndex()))
		}
		return f, ok
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
	f0, has0 := m[mc.EntityDataKeyFlags].(int64)
	f1, has1 := m[mc.EntityDataKeyFlagsTwo].(int64)
	f := addMountMetadataFlags(s, m, actorFlags{low: f0, high: f1})
	if f.touchedLow {
		if !has0 {
			_ = m[mc.EntityDataKeyFlags].(int64)
		}
		m[mc.EntityDataKeyFlags] = boxInt64(f.low)
	}
	if f.touchedHigh {
		if !has1 {
			_ = m[mc.EntityDataKeyFlagsTwo].(int64)
		}
		m[mc.EntityDataKeyFlagsTwo] = boxInt64(f.high)
	}
}

func addMountMetadataFlags(s entity.MountState, m mc.Metadata, f actorFlags) actorFlags {
	if s.Saddled {
		f.set(mc.EntityDataFlagSaddled)
	}
	if s.Tamed {
		f.set(mc.EntityDataFlagTamed)
	}
	if s.Chested {
		f.set(mc.EntityDataFlagChested)
	}
	if s.Sitting {
		f.set(mc.EntityDataFlagSitting)
	}
	if s.Controlled {
		f.set(mc.EntityDataFlagKeyboardControlled)
	}
	if s.PowerJump {
		f.set(mc.EntityDataFlagPowerJump)
	}
	if s.Dash {
		f.set(mc.EntityDataFlagDash)
	}
	if s.Rearing {
		var flags int64
		if s.Eating {
			flags |= horseFlagEating
		}
		if s.Standing {
			flags |= horseFlagStanding
		}
		m[mc.EntityDataKeyHorseFlags] = boxInt64(flags)
	}
	if s.ContainerSize > 0 {
		m[mc.EntityDataKeyContainerType] = boxByte(byte(mc.ContainerTypeHorse))
		m[mc.EntityDataKeyContainerSize] = boxInt32(s.ContainerSize)
		// BDS sends a strength modifier of 3 for tamed llamas, the only
		// mounts with a strength, and 0 for the others.
		var modifier int32
		if s.MaxStrength != 0 {
			modifier = 3
		}
		m[mc.EntityDataKeyContainerStrengthModifier] = boxInt32(modifier)
	}
	if s.MaxStrength > 0 {
		m[mc.EntityDataKeyStrength] = boxInt32(s.Strength)
		m[mc.EntityDataKeyStrengthMax] = boxInt32(s.MaxStrength)
	}
	return f
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

	m[mc.EntityDataKeyName] = boxString(nameTag)
	m[mc.EntityDataKeyAlwaysShowNameTag] = boxBoolByte(always)
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

func writeNameTagMetadataFlags(m mc.Metadata, f actorFlags, nameTag string, alwaysShow bool) actorFlags {
	show := nameTag != ""
	always := show && alwaysShow

	m[mc.EntityDataKeyName] = boxString(nameTag)
	m[mc.EntityDataKeyAlwaysShowNameTag] = boxBoolByte(always)
	if show {
		f.set(mc.EntityDataFlagShowName)
	} else {
		f.unset(mc.EntityDataFlagShowName)
	}
	if always {
		f.set(mc.EntityDataFlagAlwaysShowName)
	} else {
		f.unset(mc.EntityDataFlagAlwaysShowName)
	}
	return f
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

// bedSleeper is a mob that lies in a bed, as villagers do. BDS shows such a
// mob with the SLEEPING flag and the small box of the behaviour.sleep
// component, in place of the bed position and player flag of players.
type bedSleeper interface {
	sleeper
	// SleepFacing returns the direction from the foot of the bed to its head.
	SleepFacing() cube.Direction
}

// Width and height of the box BDS gives a villager lying in a bed
// (sleep_collider_width and sleep_collider_height).
const (
	sleepColliderWidth  = 1
	sleepColliderHeight = 0.3
)

// writeBedSleeperMetadataFlags writes the state of a mob in a bed, if it sleeps.
func writeBedSleeperMetadataFlags(bs bedSleeper, m mc.Metadata, f actorFlags) actorFlags {
	if _, ok := bs.Sleeping(); !ok {
		return f
	}
	f.set(mc.EntityDataFlagSleeping)
	m[mc.EntityDataKeyWidth], m[mc.EntityDataKeyHeight] = metaSleepWidth, metaSleepHeight
	return f
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
	v, ok := interactTextBoxed(e)
	if v == nil {
		return "", ok
	}
	return v.(string), ok
}

func interactTextBoxed(e any) (any, bool) {
	if _, ok := e.(containerMeta); ok {
		return metaInteractContainer, true
	}
	switch v := e.(type) {
	case interface{ Full() bool }:
		if !v.Full() {
			return metaInteractBoat, true
		}
		return nil, true
	case interface {
		Rideable() bool
		Passenger() *world.EntityHandle
	}:
		if v.Rideable() && v.Passenger() == nil {
			return metaInteractMinecart, true
		}
		return nil, true
	}
	return nil, false
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
	m[mc.EntityDataKeySeatLockPassengerRotation] = boxBoolByte(seat.LockRotation)
	m[mc.EntityDataKeySeatLockPassengerRotationDegrees] = boxFloat32(float32(seat.LockRotationDegrees))
	m[mc.EntityDataKeySeatRotationOffset] = boxBoolByte(seat.RotateRider)
	m[mc.EntityDataKeySeatRotationOffsetDegrees] = boxFloat32(float32(seat.RotateRiderDegrees))
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
	if len(effects) == 0 {
		return 0
	}
	type visible struct {
		id      int
		ambient bool
	}
	var stackBuf [16]visible
	shown := stackBuf[:0]
	for _, ef := range effects {
		if id, found := effect.ID(ef.Type()); found && !ef.ParticlesHidden() {
			shown = append(shown, visible{id: id, ambient: ef.Ambient()})
		}
	}
	if len(shown) == 0 {
		return 0
	}
	if len(shown) > 1 {
		slices.SortFunc(shown, func(a, b visible) int { return cmp.Compare(a.id, b.id) })
	}
	var packed int64
	for _, v := range shown {
		packed = packed<<7 | int64(v.id<<1)
		if v.ambient {
			packed |= 1
		}
	}
	return packed
}
