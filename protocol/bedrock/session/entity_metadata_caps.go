package session

import (
	"maps"
	"math"
	"sync"
	"sync/atomic"
	"unsafe"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/entity"
	"github.com/dm-vev/adamant/server/entity/effect"
	"github.com/dm-vev/adamant/server/item"
	"github.com/dm-vev/adamant/server/world"
)

type efaceHeader struct {
	typ  uintptr
	data unsafe.Pointer
}

// efaceType returns the runtime type descriptor word for a non-nil interface value, or 0 for nil.
func efaceType(v any) uintptr {
	return (*efaceHeader)(unsafe.Pointer(&v)).typ
}

// metaCaps is a 128-bit per-concrete-Go-type capability bitmask computed once per
// concrete type of world.Entity or entity.Behaviour. It replaces repeated
// reflect.TypeOf calls, sync.Map lookups, and failed interface assertions on the
// hot entity metadata path with single register bitmask tests.
type metaCaps struct {
	lo, hi uint64
}

// Named interfaces for anonymous assertions shared between capability detection and dispatch.
type (
	inLoveMeta       interface{ InLove() bool }
	freezingMeta     interface{ FreezingStrength() float32 }
	shakingMeta      interface{ Shaking() bool }
	ramAttackingMeta interface{ RamAttacking() bool }
	wardenMeta       interface {
		WardenPose() (emerging, digging, roaring, sniffing, sonicBoom bool)
		HeartbeatInterval() int32
	}
	shootingMeta           interface{ Shooting() bool }
	jumpGoalMeta           interface{ JumpGoalJumping() bool }
	snifferStateMeta       interface{ SnifferState() string }
	playingDeadMeta        interface{ PlayingDead() bool }
	croakingMeta           interface{ Croaking() bool }
	eatingMobMeta          interface{ EatingMob() bool }
	restingMeta            interface{ Resting() bool }
	poweredMeta            interface{ Powered() bool }
	wallClimbingMeta       interface{ WallClimbing() bool }
	burningMeta            interface{ Burning() bool }
	chargingMeta           interface{ Charging() bool }
	crossbowChargedMeta    interface{ CrossbowCharged() bool }
	celebratingMeta        interface{ Celebrating() bool }
	celebratingSpecialMeta interface{ CelebratingSpecial() bool }
	admiringMeta           interface{ Admiring() bool }
	castingMeta            interface {
		Casting() bool
		SpellColour() [3]float64
	}
	beamTargetMeta   interface{ BeamTargetHandle() *world.EntityHandle }
	shootTargetMeta  interface{ ShootTargetHandle() *world.EntityHandle }
	tongueTargetMeta interface{ TongueTargetHandle() *world.EntityHandle }
	ravagerMeta      interface {
		Stunned() bool
		Roaring() bool
	}
	boatFullMeta         interface{ Full() bool }
	minecartRideableMeta interface {
		Rideable() bool
		Passenger() *world.EntityHandle
	}
	cloudParticleMeta  interface{ Particle() entity.CloudParticle }
	mountStateMeta     interface{ MountState() entity.MountState }
	behaviourHolder    interface{ Behaviour() entity.Behaviour }
	foxSleeper         interface{ Sleeping() bool }
	climateVariantMeta interface{ ClimateVariant() string }
	soundVariantMeta   interface{ SoundVariant() string }
)

// Low 64 bits of metaCaps, grouped in exact metadataWriters evaluation order.
const (
	// Chunk 1: writers 0..15.
	capSheep uint64 = 1 << iota
	capShulker
	capParrot
	capSneaker
	capBlocker
	capSprinter
	capSwimmer
	capCrawler
	capGlider
	capBaby
	capInLove
	capBreather
	capInvisible
	capEffectBearer
	capImmobile
	capOnFire

	// Chunk 2: writers 16..30.
	capFreezing
	capShaking
	capRamAttacking
	capWarden
	capShooting
	capJumpGoal
	capSniffer
	capPlayingDead
	capCroaking
	capEatingMob
	capResting
	capDoorBreaker
	capBreakingObstruction
	capArmsRaised
	capUsing

	// Chunk 3: writers 31..46.
	capBedSleeper
	capSleeper
	capArrow
	capGameMode
	capOrb
	capFirework
	capOwned
	capLeashed
	capScaled
	capTNT
	capIgniteable
	capPowered
	capWallClimbing
	capBurning
	capCharging
	capCrossbowCharged

	// Chunk 4: writers 47..63.
	capCelebrating
	capCelebratingSpecial
	capAdmiring
	capCasting
	capBeamTarget
	capShootTarget
	capTongueTarget
	capRavager
	capDisplayTile
	capContainerMeta
	capMinecartDamage
	capRowTime
	capBuoyantMeta
	capBoatFull
	capMinecartRideable
	capRideTarget
	capWither
)

const (
	maskChunk1 = capSheep | capShulker | capParrot | capSneaker | capSprinter | capSwimmer |
		capCrawler | capGlider | capBaby | capInLove | capBreather | capInvisible |
		capEffectBearer | capImmobile | capOnFire
	maskChunk2 = capFreezing | capShaking | capRamAttacking | capWarden | capShooting |
		capJumpGoal | capSniffer | capPlayingDead | capCroaking | capEatingMob |
		capResting | capDoorBreaker | capBreakingObstruction | capArmsRaised | capUsing
	maskChunk3 = capBedSleeper | capSleeper | capArrow | capGameMode | capOrb |
		capFirework | capOwned | capLeashed | capScaled | capTNT | capIgniteable |
		capPowered | capWallClimbing | capBurning | capCharging | capCrossbowCharged
	maskChunk4 = capCelebrating | capCelebratingSpecial | capAdmiring | capCasting |
		capBeamTarget | capShootTarget | capTongueTarget | capRavager | capDisplayTile |
		capContainerMeta | capMinecartDamage | capRowTime | capBuoyantMeta |
		capBoatFull | capMinecartRideable | capRideTarget | capWither

	// capSessionRefLo covers every lo capability that can embed a viewer-relative
	// selfEntityRuntimeID (1) or per-session handleRuntimeID in metadata.
	capSessionRefLo = capOwned | capLeashed | capBeamTarget | capShootTarget | capTongueTarget | capWither
)

// High 64 bits of metaCaps: Chunk 5 writers plus appearance, property, and viewer helpers.
const (
	// Chunk 5: remaining metadata writers.
	capEndCrystal uint64 = 1 << iota
	capAlwaysShowNameTag
	capNamed
	capScoreTag
	capAreaEffectCloud
	capCloudParticle
	capLiving
	capSplash
	capVariable
	capMarkVariable
	capMountState
	capHurtDuration
	capArmourStand

	// Extra hot-path capabilities outside addSpecificMetadataFlags.
	capBehaviour
	capRiderSeat
	capFoxSleeping
	capMobHealth
	capClimateVariant
	capSoundVariant
	capCreakingState
	capAppearance
	capPropEntity
	capMetadataContributor
)

const (
	maskChunk5 = capEndCrystal | capNamed | capScoreTag | capAreaEffectCloud |
		capLiving | capSplash | capVariable | capMarkVariable | capMountState |
		capHurtDuration | capArmourStand
	maskPropCaps = capClimateVariant | capSoundVariant | capCreakingState | capPropEntity
)

var (
	metaCapsMu    sync.Mutex
	metaCapsTable atomic.Pointer[map[uintptr]metaCaps]
)

func init() {
	m := make(map[uintptr]metaCaps, 128)
	metaCapsTable.Store(&m)
}

// metaCapsOf returns the cached capability bitmask for e's concrete Go type.
func metaCapsOf(e any) metaCaps {
	tp := efaceType(e)
	if tp == 0 {
		return metaCaps{}
	}
	if caps, ok := (*metaCapsTable.Load())[tp]; ok {
		return caps
	}
	return metaCapsSlow(e, tp)
}

func metaCapsSlow(e any, tp uintptr) metaCaps {
	caps := metaCaps{
		lo: computeMetaCapsLo1(e) | computeMetaCapsLo2(e) | computeMetaCapsLo3(e),
		hi: computeMetaCapsHi(e),
	}
	metaCapsMu.Lock()
	old := *metaCapsTable.Load()
	if existing, ok := old[tp]; ok {
		metaCapsMu.Unlock()
		return existing
	}
	next := make(map[uintptr]metaCaps, len(old)+1)
	maps.Copy(next, old)
	next[tp] = caps
	metaCapsTable.Store(&next)
	metaCapsMu.Unlock()
	return caps
}

func computeMetaCapsLo1(e any) (lo uint64) {
	if _, ok := e.(*entity.Sheep); ok {
		lo |= capSheep
	}
	if _, ok := e.(*entity.Shulker); ok {
		lo |= capShulker
	}
	if _, ok := e.(*entity.Parrot); ok {
		lo |= capParrot
	}
	if _, ok := e.(sneaker); ok {
		lo |= capSneaker
	}
	if _, ok := e.(blocker); ok {
		lo |= capBlocker
	}
	if _, ok := e.(sprinter); ok {
		lo |= capSprinter
	}
	if _, ok := e.(swimmer); ok {
		lo |= capSwimmer
	}
	if _, ok := e.(crawler); ok {
		lo |= capCrawler
	}
	if _, ok := e.(glider); ok {
		lo |= capGlider
	}
	if _, ok := e.(baby); ok {
		lo |= capBaby
	}
	if _, ok := e.(inLoveMeta); ok {
		lo |= capInLove
	}
	if _, ok := e.(breather); ok {
		lo |= capBreather
	}
	if _, ok := e.(invisible); ok {
		lo |= capInvisible
	}
	if _, ok := e.(effectBearer); ok {
		lo |= capEffectBearer
	}
	if _, ok := e.(immobile); ok {
		lo |= capImmobile
	}
	if _, ok := e.(onFire); ok {
		lo |= capOnFire
	}
	if _, ok := e.(freezingMeta); ok {
		lo |= capFreezing
	}
	if _, ok := e.(shakingMeta); ok {
		lo |= capShaking
	}
	if _, ok := e.(ramAttackingMeta); ok {
		lo |= capRamAttacking
	}
	if _, ok := e.(wardenMeta); ok {
		lo |= capWarden
	}
	if _, ok := e.(shootingMeta); ok {
		lo |= capShooting
	}
	return lo
}

func computeMetaCapsLo2(e any) (lo uint64) {
	if _, ok := e.(jumpGoalMeta); ok {
		lo |= capJumpGoal
	}
	if _, ok := e.(snifferStateMeta); ok {
		lo |= capSniffer
	}
	if _, ok := e.(playingDeadMeta); ok {
		lo |= capPlayingDead
	}
	if _, ok := e.(croakingMeta); ok {
		lo |= capCroaking
	}
	if _, ok := e.(eatingMobMeta); ok {
		lo |= capEatingMob
	}
	if _, ok := e.(restingMeta); ok {
		lo |= capResting
	}
	if _, ok := e.(doorBreaker); ok {
		lo |= capDoorBreaker
	}
	if _, ok := e.(breakingObstruction); ok {
		lo |= capBreakingObstruction
	}
	if _, ok := e.(armsRaised); ok {
		lo |= capArmsRaised
	}
	if _, ok := e.(using); ok {
		lo |= capUsing
	}
	if _, ok := e.(bedSleeper); ok {
		lo |= capBedSleeper
	}
	if _, ok := e.(sleeper); ok {
		lo |= capSleeper
	}
	if _, ok := e.(arrow); ok {
		lo |= capArrow
	}
	if _, ok := e.(gameMode); ok {
		lo |= capGameMode
	}
	if _, ok := e.(orb); ok {
		lo |= capOrb
	}
	if _, ok := e.(firework); ok {
		lo |= capFirework
	}
	if _, ok := e.(owned); ok {
		lo |= capOwned
	}
	if _, ok := e.(leashed); ok {
		lo |= capLeashed
	}
	if _, ok := e.(scaled); ok {
		lo |= capScaled
	}
	if _, ok := e.(tnt); ok {
		lo |= capTNT
	}
	if _, ok := e.(igniteable); ok {
		lo |= capIgniteable
	}
	return lo
}

func computeMetaCapsLo3(e any) (lo uint64) {
	if _, ok := e.(poweredMeta); ok {
		lo |= capPowered
	}
	if _, ok := e.(wallClimbingMeta); ok {
		lo |= capWallClimbing
	}
	if _, ok := e.(burningMeta); ok {
		lo |= capBurning
	}
	if _, ok := e.(chargingMeta); ok {
		lo |= capCharging
	}
	if _, ok := e.(crossbowChargedMeta); ok {
		lo |= capCrossbowCharged
	}
	if _, ok := e.(celebratingMeta); ok {
		lo |= capCelebrating
	}
	if _, ok := e.(celebratingSpecialMeta); ok {
		lo |= capCelebratingSpecial
	}
	if _, ok := e.(admiringMeta); ok {
		lo |= capAdmiring
	}
	if _, ok := e.(castingMeta); ok {
		lo |= capCasting
	}
	if _, ok := e.(beamTargetMeta); ok {
		lo |= capBeamTarget
	}
	if _, ok := e.(shootTargetMeta); ok {
		lo |= capShootTarget
	}
	if _, ok := e.(tongueTargetMeta); ok {
		lo |= capTongueTarget
	}
	if _, ok := e.(ravagerMeta); ok {
		lo |= capRavager
	}
	if _, ok := e.(displayTile); ok {
		lo |= capDisplayTile
	}
	if _, ok := e.(containerMeta); ok {
		lo |= capContainerMeta
	}
	if _, ok := e.(minecartDamage); ok {
		lo |= capMinecartDamage
	}
	if _, ok := e.(rowTime); ok {
		lo |= capRowTime
	}
	if _, ok := e.(buoyantMeta); ok {
		lo |= capBuoyantMeta
	}
	if _, ok := e.(boatFullMeta); ok {
		lo |= capBoatFull
	}
	if _, ok := e.(minecartRideableMeta); ok {
		lo |= capMinecartRideable
	}
	if _, ok := e.(rideTarget); ok {
		lo |= capRideTarget
	}
	if _, ok := e.(witherMeta); ok {
		lo |= capWither
	}
	return lo
}

func computeMetaCapsHi(e any) (hi uint64) {
	if _, ok := e.(endCrystalMeta); ok {
		hi |= capEndCrystal
	}
	if _, ok := e.(alwaysShowNameTag); ok {
		hi |= capAlwaysShowNameTag
	}
	if _, ok := e.(named); ok {
		hi |= capNamed
	}
	if _, ok := e.(scoreTag); ok {
		hi |= capScoreTag
	}
	if _, ok := e.(areaEffectCloud); ok {
		hi |= capAreaEffectCloud
	}
	if _, ok := e.(cloudParticleMeta); ok {
		hi |= capCloudParticle
	}
	if _, ok := e.(living); ok {
		hi |= capLiving
	}
	if _, ok := e.(splash); ok {
		hi |= capSplash
	}
	if _, ok := e.(variable); ok {
		hi |= capVariable
	}
	if _, ok := e.(markVariable); ok {
		hi |= capMarkVariable
	}
	if _, ok := e.(mountStateMeta); ok {
		hi |= capMountState
	}
	if _, ok := e.(hurtDuration); ok {
		hi |= capHurtDuration
	}
	if _, ok := e.(armourStand); ok {
		hi |= capArmourStand
	}
	if _, ok := e.(behaviourHolder); ok {
		hi |= capBehaviour
	}
	if _, ok := e.(riderSeat); ok {
		hi |= capRiderSeat
	}
	if _, ok := e.(foxSleeper); ok {
		hi |= capFoxSleeping
	}
	if _, ok := e.(mobHealth); ok {
		hi |= capMobHealth
	}
	if _, ok := e.(climateVariantMeta); ok {
		hi |= capClimateVariant
	}
	if _, ok := e.(soundVariantMeta); ok {
		hi |= capSoundVariant
	}
	if _, ok := e.(creakingState); ok {
		hi |= capCreakingState
	}
	if hasActorAppearance(e) {
		hi |= capAppearance
	}
	if hasActorPropEntity(e) {
		hi |= capPropEntity
	}
	if _, ok := e.(world.MetadataContributor); ok {
		hi |= capMetadataContributor
	}
	return hi
}

func hasActorPropEntity(e any) bool {
	switch e.(type) {
	case *entity.Bee, *entity.Armadillo, *entity.CopperGolem, *entity.HappyGhast,
		*entity.SulfurCube, *entity.ZombieNautilus:
		return true
	}
	return false
}

func (s *Session) addSpecificMetadataFlags(e any, m mc.Metadata, f actorFlags, caps metaCaps) actorFlags {
	if caps.lo&maskChunk1 != 0 {
		f = addSpecificMetadataChunk1(e, m, f, caps.lo)
	}
	if caps.lo&maskChunk2 != 0 {
		f = addSpecificMetadataChunk2(e, m, f, caps.lo)
	}
	if caps.lo&maskChunk3 != 0 {
		f = s.addSpecificMetadataChunk3(e, m, f, caps.lo)
	}
	if caps.lo&maskChunk4 != 0 {
		f = s.addSpecificMetadataChunk4(e, m, f, caps.lo)
	}
	if caps.hi&maskChunk5 != 0 || caps.lo&capEffectBearer != 0 {
		f = s.addSpecificMetadataChunk5(e, m, f, caps)
	}
	return f
}

func addSpecificMetadataChunk1(e any, m mc.Metadata, f actorFlags, lo uint64) actorFlags {
	if lo&capSheep != 0 {
		sheep := e.(*entity.Sheep)
		m[mc.EntityDataKeyColorIndex] = boxByte(byte(sheep.WoolColor()))
		if sheep.Sheared() {
			f.set(mc.EntityDataFlagSheared)
		}
	}
	if lo&capShulker != 0 {
		shulker := e.(*entity.Shulker)
		m[mc.EntityDataKeyVariant] = boxInt32(int32(shulker.Color()))
		m[mc.EntityDataKeyPeekID] = boxInt32(int32(shulker.Peek()))
		m[mc.EntityDataKeyAttachFace] = boxByte(byte(shulker.AttachFace()))
		m[mc.EntityDataKeyAttached] = boxedInt16s[1]
		pos := cube.PosFromVec3(shulker.Position())
		m[mc.EntityDataKeyAttachedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
	}
	if lo&capParrot != 0 {
		parrot := e.(*entity.Parrot)
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
	if lo&capSneaker != 0 && e.(sneaker).Sneaking() {
		f.set(mc.EntityDataFlagSneaking)
		if lo&capBlocker != 0 {
			if _, blocking := e.(blocker).Blocking(); blocking {
				f.set(mc.EntityDataFlagBlocking)
			}
		}
	}
	if lo&capSprinter != 0 && e.(sprinter).Sprinting() {
		f.set(mc.EntityDataFlagSprinting)
	}
	if lo&capSwimmer != 0 && e.(swimmer).Swimming() {
		f.set(mc.EntityDataFlagSwimming)
	}
	if lo&capCrawler != 0 && e.(crawler).Crawling() {
		f.set(mc.EntityDataFlagCrawling)
	}
	if lo&capGlider != 0 && e.(glider).Gliding() {
		f.set(mc.EntityDataFlagGliding)
	}
	if lo&capBaby != 0 && e.(baby).Baby() {
		f.set(mc.EntityDataFlagBaby)
	}
	if lo&capInLove != 0 && e.(inLoveMeta).InLove() {
		f.set(mc.EntityDataFlagInLove)
	}
	if lo&capBreather != 0 {
		b := e.(breather)
		m[mc.EntityDataKeyAirSupply] = boxInt16(int16(b.AirSupply().Milliseconds() / 50))
		m[mc.EntityDataKeyAirSupplyMax] = boxInt16(int16(b.MaxAirSupply().Milliseconds() / 50))
		if b.Breathing() {
			f.set(mc.EntityDataFlagBreathing)
		}
	}
	if lo&capInvisible != 0 {
		if e.(invisible).Invisible() {
			f.set(mc.EntityDataFlagInvisible)
		}
	} else if lo&capEffectBearer != 0 {
		for _, ef := range e.(effectBearer).Effects() {
			if ef.Type() == effect.Invisibility {
				f.set(mc.EntityDataFlagInvisible)
				break
			}
		}
	}
	if lo&capImmobile != 0 && e.(immobile).Immobile() {
		f.set(mc.EntityDataFlagNoAI)
	}
	if lo&capOnFire != 0 && e.(onFire).OnFireDuration() > 0 {
		f.set(mc.EntityDataFlagOnFire)
	}
	return f
}

func addSpecificMetadataChunk2(e any, m mc.Metadata, f actorFlags, lo uint64) actorFlags {
	if lo&capFreezing != 0 {
		m[mc.EntityDataKeyFreezingEffectStrength] = boxFloat32(e.(freezingMeta).FreezingStrength())
	}
	if lo&capShaking != 0 && e.(shakingMeta).Shaking() {
		f.set(mc.EntityDataFlagShaking)
	}
	if lo&capRamAttacking != 0 && e.(ramAttackingMeta).RamAttacking() {
		f.set(mc.EntityDataFlagRamAttack)
	}
	if lo&capWarden != 0 {
		w := e.(wardenMeta)
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
		m[mc.EntityDataKeyHeartbeatSoundEvent] = metaInt32Heartbeat
	}
	if lo&capShooting != 0 && e.(shootingMeta).Shooting() {
		f.set(mc.EntityDataFlagFacingTargetToRangeAttack)
	}
	if lo&capJumpGoal != 0 && e.(jumpGoalMeta).JumpGoalJumping() {
		f.set(mc.EntityDataFlagJumpGoal)
	}
	if lo&capSniffer != 0 {
		if flag, ok := snifferStateFlags[e.(snifferStateMeta).SnifferState()]; ok {
			f.set(flag)
		}
	}
	if lo&capPlayingDead != 0 && e.(playingDeadMeta).PlayingDead() {
		f.set(mc.EntityDataFlagPlayingDead)
	}
	if lo&capCroaking != 0 && e.(croakingMeta).Croaking() {
		f.set(mc.EntityDataFlagCroaking)
	}
	if lo&capEatingMob != 0 && e.(eatingMobMeta).EatingMob() {
		f.set(mc.EntityDataFlagDigestMob)
	}
	if lo&capResting != 0 && e.(restingMeta).Resting() {
		f.set(mc.EntityDataFlagResting)
	}
	if lo&capDoorBreaker != 0 && e.(doorBreaker).CanBreakDoors() {
		f.set(mc.EntityDataFlagDoorBreaker)
	}
	if lo&capBreakingObstruction != 0 && e.(breakingObstruction).BreakingObstruction() {
		f.set(mc.EntityDataFlagBreakingObstruction)
	}
	if lo&capArmsRaised != 0 && e.(armsRaised).ArmsRaised() {
		f.set(mc.EntityDataFlagCharging)
	}
	if lo&capUsing != 0 && e.(using).UsingItem() {
		f.set(mc.EntityDataFlagUsingItem)
	}
	return f
}

func (s *Session) addSpecificMetadataChunk3(e any, m mc.Metadata, f actorFlags, lo uint64) actorFlags {
	if lo&capBedSleeper != 0 {
		f = writeBedSleeperMetadataFlags(e.(bedSleeper), m, f)
	} else if lo&capSleeper != 0 {
		if pos, ok := e.(sleeper).Sleeping(); ok {
			m[mc.EntityDataKeyBedPosition] = mc.BlockPos{int32(pos[0]), int32(pos[1]), int32(pos[2])}
			m.SetFlag(mc.EntityDataKeyPlayerFlags, 1)
		}
	}
	if lo&capArrow != 0 && e.(arrow).Critical() {
		f.set(mc.EntityDataFlagCritical)
	}
	if lo&capGameMode != 0 && e.(gameMode).GameMode().HasCollision() {
		f.set(mc.EntityDataFlagHasCollision)
	}
	if lo&capOrb != 0 {
		m[mc.EntityDataKeyValue] = boxInt32(int32(e.(orb).Experience()))
	}
	if lo&(capFirework|capOwned) != 0 {
		if lo&capFirework != 0 {
			fw := e.(firework)
			m[mc.EntityDataKeyDisplayFirework] = networkItemNBT(item.NewStack(fw.Firework(), 1))
			if lo&capOwned != 0 && fw.Attached() {
				if owner := e.(owned).Owner(); owner != nil {
					m[mc.EntityDataKeyCustomDisplay] = s.handleUniqueID(owner)
				}
			}
		} else if owner := e.(owned).Owner(); owner != nil {
			m[mc.EntityDataKeyOwner] = s.handleUniqueID(owner)
		}
	}
	if lo&capLeashed != 0 {
		if id := e.(leashed).HolderLeashID(); id != 0 {
			if s.ent != nil && id == s.ent.UniqueID() {
				id = int64(selfEntityRuntimeID)
			}
			m[mc.EntityDataKeyLeashHolder] = id
		}
	}
	if lo&capScaled != 0 {
		m[mc.EntityDataKeyScale] = boxFloat32(float32(e.(scaled).Scale()))
	}
	if lo&capTNT != 0 {
		m[mc.EntityDataKeyFuseTime] = boxInt32(int32(e.(tnt).Fuse().Milliseconds() / 50))
		f.set(mc.EntityDataFlagIgnited)
	}
	if lo&capIgniteable != 0 {
		if i := e.(igniteable); i.Ignited() {
			m[mc.EntityDataKeyFuseTime] = boxInt32(int32(i.FuseTicks()))
			f.set(mc.EntityDataFlagIgnited)
		}
	}
	if lo&capPowered != 0 && e.(poweredMeta).Powered() {
		f.set(mc.EntityDataFlagPowered)
	}
	if lo&capWallClimbing != 0 && e.(wallClimbingMeta).WallClimbing() {
		f.set(mc.EntityDataFlagWallClimbing)
	}
	if lo&capBurning != 0 && e.(burningMeta).Burning() {
		f.set(mc.EntityDataFlagOnFire)
	}
	if lo&capCharging != 0 && e.(chargingMeta).Charging() {
		f.set(mc.EntityDataFlagCharging)
	}
	if lo&capCrossbowCharged != 0 && e.(crossbowChargedMeta).CrossbowCharged() {
		f.set(mc.EntityDataFlagCharged)
	}
	return f
}

func (s *Session) addSpecificMetadataChunk4(e any, m mc.Metadata, f actorFlags, lo uint64) actorFlags {
	if lo&capCelebrating != 0 && e.(celebratingMeta).Celebrating() {
		f.set(mc.EntityDataFlagCelebrating)
	}
	if lo&capCelebratingSpecial != 0 && e.(celebratingSpecialMeta).CelebratingSpecial() {
		f.set(mc.EntityDataFlagCelebratingSpecial)
	}
	if lo&capAdmiring != 0 && e.(admiringMeta).Admiring() {
		f.set(mc.EntityDataFlagAdmiring)
	}
	if lo&capCasting != 0 {
		if c := e.(castingMeta); c.Casting() {
			f.set(mc.EntityDataFlagCasting)
			colour := c.SpellColour()
			m[mc.EntityDataKeyDataSpellCastingColor] = int32(-1<<24 | int32(colour[0]*255)<<16 | int32(colour[1]*255)<<8 | int32(colour[2]*255))
		}
	}
	if lo&capBeamTarget != 0 {
		if h := e.(beamTargetMeta).BeamTargetHandle(); h != nil {
			m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
		}
	}
	if lo&capShootTarget != 0 {
		if h := e.(shootTargetMeta).ShootTargetHandle(); h != nil {
			m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
		} else {
			m[mc.EntityDataKeyTarget] = metaInt64Zero
		}
	}
	if lo&capTongueTarget != 0 {
		if h := e.(tongueTargetMeta).TongueTargetHandle(); h != nil {
			m[mc.EntityDataKeyTarget] = s.handleUniqueID(h)
		} else {
			m[mc.EntityDataKeyTarget] = metaInt64Zero
		}
	}
	if lo&capRavager != 0 {
		r := e.(ravagerMeta)
		if r.Stunned() {
			f.set(mc.EntityDataFlagStunned)
		}
		if r.Roaring() {
			f.set(mc.EntityDataFlagRoaring)
		}
	}
	if lo&(capDisplayTile|capContainerMeta|capMinecartDamage|capRowTime|capBuoyantMeta|capBoatFull|capMinecartRideable|capRideTarget|capWither) != 0 {
		s.addVehicleAndWitherMetadata(e, m, lo)
	}
	return f
}

func (s *Session) addVehicleAndWitherMetadata(e any, m mc.Metadata, lo uint64) {
	if lo&capDisplayTile != 0 {
		d := e.(displayTile)
		var display int32
		if b, ok := d.DisplayBlock(); ok {
			display = int32(s.blockHash(b))
		}
		m[mc.EntityDataKeyDisplayBlockState] = boxInt32(display)
		m[mc.EntityDataKeyDisplayOffset] = boxInt32(int32(d.DisplayOffset()))
		m[mc.EntityDataKeyCustomDisplay] = boxBoolByte(d.CustomDisplay())
	}
	if lo&capContainerMeta != 0 {
		c := e.(containerMeta)
		if typ, ok := actorContainerType(c.ContainerKind()); ok {
			m[mc.EntityDataKeyContainerType] = boxByte(typ)
			m[mc.EntityDataKeyContainerSize] = boxInt32(c.ContainerSize())
			m[mc.EntityDataKeyContainerStrengthModifier] = boxInt32(c.ContainerStrengthModifier())
		}
	}
	if lo&capMinecartDamage != 0 {
		h := e.(minecartDamage)
		m[mc.EntityDataKeyHurt] = boxInt32(int32(h.RollingAmplitude()))
		m[mc.EntityDataKeyHurtDirection] = boxInt32(int32(h.RollingDirection()))
		m[mc.EntityDataKeyStructuralIntegrity] = boxInt32(int32(h.Damage()))
	}
	if lo&capRowTime != 0 {
		r := e.(rowTime)
		m[mc.EntityDataKeyRowTimeLeft] = boxFloat32(r.RowTimeLeft())
		m[mc.EntityDataKeyRowTimeRight] = boxFloat32(r.RowTimeRight())
	}
	if lo&capBuoyantMeta != 0 {
		buoyant := e.(buoyantMeta).Buoyant()
		m[mc.EntityDataKeyIsBuoyant] = boxBoolByte(buoyant)
		if buoyant {
			m[mc.EntityDataKeyBuoyancyData] = metaBuoyancy
			m[mc.EntityDataKeyControllingSeatIndex] = boxedBytes[0]
		}
	}
	switch {
	case lo&capContainerMeta != 0:
		m[mc.EntityDataKeyInteractText] = metaInteractContainer
	case lo&capBoatFull != 0:
		if !e.(boatFullMeta).Full() {
			m[mc.EntityDataKeyInteractText] = metaInteractBoat
		}
	case lo&capMinecartRideable != 0:
		v := e.(minecartRideableMeta)
		if v.Rideable() && v.Passenger() == nil {
			m[mc.EntityDataKeyInteractText] = metaInteractMinecart
		}
	}
	if lo&capRideTarget != 0 {
		m[mc.EntityDataKeyCanRideTarget] = boxBoolByte(e.(rideTarget).CanRideTarget())
	}
	if lo&capWither != 0 {
		w := e.(witherMeta)
		for i, h := range w.HeadTargets() {
			var id int64
			if h != nil {
				id = int64(s.handleRuntimeID(h))
			}
			m[witherTargetKeys[i]] = boxInt64(id)
		}
		m[mc.EntityDataKeyInvulnerableTicks] = boxInt32(int32(w.InvulnerableTicks()))
	}
}

func (s *Session) addSpecificMetadataChunk5(e any, m mc.Metadata, f actorFlags, caps metaCaps) actorFlags {
	hi := caps.hi
	if hi&capEndCrystal != 0 {
		ec := e.(endCrystalMeta)
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
	if hi&capNamed != 0 {
		alwaysShow := true
		if hi&capAlwaysShowNameTag != 0 {
			alwaysShow = e.(alwaysShowNameTag).AlwaysShowNameTag()
		}
		f = writeNameTagMetadataFlags(m, f, e.(named).NameTag(), alwaysShow)
	}
	if hi&capScoreTag != 0 {
		m[mc.EntityDataKeyScore] = boxString(e.(scoreTag).ScoreTag())
	}
	if hi&capAreaEffectCloud != 0 {
		c := e.(areaEffectCloud)
		m[mc.EntityDataKeyDataRadius] = boxFloat32(float32(c.Radius()))
		m[mc.EntityDataKeyDataDuration] = metaInt32Max
		m[mc.EntityDataKeyDataChangeOnPickup] = metaFloat32Min
		m[mc.EntityDataKeyDataChangeRate] = metaFloat32Min
		if hi&capCloudParticle != 0 {
			if particle, ok := cloudParticleType(e.(cloudParticleMeta).Particle()); ok {
				m[mc.EntityDataKeyDataParticle] = boxInt32(particle)
			}
		}
		colour, am := effect.ResultingColour(c.Effects())
		m[mc.EntityDataKeyEffectColor] = boxInt32(int32FromRGBA(colour))
		m[mc.EntityDataKeyEffectAmbience] = boxBoolByte(am)
	}
	if hi&capLiving != 0 && s.ent != nil {
		l := e.(living)
		if s.ent.UUID() == l.UUID() {
			deathPos, deathDimension, died := l.DeathPosition()
			if died {
				dim, _ := world.DimensionID(deathDimension)
				m[mc.EntityDataKeyPlayerLastDeathPosition] = vec64To32(deathPos)
				m[mc.EntityDataKeyPlayerLastDeathDimension] = boxInt32(int32(dim))
			}
			m[mc.EntityDataKeyPlayerHasDied] = boxBoolByte(died)
		}
	}
	if hi&capSplash != 0 {
		tip := e.(splash).Potion().Uint8()
		m[mc.EntityDataKeyAuxValueData] = boxInt16(int16(tip))
		if tip > 4 {
			m[mc.EntityDataKeyCustomDisplay] = boxByte(tip + 1)
		}
	}
	if caps.lo&capEffectBearer != 0 {
		m[mc.EntityDataKeyVisibleMobEffects] = boxInt64(packVisibleEffects(e.(effectBearer).Effects()))
	}
	if hi&capVariable != 0 {
		m[mc.EntityDataKeyVariant] = boxInt32(e.(variable).Variant())
	}
	if hi&capMarkVariable != 0 {
		m[mc.EntityDataKeyMarkVariant] = boxInt32(e.(markVariable).MarkVariant())
	}
	if hi&capMountState != 0 {
		f = addMountMetadataFlags(e.(mountStateMeta).MountState(), m, f)
	}
	if hi&capHurtDuration != 0 {
		m[mc.EntityDataKeyHurt] = boxInt32(int32(e.(hurtDuration).HurtDuration().Milliseconds() / 50))
	}
	if hi&capArmourStand != 0 {
		m[mc.EntityDataKeyPoseIndex] = boxInt32(int32(e.(armourStand).PoseIndex()))
	}
	return f
}
