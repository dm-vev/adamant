package entity

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// CreakingType is the Java Creaking entity type.
var CreakingType creakingType

type creakingType struct{}

func (creakingType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		creakingConfig{}.Apply(data)
	}
	state := data.Data.(*creakingState)
	if state.bound && !state.homeDimensionSet {
		if id, ok := world.DimensionID(tx.World().Dimension()); ok {
			state.homeDimension, state.homeDimensionSet = int32(id), true
		}
	}
	return &Creaking{Ent: &Ent{tx: tx, handle: handle, data: data}}
}
func (creakingType) EncodeEntity() string { return "minecraft:creaking" }
func (creakingType) BBox(world.Entity) cube.BBox {
	halfWidth, height := float64(float32(.9))/2, float64(float32(2.7))
	return cube.Box(-halfWidth, 0, -halfWidth, halfWidth, height, halfWidth)
}
func (creakingType) DecodeNBT(n map[string]any, data *world.EntityData) {
	conf := creakingConfig{}
	for _, attr := range ReadNativeMobAttributes(n) {
		switch attr.Name {
		case "minecraft:health":
			conf.health, conf.healthSet = attr.Current, true
		case "minecraft:movement":
			conf.speed = attr.Current
		}
	}
	if !conf.healthSet {
		if _, ok := n["Health"]; ok {
			conf.health, conf.healthSet = float64(nbtconv.Float32(n, "Health")), true
		}
	}
	if home, ok := n["home_pos"].([]int32); ok && len(home) == 3 {
		conf.home = cube.Pos{int(home[0]), int(home[1]), int(home[2])}
		conf.bound = true
	}
	if home, ok := n["home_pos"].([]any); ok && len(home) == 3 {
		coords := make([]int, 3)
		valid := true
		for i, v := range home {
			switch n := v.(type) {
			case int32:
				coords[i] = int(n)
			case int64:
				coords[i] = int(n)
			default:
				valid = false
			}
		}
		if valid {
			conf.home, conf.bound = cube.Pos{coords[0], coords[1], coords[2]}, true
		}
	}
	homePos, ok := n["HomePos"].([]float32)
	if !ok {
		if list, listOK := n["HomePos"].([]any); listOK && len(list) == 3 {
			coords := make([]float32, 3)
			valid := true
			for i, value := range list {
				coords[i], valid = value.(float32)
				if !valid {
					break
				}
			}
			if valid {
				homePos, ok = coords, true
			}
		}
	}
	if ok && len(homePos) == 3 {
		conf.home = cube.PosFromVec3(mgl64.Vec3{float64(homePos[0]), float64(homePos[1]), float64(homePos[2])})
		conf.bound = true
	}
	if dim, ok := n["HomeDimensionId"].(int32); ok {
		conf.homeDimension, conf.homeDimensionSet = dim, true
	}
	if java := ReadNativeMobJavaState(n); java != nil {
		if home, ok := java["home_pos"].([]int32); ok && len(home) == 3 {
			conf.home = cube.Pos{int(home[0]), int(home[1]), int(home[2])}
			conf.bound = true
		}
	}
	// Java restores home/health, not gaze, sway or teardown animation state.
	// Bedrock properties are emitted for clients but do not change reload rules.
	if conf.bound {
		data.FireDuration = 0
	}
	state := conf.New()
	java := ReadNativeMobJavaState(n)
	if java == nil {
		java = n
	}
	if words, ok := java["last_hurt_by_player"].([]int32); ok && len(words) == 4 {
		for i, word := range words {
			binary.BigEndian.PutUint32(state.lastHurtByPlayer[i*4:], uint32(word))
		}
	}
	state.lastHurtByPlayerMemoryTime = int(nbtconv.Int32(java, "last_hurt_by_player_memory_time"))
	state.persistenceRequired = nbtconv.Uint8(java, "PersistenceRequired") != 0
	data.Data = state
}
func (creakingType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*creakingState)
	native := CloneNativeMobNBT(data.NativeNBT)
	if native == nil {
		native = make(map[string]any)
	}
	native["Attributes"] = EncodeNativeMobAttributes(ReadNativeMobAttributes(data.NativeNBT), []NativeMobAttribute{
		{Name: "minecraft:health", Base: 1, Current: b.health.Health(), DefaultMin: 0, DefaultMax: 1, Min: 0, Max: 1},
		{Name: "minecraft:movement", Base: .4, Current: b.speed, DefaultMin: 0, DefaultMax: math.MaxFloat32, Min: 0, Max: math.MaxFloat32},
	})
	if b.bound {
		native["HomePos"] = []any{float32(b.home.X()), float32(b.home.Y()), float32(b.home.Z())}
		if b.homeDimensionSet {
			native["HomeDimensionId"] = b.homeDimension
		}
	} else {
		delete(native, "HomePos")
		delete(native, "HomeDimensionId")
	}
	state, sway := b.nativeState()
	switch {
	case b.active && !b.canMove:
		native["definitions"] = []any{"+minecraft:creaking", b.spawnDefinition(), "-minecraft:neutral", "-minecraft:hostile_unobserved", "-minecraft:mobile", "+minecraft:hostile", "+minecraft:immobile"}
	case b.active && b.canMove:
		native["definitions"] = []any{"+minecraft:creaking", b.spawnDefinition()}
	default:
		native["definitions"] = []any{"+minecraft:creaking", b.spawnDefinition(), "-minecraft:hostile_unobserved", "-minecraft:immobile", "+minecraft:neutral", "+minecraft:mobile"}
	}
	native["properties"] = map[string]any{
		"minecraft:creaking_state":         creakingNativeStateName(state),
		"minecraft:creaking_swaying_ticks": int32(sway),
	}
	// A zero reference explicitly clears an expired memory in retained native NBT.
	words := make([]int32, 4)
	for i := range words {
		words[i] = int32(binary.BigEndian.Uint32(b.lastHurtByPlayer[i*4:]))
	}
	javaState := map[string]any{"last_hurt_by_player": words, "last_hurt_by_player_memory_time": int32(b.lastHurtByPlayerMemoryTime)}
	javaState["PersistenceRequired"] = uint8(0)
	if b.persistenceRequired {
		javaState["PersistenceRequired"] = uint8(1)
	}
	if b.bound {
		javaState["home_pos"] = []int32{int32(b.home.X()), int32(b.home.Y()), int32(b.home.Z())}
	}
	return MergeNativeMobJavaState(native, javaState)
}

type creakingConfig struct {
	health, speed    float64
	healthSet        bool
	home             cube.Pos
	homeDimension    int32
	homeDimensionSet bool
	bound            bool
}

func (c creakingConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c creakingConfig) New() *creakingState {
	health := c.health
	if !c.healthSet {
		health = 1
	}
	speed := c.speed
	if speed <= 0 {
		speed = float64(float32(.4))
	}
	s := &creakingState{health: NewHealthManager(health, 1), effects: NewEffectManager(), speed: speed, home: c.home, homeDimension: c.homeDimension, homeDimensionSet: c.homeDimensionSet, bound: c.bound, canMove: true}
	s.br = newCreakingBrain(s)
	return s
}

type creakingState struct {
	health           *HealthManager
	effects          *EffectManager
	ai               MobAI
	br               *Brain
	speed            float64
	home             cube.Pos
	bound, active    bool
	tearingDown      bool
	dead             bool
	homeDimension    int32
	homeDimensionSet bool
	// attackTicks and swayTicks are Creaking.attackAnimationRemainingTicks
	// and invulnerabilityAnimationRemainingTicks.
	attackTicks, swayTicks     int
	deathTicks, stuckCounter   int
	canMove                    bool
	lastHurtByPlayer           uuid.UUID
	lastHurtByPlayerMemoryTime int
	persistenceRequired        bool
}
type Creaking struct{ *Ent }

func NewCreaking(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(CreakingType, creakingConfig{})
}
func (*Creaking) SpeciesType() world.EntityType { return CreakingType }
func (c *Creaking) state() *creakingState       { return c.Behaviour().(*creakingState) }
func (c *Creaking) Tick(tx *world.Tx, current int64) {
	c.state().effects.Tick(c, tx)
	c.Ent.Tick(tx, current)
}

func (s *creakingState) mobAI() *MobAI { return &s.ai }
func (s *creakingState) brain() *Brain { return s.br }

// Tick ports Creaking.tick and aiStep: a creaking whose heart no longer
// protects it dies, and one a player looks at freezes.
func (s *creakingState) Tick(e *Ent, tx *world.Tx) *Movement {
	c := &Creaking{Ent: e}
	if s.lastHurtByPlayerMemoryTime > 0 {
		s.lastHurtByPlayerMemoryTime--
	} else {
		s.lastHurtByPlayer = uuid.Nil
	}
	if s.bound && !s.tearingDown {
		dimensionMatches := true
		if s.homeDimensionSet {
			id, ok := world.DimensionID(tx.World().Dimension())
			dimensionMatches = ok && int32(id) == s.homeDimension
		}
		if !dimensionMatches || !(block.CreakingHeart{}).IsCreakingProtector(s.home, tx, e.H()) {
			s.health.AddHealth(-s.health.Health())
		}
	}
	if s.health.Health() <= 0 && !s.tearingDown {
		// LivingEntity.tickDeath.
		if s.deathTicks == 0 {
			c.viewAction(DeathAction{})
		}
		s.deathTicks++
		if s.deathTicks >= 20 {
			s.dead = true
			_ = e.CloseIn(tx)
			return nil
		}
	} else if s.tearingDown {
		// Creaking.tickDeath.
		s.deathTicks++
		if s.deathTicks > 45 {
			s.tearDown(e, tx)
			return nil
		}
	}
	// Creaking.createAttributes.
	s.ai.MovementSpeed, s.ai.FollowRange, s.ai.StepHeight, s.ai.EyeHeight = s.speed, 32, 1.0625, float64(float32(2.3))
	if s.swayTicks > 0 {
		if s.swayTicks--; s.swayTicks == 0 {
			e.updateState()
		}
	}
	if s.attackTicks > 0 {
		s.attackTicks--
	}
	if s.ai.started && s.health.Health() > 0 {
		s.updateCanMove(s.ai.Mob(e, c))
	}
	return s.ai.Tick(e, c, func(m *Mob) {
		m.AI.nav.SetCanFloat(true)
		m.AI.nav.SetCanPassDoors(true)
		// Creaking.CreakingPathNavigation pauses while the creaking is
		// frozen, and its HomeNodeEvaluator keeps it within 32 blocks of
		// its heart.
		m.AI.nav.Paused = func() bool { return !s.canMove }
		m.AI.nav.SetBlocked(func(m *Mob, pos cube.Pos) bool {
			if !s.bound {
				return false
			}
			d := distSqrPos(s.home, pos)
			return d > 1024 && d >= distSqrPos(s.home, m.BlockPos())
		})
		m.AI.moveOverride, m.AI.lookOverride = creakingMoveControl{s}, creakingLookControl{s}
		if s.bound {
			// Creaking.setTransient.
			m.AI.SetPathfindingMalus(PathDamaging, 8)
			m.AI.SetPathfindingMalus(PathPowderSnow, 8)
			m.AI.SetPathfindingMalus(PathLava, 8)
			m.AI.SetPathfindingMalus(PathFire, 0)
			m.AI.SetPathfindingMalus(PathFireInNeighbour, 0)
		}
	})
}

// updateCanMove ports the part of Creaking.aiStep that freezes a creaking a
// player looks at and lets it move again once nobody does.
func (s *creakingState) updateCanMove(m *Mob) {
	now := s.checkCanMove(m)
	if now == s.canMove {
		return
	}
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	if now {
		playCreakingSound(m.Ent, m.tx, "unfreeze")
	} else {
		m.StopInPlace()
		playCreakingSound(m.Ent, m.tx, "freeze")
	}
	s.canMove = now
	m.updateState()
}

// checkCanMove ports Creaking.checkCanMove: a creaking stays still while a
// player it may attack looks at it, and wakes up when one looks at it within
// twelve blocks.
func (s *creakingState) checkCanMove(m *Mob) bool {
	players := creakingNearestPlayers(m)
	if len(players) == 0 {
		if s.active {
			s.deactivate(m)
		}
		return true
	}
	potential := false
	for _, p := range players {
		if !m.CanAttack(p) {
			continue
		}
		potential = true
		if (!s.active || !creakingDisguise(p)) && creakingLookingAt(m.Ent, p, m.tx) {
			if s.active {
				return false
			}
			if p.Position().Sub(m.data.Pos).LenSqr() < 144 {
				s.activate(m, p)
				return false
			}
		}
	}
	if !potential && s.active {
		s.deactivate(m)
	}
	return true
}

// creakingNearestPlayers returns the players of the NEAREST_PLAYERS memory.
func creakingNearestPlayers(m *Mob) []world.Entity {
	v, _ := m.Brain().get(memNearestPlayers)
	handles, _ := v.([]*world.EntityHandle)
	players := make([]world.Entity, 0, len(handles))
	for _, h := range handles {
		if p, ok := h.Entity(m.tx); ok {
			players = append(players, p)
		}
	}
	return players
}

// activate ports Creaking.activate: the creaking attacks the player that
// looked at it.
func (s *creakingState) activate(m *Mob, p world.Entity) {
	m.Brain().setEntity(memAttackTarget, p)
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	playCreakingSoundAt(m.Ent, m.tx, "mob.warning", m.data.Pos.Add(mgl64.Vec3{0, 2.295, 0}))
	playCreakingSoundAt(m.Ent, m.tx, "stare", m.data.Pos.Add(mgl64.Vec3{0, 2.295, 0}))
	playCreakingSound(m.Ent, m.tx, "activate")
	s.setActive(m, true)
}

// deactivate ports Creaking.deactivate.
func (s *creakingState) deactivate(m *Mob) {
	m.Brain().erase(memAttackTarget)
	m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	playCreakingSound(m.Ent, m.tx, "deactivate")
	s.setActive(m, false)
}

func (s *creakingState) setActive(m *Mob, active bool) {
	if s.active != active {
		s.active = active
		m.updateState()
	}
}

// customServerAiStep ports Creaking.customServerAiStep and
// CreakingAi.updateActivity: a frozen creaking idles.
func (s *creakingState) customServerAiStep(m *Mob) {
	s.br.tick(m)
	if !s.canMove {
		s.br.useDefaultActivity()
		return
	}
	s.br.setActiveToFirstValid(activityFight, activityIdle)
}

// doHurtTarget ports Creaking.doHurtTarget.
func (s *creakingState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	s.attackTicks = 15
	playCreakingSound(m.Ent, m.tx, "attack")
	_, hurt := l.Hurt(3, AttackDamageSource{Attacker: m.Self})
	return hurt
}

// walkTargetValue ports Creaking.getWalkTargetValue: a creaking cares not
// where it walks.
func (s *creakingState) walkTargetValue(*Mob, cube.Pos) float64 { return 0 }

// tickJumpControl ports Creaking.CreakingJumpControl: a frozen creaking does
// not jump.
func (s *creakingState) tickJumpControl(m *Mob) {
	if s.canMove {
		m.AI.jump.tick(m)
		return
	}
	m.AI.jumping = false
}

// creakingMoveControl ports Creaking.CreakingMoveControl.
type creakingMoveControl struct{ s *creakingState }

func (c creakingMoveControl) tick(m *Mob) {
	if c.s.canMove {
		m.AI.move.tick(m)
	}
}

// creakingLookControl ports Creaking.CreakingLookControl.
type creakingLookControl struct{ s *creakingState }

func (c creakingLookControl) tick(m *Mob) {
	if c.s.canMove {
		m.AI.look.tick(m)
	}
}

// newCreakingBrain ports CreakingAi and the brain provider of Creaking.
func newCreakingBrain(s *creakingState) *Brain {
	br := newBrain(nearestLivingEntitySensor{}, playerSensor{})
	swim := newSwim(.8)
	swimExtra := swim.checkExtra
	swim.checkExtra = func(m *Mob) bool { return s.canMove && (swimExtra == nil || swimExtra(m)) }
	br.addActivity(activityCore, 0, swim, newLookAtTargetSink(45, 90), newMoveToTargetSink(150, 250, nil))
	br.addActivity(activityIdle, 10,
		newStartAttacking(func(*Mob) bool { return s.active }, func(m *Mob) world.Entity {
			return m.Brain().entity(m, memNearestVisibleAttackablePlayer)
		}),
		newSetEntityLookTargetSometimes(nil, 8, uniformInt{30, 60}),
		newRunOne(nil,
			weightedBehavior{newRandomStroll(float64(float32(.3)), true), 2},
			weightedBehavior{newSetWalkTargetFromLookTarget(nil, constSpeed(float64(float32(.3))), 3), 2},
			weightedBehavior{newDoNothing(30, 60), 1},
		),
	)
	br.addActivityWith(activityFight, []prioritizedBehavior{
		{10, newSetWalkTargetFromAttackTargetIfTargetOutOfReach(constSpeed(1))},
		{10, newTriggerIf(func(*Mob) bool { return s.canMove }, newMeleeAttack(40))},
		{10, newStopAttackingIfTargetInvalid(func(m *Mob, t world.Entity) bool { return !creakingTargetReachable(m, t) }, nil, false)},
	}, []memoryCondition{{memAttackTarget, memPresent}}, nil)
	return br
}

// creakingTargetReachable ports CreakingAi.isAttackTargetStillReachable: the
// target is one of the players the creaking sees and may attack.
func creakingTargetReachable(m *Mob, t world.Entity) bool {
	v, ok := m.Brain().get(memNearestVisibleAttackablePlayers)
	if !ok || !isPlayerEntity(t) {
		return false
	}
	for _, h := range v.([]*world.EntityHandle) {
		if h == t.H() {
			return true
		}
	}
	return false
}

func creakingDisguise(p world.Entity) bool {
	if disguise, ok := p.(interface{ WearingCreakingDisguise() bool }); ok {
		return disguise.WearingCreakingDisguise()
	}
	return false
}

// creakingLookingAt ports LivingEntity.isLookingAtMe for Creaking.checkCanMove:
// the player looks at the eyes, the feet or the middle of the creaking and
// sees it.
func creakingLookingAt(e *Ent, p world.Entity, tx *world.Tx) bool {
	view, ok := p.(interface {
		Rotation() cube.Rotation
		EyeHeight() float64
	})
	if !ok {
		return false
	}
	direction := view.Rotation().Vec3()
	playerPos := p.Position()
	eyeY := playerPos[1] + view.EyeHeight()
	origin := mgl64.Vec3{playerPos[0], eyeY, playerPos[2]}
	creakingY := e.Position()[1]
	creakingEyeY := creakingY + float64(float32(2.3))
	for _, y := range []float64{creakingEyeY, creakingY + .5, (creakingEyeY + creakingY) * .5} {
		target := mgl64.Vec3{e.Position()[0], y, e.Position()[2]}
		to := target.Sub(origin)
		if to.Len() == 0 || direction.Dot(to.Normalize()) <= .5 {
			continue
		}
		if dragonLineOfSight(tx, origin, target) {
			return true
		}
	}
	return false
}

// stuckInPlayer ports Creaking.playerIsStuckInYou: the eyes of a player
// stayed inside the creaking for more than four checks.
func (s *creakingState) stuckInPlayer(e *Ent, tx *world.Tx) bool {
	v, _ := s.br.get(memNearestPlayers)
	handles, _ := v.([]*world.EntityHandle)
	if len(handles) == 0 {
		s.stuckCounter = 0
		return false
	}
	box := CreakingType.BBox(e).Translate(e.Position())
	for _, handle := range handles {
		player, ok := handle.Entity(tx)
		if ok && box.Vec3Within(player.Position().Add(mgl64.Vec3{0, playerEyeHeight(player), 0})) {
			s.stuckCounter++
			return s.stuckCounter > 4
		}
	}
	s.stuckCounter = 0
	return false
}
func playerEyeHeight(p world.Entity) float64 {
	if eye, ok := p.(interface{ EyeHeight() float64 }); ok {
		return eye.EyeHeight()
	}
	return 1.62
}

func (c *Creaking) PlayerIsStuck() bool {
	return c.state().stuckInPlayer(c.Ent, c.tx)
}
func (c *Creaking) CreakingState() (int32, int32) {
	state, _ := c.state().nativeState()
	return state, int32(min(6, c.state().swayTicks))
}
func (s *creakingState) nativeState() (int32, int) {
	switch {
	case s.tearingDown:
		return 4, s.swayTicks
	case s.swayTicks > 0:
		return 3, s.swayTicks
	case s.active && s.canMove:
		return 2, s.swayTicks
	case s.active:
		return 1, s.swayTicks
	default:
		return 0, s.swayTicks
	}
}
func creakingNativeStateName(state int32) string {
	switch state {
	case 1:
		return "hostile_observed"
	case 2:
		return "hostile_unobserved"
	case 3:
		return "twitching"
	case 4:
		return "crumbling"
	default:
		return "neutral"
	}
}
func (s *creakingState) spawnDefinition() string {
	if s.bound {
		return "+minecraft:spawned_by_creaking_heart"
	}
	return "+minecraft:spawned_by_player"
}
func (c *Creaking) RemoveProtector(source world.DamageSource) {
	s := c.state()
	if s.dead || s.tearingDown {
		return
	}
	if source == nil {
		s.dieNow(c.Ent, c.tx)
		return
	}
	s.tearingDown = true
	s.health.AddHealth(-s.health.Health())
	s.deathTicks = 0
	c.viewAction(DeathAction{})
	c.Ent.updateState()
}
func (c *Creaking) RemoveProtectorByExplosion(source world.ExplosionSource) {
	c.RemoveProtector(ExplosionDamageSource{Source: source})
}
func (s *creakingState) tearDown(e *Ent, tx *world.Tx) {
	box := CreakingType.BBox(e)
	center := box.Min().Add(box.Max()).Mul(.5).Add(e.Position())
	xSpread := float64(float32(box.Width() * .3))
	ySpread := float64(float32(box.Height() * .3))
	zSpread := float64(float32(box.Length() * .3))
	// ServerLevel.sendParticles sends a spread/count packet without advancing
	// Level.random. Bedrock presentation sampling must not consume gameplay RNG.
	for range 100 {
		position := center.Add(mgl64.Vec3{
			rand.NormFloat64() * xSpread,
			rand.NormFloat64() * ySpread,
			rand.NormFloat64() * zSpread,
		})
		tx.AddParticle(position, particle.BlockBreak{Block: block.Wood{Wood: block.PaleOakWood(), Axis: cube.Y}})
	}
	for range 10 {
		position := center.Add(mgl64.Vec3{
			rand.NormFloat64() * xSpread,
			rand.NormFloat64() * ySpread,
			rand.NormFloat64() * zSpread,
		})
		tx.AddParticle(position, particle.BlockBreak{Block: block.CreakingHeart{State: block.CreakingHeartAwake}})
	}
	s.dead = true
	_ = e.CloseIn(tx)
}
func (s *creakingState) dieNow(e *Ent, tx *world.Tx) {
	s.dead = true
	s.tearDown(e, tx)
}

func (c *Creaking) Health() float64        { return c.state().health.Health() }
func (c *Creaking) MaxHealth() float64     { return c.state().health.MaxHealth() }
func (c *Creaking) SetMaxHealth(v float64) { c.state().health.SetMaxHealth(v) }
func (c *Creaking) Dead() bool             { return c.state().dead || c.Health() <= 0 }
func (c *Creaking) Hurt(d float64, src world.DamageSource) (float64, bool) {
	s := c.state()
	if c.Dead() {
		return 0, false
	}
	bypass := false
	if _, ok := src.(VoidDamageSource); ok {
		bypass = true
	}
	if source, ok := src.(interface{ BypassesInvulnerability() bool }); ok && source.BypassesInvulnerability() {
		bypass = true
	}
	if s.bound && !bypass {
		if src.Fire() || s.swayTicks > 0 {
			return 0, false
		}
		_, responsible := s.responsiblePlayer(src, c.tx)
		if !creakingDamageHasDirectAttacker(src) && !responsible {
			return 0, false
		}
		s.swayTicks = 8
		c.tx.EmitGameEvent(world.GameEventEntityAction, c.Position(), world.GameEventContext{Source: c})
		if responsible && (block.CreakingHeart{}).IsCreakingProtector(s.home, c.tx, c.H()) {
			(block.CreakingHeart{}).CreakingHurt(s.home, c.tx)
		}
		c.Ent.updateState()
		return 0, true
	}
	if d <= 0 {
		return 0, false
	}
	if src.Fire() {
		if _, ok := s.effects.Effect(effect.FireResistance); ok {
			return 0, false
		}
	}
	if resistance, ok := s.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	s.health.AddHealth(-d)
	return d, true
}
func creakingDamageHasDirectAttacker(src world.DamageSource) bool {
	switch source := src.(type) {
	case AttackDamageSource:
		_, ok := source.Attacker.(Living)
		return ok && source.Attacker != nil
	case SpearDamageSource:
		_, ok := source.Attacker.(Living)
		return ok && source.Attacker != nil
	case MaceSmashDamageSource:
		_, ok := source.Attacker.(Living)
		return ok && source.Attacker != nil
	case ProjectileDamageSource:
		return source.Projectile != nil
	case ExplosionDamageSource:
		var direct world.Entity
		switch explosion := source.Source.(type) {
		case world.EntityExplosionSource:
			direct = explosion.Entity
		case *world.EntityExplosionSource:
			if explosion != nil {
				direct = explosion.Entity
			}
		}
		if _, living := direct.(Living); living {
			return true
		}
		if projectile, ok := direct.(*Ent); ok {
			_, ok = projectile.Behaviour().(*ProjectileBehaviour)
			return ok
		}
		return false
	}
	return false
}
func (s *creakingState) responsiblePlayer(src world.DamageSource, tx *world.Tx) (world.Entity, bool) {
	var source world.Entity
	switch damage := src.(type) {
	case AttackDamageSource:
		source = damage.Attacker
	case SpearDamageSource:
		source = damage.Attacker
	case MaceSmashDamageSource:
		source = damage.Attacker
	case ProjectileDamageSource:
		source = damage.Owner
	case ExplosionDamageSource:
		source = world.ExplosionCausingEntity(damage.Source, tx)
	}
	if source != nil && source.H().Type().EncodeEntity() == "minecraft:player" {
		s.lastHurtByPlayer, s.lastHurtByPlayerMemoryTime = source.H().UUID(), 100
	} else if wolf, ok := source.(*Wolf); ok && wolf.Tamed() {
		s.lastHurtByPlayer, _ = uuid.Parse(wolf.behaviour().ownerUUID)
		if s.lastHurtByPlayer != uuid.Nil {
			s.lastHurtByPlayerMemoryTime = 100
		} else {
			s.lastHurtByPlayerMemoryTime = 0
		}
	}
	if s.lastHurtByPlayer == uuid.Nil {
		return nil, false
	}
	player, ok := tx.EntityByUUID(s.lastHurtByPlayer)
	return player, ok && player.H().Type().EncodeEntity() == "minecraft:player"
}
func (c *Creaking) Heal(v float64, _ world.HealingSource) float64 {
	if c.Dead() || v <= 0 {
		return 0
	}
	old := c.Health()
	c.state().health.AddHealth(v)
	return c.Health() - old
}
func (c *Creaking) KnockBack(src mgl64.Vec3, force, height float64) {
	if !c.state().canMove {
		return
	}
	d := c.Position().Sub(src)
	d[1] = height
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	c.SetVelocity(d)
}
func (c *Creaking) AddEffect(v effect.Effect)  { c.state().effects.Add(v, c) }
func (c *Creaking) RemoveEffect(v effect.Type) { c.state().effects.Remove(v, c) }
func (c *Creaking) Effects() []effect.Effect   { return c.state().effects.Effects() }
func (c *Creaking) Speed() float64             { return c.state().speed }
func (c *Creaking) SetSpeed(v float64)         { c.state().speed = v }
func (c *Creaking) FireImmune() bool           { return c.state().bound }
func (c *Creaking) SetOnFire(duration time.Duration) {
	if c.state().bound {
		c.data.FireDuration = 0
		return
	}
	c.Ent.SetOnFire(duration)
}
func (c *Creaking) CanUsePortal() bool { return !c.state().bound }
func (c *Creaking) TravelThroughPortal(tx *world.Tx, target world.Dimension) {
	if !c.state().bound {
		c.Ent.TravelThroughPortal(tx, target)
	}
}
func (c *Creaking) PersistenceRequired() bool { return c.state().persistenceRequired }

// SetPersistenceRequired controls Java's named/persistent mob lifetime rule.
func (c *Creaking) SetPersistenceRequired(required bool) { c.state().persistenceRequired = required }
func (c *Creaking) viewAction(action world.EntityAction) {
	viewers := c.tx.Viewers(c.Position())
	for _, v := range viewers {
		v.ViewEntityAction(c, action)
	}
	c.tx.ReleaseViewers(viewers)
}

func playCreakingSound(e *Ent, tx *world.Tx, soundType string) {
	playCreakingSoundAt(e, tx, soundType, e.Position())
}
func playCreakingSoundAt(e *Ent, tx *world.Tx, soundType string, position mgl64.Vec3) {
	tx.PlaySound(position, sound.NativeEvent{
		SoundType:      soundType,
		EntityType:     "minecraft:creaking",
		ExtraData:      -1,
		EntityUniqueID: e.H().UniqueID(),
	})
}

var _ Living = (*Creaking)(nil)
var _ world.EntityType = creakingType{}
