package session

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// TestMetadataWritersMatch holds addSpecificMetadata, which runs the writers
// that apply to the type of what it is given, to the function it replaced,
// addSpecificMetadataReference: the same metadata, or the same panic, for an
// entity of every type of the default registry and for its behaviour, as
// spawned and in the states a test can put it in, and for a player, seen by
// another and by itself. Each is written twice: the first time fills the
// cache of the writers of its type, and the second takes them from it.
func TestMetadataWritersMatch(t *testing.T) {
	t.Parallel()
	w := metadataWorld(t)
	viewer := testSession("metadata-writers")
	viewer.br = world.DefaultBlockRegistry
	pos := mgl64.Vec3{0.5, 120, 0.5}
	conf := player.Config{Name: "Steve", UUID: uuid.New(), Position: pos}
	ph := world.EntitySpawnOpts{Position: pos, ID: conf.UUID}.New(player.Type, conf)
	self := testSessionOf("Steve", ph, conf.UUID.String())
	self.br = world.DefaultBlockRegistry

	compare := func(s *Session, name string, v any) {
		for pass := range 2 {
			want, got := mc.NewMetadata(), mc.NewMetadata()
			wantPanic := catchPanic(func() { s.addSpecificMetadataReference(v, want) })
			gotPanic := catchPanic(func() { s.addSpecificMetadata(v, got) })
			if wantPanic != gotPanic {
				t.Errorf("%s (%T), pass %d: panics with %q, want %q", name, v, pass, gotPanic, wantPanic)
			} else if !reflect.DeepEqual(got, want) {
				t.Errorf("%s (%T), pass %d:\n got %v\nwant %v", name, v, pass, got, want)
			}
		}
	}
	both := func(s *Session, name string, e world.Entity) {
		compare(s, name, e)
		if b, ok := e.(interface{ Behaviour() entity.Behaviour }); ok {
			var beh entity.Behaviour
			if p := catchPanic(func() { beh = b.Behaviour() }); p != "" {
				t.Logf("%s: its behaviour panics: %s", name, p)
				return
			}
			compare(s, name+" behaviour", beh)
		}
	}
	<-w.Exec(func(tx *world.Tx) {
		ents, err := registryEntities(tx)
		if err != nil {
			t.Error(err)
			return
		}
		p := tx.AddEntity(ph).(*player.Player)
		for _, state := range metadataStates {
			for _, e := range ents {
				state.set(tx, e)
				both(viewer, state.name+" "+e.H().Type().EncodeEntity(), e)
			}
			state.set(tx, p)
			both(viewer, state.name+" player", p)
			both(self, state.name+" player of the session", p)
		}
	})
}

// metadataStates are the states TestMetadataWritersMatch puts entities in,
// each on top of those before it.
var metadataStates = []struct {
	name string
	set  func(tx *world.Tx, e world.Entity)
}{
	{"spawned", func(*world.Tx, world.Entity) {}},
	{"named", func(_ *world.Tx, e world.Entity) {
		if n, ok := e.(interface{ SetNameTag(string) }); ok {
			n.SetNameTag("name")
		}
		if p, ok := e.(*player.Player); ok {
			p.SetScoreTag("score")
		}
	}},
	// One effect: the visible effects are packed in the order of Effects,
	// which is at random.
	{"burning and invisible", func(_ *world.Tx, e world.Entity) {
		if f, ok := e.(interface{ SetOnFire(time.Duration) }); ok {
			f.SetOnFire(time.Minute)
		}
		if l, ok := e.(interface{ AddEffect(effect.Effect) }); ok {
			l.AddEffect(effect.New(effect.Invisibility, 1, time.Minute))
		}
	}},
	{"moving", func(_ *world.Tx, e world.Entity) {
		if p, ok := e.(*player.Player); ok {
			p.StartSneaking()
			p.StartSprinting()
			p.StartSwimming()
			p.StartGliding()
			p.SetInvisible()
			p.Sleep(cube.PosFromVec3(p.Position()))
		}
	}},
}

// catchPanic runs f and returns what it panicked with, or "" if it did not.
func catchPanic(f func()) (panicked string) {
	defer func() {
		if r := recover(); r != nil {
			panicked = fmt.Sprint(r)
		}
	}()
	f()
	return ""
}

// addSpecificMetadataReference is addSpecificMetadata as it was before it
// ran writers by the type of what it is given, which
// TestMetadataWritersMatch holds it to.
func (s *Session) addSpecificMetadataReference(e any, m mc.Metadata) {
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
	if text, _ := interactText(e); text != "" {
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
