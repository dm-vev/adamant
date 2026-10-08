package entity

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"sort"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/java26"
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
	if !state.randomInitialized && handle != nil {
		hash := fnv.New64a()
		uuid := handle.UUID()
		_, _ = hash.Write(uuid[:])
		state.random = java26.NewLegacyRandom(int64(hash.Sum64()))
		state.randomInitialized = true
		state.sensorDelay = int(state.random.NextInt(20))
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
		speed = .4
	}
	return &creakingState{health: NewHealthManager(health, 1), effects: NewEffectManager(), mc: &MovementComputer{Gravity: .08, Drag: .02, DragBeforeGravity: true}, speed: speed, home: c.home, homeDimension: c.homeDimension, homeDimensionSet: c.homeDimensionSet, bound: c.bound, canMove: true}
}

type creakingState struct {
	health                                                                      *HealthManager
	effects                                                                     *EffectManager
	mc                                                                          *MovementComputer
	random                                                                      java26.Random
	randomInitialized                                                           bool
	speed                                                                       float64
	home                                                                        cube.Pos
	bound, active, tearingDown, dead                                            bool
	homeDimension                                                               int32
	homeDimensionSet                                                            bool
	attackTicks                                                                 int
	sensorDelay, stuckCounter, attackCooldown, swayTicks, deathTicks, idleTicks int
	lookTarget                                                                  *world.EntityHandle
	lookTicks                                                                   int
	target                                                                      *world.EntityHandle
	nearestPlayers                                                              []*world.EntityHandle
	path                                                                        []cube.Pos
	pathGoal                                                                    cube.Pos
	hasPathGoal                                                                 bool
	pathIndex                                                                   int
	canMove                                                                     bool
	lastHurtByPlayer                                                            uuid.UUID
	lastHurtByPlayerMemoryTime                                                  int
	persistenceRequired                                                         bool
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

func (s *creakingState) Tick(e *Ent, tx *world.Tx) *Movement {
	if s.lastHurtByPlayerMemoryTime > 0 {
		s.lastHurtByPlayerMemoryTime--
	} else {
		s.lastHurtByPlayer = uuid.Nil
	}
	if s.swayTicks > 0 {
		s.swayTicks--
		e.updateState()
	}
	if s.attackCooldown > 0 {
		s.attackCooldown--
	}
	if s.attackTicks > 0 {
		s.attackTicks--
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
		if s.deathTicks == 0 {
			(&Creaking{Ent: e}).viewAction(DeathAction{})
		}
		s.deathTicks++
		if s.deathTicks >= 20 {
			s.dead = true
			_ = e.CloseIn(tx)
			return nil
		}
		e.SetVelocity(mgl64.Vec3{})
		return s.move(e, tx, e.Rotation())
	}
	if s.tearingDown {
		s.deathTicks++
		if s.deathTicks > 45 {
			s.tearDown(e, tx)
			return nil
		}
		e.SetVelocity(mgl64.Vec3{})
		return s.move(e, tx, e.Rotation())
	}
	if s.sensorDelay > 0 {
		s.sensorDelay--
	}
	if s.sensorDelay <= 0 {
		s.refreshPlayers(e, tx)
		s.sensorDelay = 20
	}
	s.checkCanMove(e, tx)
	if s.target == nil {
		s.tickIdle(e, tx)
	} else {
		s.tickFight(e, tx)
	}
	velocity := e.Velocity()
	if !s.canMove {
		velocity[0], velocity[2] = 0, 0
	} else if waterDepth, inLava := creakingFluidState(e, tx); waterDepth > .4 || inLava {
		if s.random.NextFloat() < .8 {
			velocity[1] = .42
		}
	}
	e.SetVelocity(velocity)
	return s.move(e, tx, s.desiredRotation(e, tx))
}
func (s *creakingState) move(e *Ent, tx *world.Tx, rotation cube.Rotation) *Movement {
	movement := s.mc.TickMovement(e, e.Position(), e.Velocity(), rotation, e.Rotation(), tx)
	e.data.Pos, e.data.Vel, e.data.Rot = movement.Position(), movement.Velocity(), movement.Rotation()
	return movement
}
func (s *creakingState) desiredRotation(e *Ent, tx *world.Tx) cube.Rotation {
	look := e.Velocity()
	if look[0]*look[0]+look[2]*look[2] < 1e-8 {
		var handle *world.EntityHandle
		if s.active {
			handle = s.target
		} else {
			handle = s.lookTarget
		}
		if handle != nil {
			if target, ok := handle.Entity(tx); ok {
				look = target.Position().Sub(e.Position())
			}
		}
	}
	rotation := e.Rotation()
	if look.LenSqr() > 1e-8 {
		yaw := math.Atan2(-look[0], look[2]) * 180 / math.Pi
		horizontal := math.Sqrt(look[0]*look[0] + look[2]*look[2])
		pitch := -math.Atan2(look[1], horizontal) * 180 / math.Pi
		maxYaw := 45.0
		if e.Velocity()[0]*e.Velocity()[0]+e.Velocity()[2]*e.Velocity()[2] >= 1e-8 {
			maxYaw = 90
		}
		yawDelta := math.Mod(yaw-rotation.Yaw()+540, 360) - 180
		rotation[0] += max(-maxYaw, min(maxYaw, yawDelta))
		pitchDelta := pitch - rotation.Pitch()
		rotation[1] += max(-90.0, min(90.0, pitchDelta))
	}
	return rotation
}

func (s *creakingState) refreshPlayers(e *Ent, tx *world.Tx) {
	players := make([]world.Entity, 0, 4)
	area := cube.Box(-32, -32, -32, 32, 32, 32).Translate(e.Position())
	for candidate := range tx.EntitiesWithin(area) {
		distance := candidate.Position().Sub(e.Position()).LenSqr()
		if candidate.H() == e.H() || candidate.H().Type().EncodeEntity() != "minecraft:player" || distance >= 1024 {
			continue
		}
		if living, ok := candidate.(Living); !ok || living.Dead() {
			continue
		}
		if mode, ok := candidate.(interface{ GameMode() world.GameMode }); ok && mode.GameMode() == world.GameModeSpectator {
			continue
		}
		players = append(players, candidate)
	}
	sort.Slice(players, func(i, j int) bool {
		return players[i].Position().Sub(e.Position()).LenSqr() < players[j].Position().Sub(e.Position()).LenSqr()
	})
	s.nearestPlayers = s.nearestPlayers[:0]
	for _, player := range players {
		s.nearestPlayers = append(s.nearestPlayers, player.H())
	}
	if s.target != nil {
		target, ok := s.target.Entity(tx)
		if !ok || !creakingCanAttack(target) || target.H().Type().EncodeEntity() != "minecraft:player" || !creakingVisible(e, target, tx) {
			s.target = nil
			s.path = nil
			s.hasPathGoal = false
		}
	}
	if s.active && s.target == nil {
		for _, handle := range s.nearestPlayers {
			target, ok := handle.Entity(tx)
			if ok && creakingCanAttack(target) && creakingVisible(e, target, tx) {
				s.target = handle
				break
			}
		}
	}
}
func (s *creakingState) checkCanMove(e *Ent, tx *world.Tx) {
	before := s.canMove
	defer func() {
		if before != s.canMove {
			e.updateState()
		}
	}()
	if len(s.nearestPlayers) == 0 {
		if s.active {
			s.deactivate(e, tx)
		}
		s.target = nil
		s.canMove = true
		return
	}
	hasPotential := false
	for _, handle := range s.nearestPlayers {
		p, ok := handle.Entity(tx)
		if !ok || !creakingCanAttack(p) {
			continue
		}
		hasPotential = true
		if (!s.active || !creakingDisguise(p)) && creakingLookingAt(e, p, tx) {
			if s.active {
				s.canMove = false
				return
			}
			if p.Position().Sub(e.Position()).LenSqr() < 144 {
				s.activate(e, tx, p)
				s.canMove = false
				return
			}
		}
	}
	if !hasPotential && s.active {
		s.deactivate(e, tx)
	}
	s.canMove = true
}
func creakingCanAttack(p world.Entity) bool {
	if l, ok := p.(Living); !ok || l.Dead() {
		return false
	}
	if mode, ok := p.(interface{ GameMode() world.GameMode }); ok {
		if mode.GameMode() == world.GameModeSpectator || mode.GameMode().CreativeInventory() {
			return false
		}
	}
	return true
}
func creakingDisguise(p world.Entity) bool {
	if disguise, ok := p.(interface{ WearingCreakingDisguise() bool }); ok {
		return disguise.WearingCreakingDisguise()
	}
	return false
}
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
func creakingLookTarget(e *Ent, tx *world.Tx) world.Entity {
	var nearest world.Entity
	best := 64.0
	area := cube.Box(-8, -8, -8, 8, 8, 8).Translate(e.Position())
	for candidate := range tx.EntitiesWithin(area) {
		if candidate.H() == e.H() {
			continue
		}
		if living, ok := candidate.(Living); !ok || living.Dead() {
			continue
		}
		distance := candidate.Position().Sub(e.Position()).LenSqr()
		if distance < best && creakingVisible(e, candidate, tx) {
			nearest, best = candidate, distance
		}
	}
	return nearest
}
func creakingVisible(e *Ent, p world.Entity, tx *world.Tx) bool {
	return dragonLineOfSight(tx, e.Position().Add(mgl64.Vec3{0, float64(float32(2.3)), 0}), p.Position().Add(mgl64.Vec3{0, playerEyeHeight(p), 0}))
}
func (s *creakingState) activate(e *Ent, tx *world.Tx, target world.Entity) {
	s.active = true
	s.target = target.H()
	s.path = nil
	s.hasPathGoal = false
	playCreakingSoundAt(e, tx, "mob.warning", e.Position().Add(mgl64.Vec3{0, 2.295, 0}))
	playCreakingSoundAt(e, tx, "stare", e.Position().Add(mgl64.Vec3{0, 2.295, 0}))
	playCreakingSound(e, tx, "activate")
}
func (s *creakingState) deactivate(e *Ent, tx *world.Tx) {
	s.active = false
	s.target = nil
	s.path = nil
	s.hasPathGoal = false
	playCreakingSound(e, tx, "deactivate")
}
func (s *creakingState) tickIdle(e *Ent, tx *world.Tx) {
	if s.lookTicks > 0 {
		s.lookTicks--
		if s.lookTicks == 0 {
			if target := creakingLookTarget(e, tx); target != nil {
				s.lookTarget = target.H()
			}
		}
	} else {
		s.lookTicks = 29 + int(s.random.NextInt(31))
	}
	if s.lookTarget != nil {
		target, ok := s.lookTarget.Entity(tx)
		living, isLiving := target.(Living)
		if !ok || !isLiving || living.Dead() || !creakingVisible(e, target, tx) {
			s.lookTarget = nil
		}
	}
	if s.idleTicks > 0 {
		s.idleTicks--
		return
	}
	if len(s.path) == 0 || s.pathIndex >= len(s.path) {
		keys := [3]float64{
			-math.Pow(float64(s.random.NextFloat()), .5),
			-math.Pow(float64(s.random.NextFloat()), .5),
			-math.Pow(float64(s.random.NextFloat()), 1),
		}
		order := [3]int{0, 1, 2}
		sort.Slice(order[:], func(i, j int) bool { return keys[order[i]] < keys[order[j]] })
		for _, choice := range order {
			switch choice {
			case 0:
				if s.setRandomPath(e, tx, 10, 7) {
					return
				}
			case 1:
				if s.lookTarget != nil {
					if target, ok := s.lookTarget.Entity(tx); ok && target.Position().Sub(e.Position()).LenSqr() > 9 {
						s.setPath(e, tx, cube.PosFromVec3(target.Position()))
						if len(s.path) > 0 {
							return
						}
					}
				}
			case 2:
				s.idleTicks = 30 + int(s.random.NextInt(31))
				return
			}
		}
	}
	s.followPath(e, tx, .3)
}
func (s *creakingState) tickFight(e *Ent, tx *world.Tx) {
	if s.target == nil {
		return
	}
	target, ok := s.target.Entity(tx)
	if !ok || !creakingCanAttack(target) || target.H().Type().EncodeEntity() != "minecraft:player" || !creakingVisible(e, target, tx) {
		s.target = nil
		s.path = nil
		s.hasPathGoal = false
		return
	}
	if s.targetOutOfReach(e, target) {
		s.setPath(e, tx, cube.PosFromVec3(target.Position()))
		s.followPath(e, tx, 1)
	} else {
		s.path = nil
	}
	if !s.canMove || s.attackCooldown > 0 || s.targetOutOfReach(e, target) || !creakingVisible(e, target, tx) {
		return
	}
	living, ok := target.(Living)
	if !ok {
		return
	}
	living.Hurt(3, AttackDamageSource{Attacker: &Creaking{Ent: e}})
	s.attackCooldown = 40
	s.attackTicks = 15
}
func (s *creakingState) targetOutOfReach(e *Ent, target world.Entity) bool {
	attacker := CreakingType.BBox(e).Translate(e.Position()).GrowVec3(mgl64.Vec3{math.Sqrt(2.04) - .6, 0, math.Sqrt(2.04) - .6})
	targetBox := target.H().Type().BBox(target).Translate(target.Position())
	return !attacker.IntersectsWith(targetBox)
}
func (s *creakingState) setRandomPath(e *Ent, tx *world.Tx, horizontal, vertical int) bool {
	origin := e.Position()
	var chosen cube.Pos
	found := false
	for range 10 {
		x := float64(s.random.NextInt(int32(horizontal*2+1)) - int32(horizontal))
		y := int(s.random.NextInt(int32(vertical*2+1))) - vertical
		z := float64(s.random.NextInt(int32(horizontal*2+1)) - int32(horizontal))
		if s.bound && horizontal > 1 {
			if origin[0] > float64(s.home.X()) {
				x -= s.random.NextDouble() * float64(horizontal) / 2
			} else {
				x += s.random.NextDouble() * float64(horizontal) / 2
			}
			if origin[2] > float64(s.home.Z()) {
				z -= s.random.NextDouble() * float64(horizontal) / 2
			} else {
				z += s.random.NextDouble() * float64(horizontal) / 2
			}
		}
		goal := cube.Pos{int(math.Floor(origin[0] + x)), int(math.Floor(origin[1] + float64(y))), int(math.Floor(origin[2] + z))}
		for goal.Y() < tx.Range()[1] && !isPassable(tx, goal) {
			goal = goal.Side(cube.FaceUp)
		}
		if !found && walkable(tx, goal, false, true) {
			chosen, found = goal, true
		}
	}
	if found {
		s.setPath(e, tx, chosen)
	}
	return found
}
func (s *creakingState) setPath(e *Ent, tx *world.Tx, goal cube.Pos) {
	if s.hasPathGoal && goal.Sub(s.pathGoal).Vec3().LenSqr() <= 4 {
		return
	}
	s.pathGoal, s.hasPathGoal = goal, true
	start := cube.PosFromVec3(e.Position())
	if s.bound {
		homeDistance := start.Sub(s.home).Vec3().LenSqr()
		s.path = findPathWithEvaluator(tx, start, goal, 512, true, true, func(node cube.Pos) bool {
			distance := node.Sub(s.home).Vec3().LenSqr()
			return distance <= 1024 || distance < homeDistance
		})
	} else {
		s.path = findPath(tx, start, goal, 512, true, true)
	}
	s.pathIndex = 0
}
func (s *creakingState) followPath(e *Ent, tx *world.Tx, speed float64) {
	if s.pathIndex >= len(s.path) {
		e.SetVelocity(mgl64.Vec3{})
		return
	}
	node := s.path[s.pathIndex]
	target := node.Vec3Centre()
	delta := target.Sub(e.Position())
	delta[1] = 0
	if delta.Len() < .25 {
		s.pathIndex++
		return
	}
	if delta.Len() == 0 {
		return
	}
	velocity := delta.Normalize().Mul(speed * .4)
	velocity[1] = e.Velocity()[1]
	e.SetVelocity(velocity)
}
func (s *creakingState) stuckInPlayer(e *Ent, tx *world.Tx) bool {
	box := CreakingType.BBox(e).Translate(e.Position())
	for _, handle := range s.nearestPlayers {
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

func creakingFluidState(e *Ent, tx *world.Tx) (waterDepth float64, inLava bool) {
	box := CreakingType.BBox(e).Translate(e.Position())
	min, max := box.Min(), box.Max()
	for x := int(math.Floor(min[0])); float64(x) < max[0]; x++ {
		for y := int(math.Floor(min[1])); float64(y) < max[1]; y++ {
			for z := int(math.Floor(min[2])); float64(z) < max[2]; z++ {
				pos := cube.Pos{x, y, z}
				liquid, ok := tx.Liquid(pos)
				if !ok {
					continue
				}
				switch liquid.LiquidType() {
				case "lava", "water":
					depth := float64(y) + liquidSurfaceHeight(liquid) - min[1]
					if depth <= 0 {
						continue
					}
					if liquid.LiquidType() == "lava" {
						inLava = true
					} else if depth > waterDepth {
						waterDepth = depth
					}
				}
			}
		}
	}
	return
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
