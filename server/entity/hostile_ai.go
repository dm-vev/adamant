package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/potion"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// This file holds the goals of the hostile mobs driven by a MobAI, ported
// from the registerGoals methods of their Java classes.

func (b *hostileState) mobAI() *MobAI { return &b.ai }

// hostileSkeleton reports if a kind is a skeleton, Java's AbstractSkeleton.
func hostileSkeleton(kind string) bool {
	switch kind {
	case "minecraft:skeleton", "minecraft:stray", "minecraft:bogged", "minecraft:parched", "minecraft:wither_skeleton":
		return true
	}
	return false
}

// hostileSelf returns the entity of a hostile mob for the species of kind.
func hostileSelf(e *Ent, kind string) world.Entity {
	return hostileSpecies(&HostileMob{Ent: e}, kind)
}

// aiTick runs the MobAI of a hostile mob.
func (b *hostileState) aiTick(e *Ent) *Movement {
	ai := &b.ai
	ai.MovementSpeed = b.speed
	ai.FollowRange = 16
	ai.EyeHeight = 1.74
	switch {
	case b.kind == "minecraft:wither_skeleton":
		ai.EyeHeight = 2.1
	case hostileSkeleton(b.kind):
	case b.kind == "minecraft:vex":
		ai.EyeHeight, ai.NoGravity, ai.NoPhysics = .51875, true, true
	case b.kind == "minecraft:ravager":
		ai.EyeHeight, ai.FollowRange, ai.StepHeight, ai.MaxHeadYaw = 1.87, 32, 1, 45
	case b.kind == "minecraft:pillager":
		ai.EyeHeight, ai.FollowRange = 1.6575, 32
	case hostileIllager(b.kind):
		ai.EyeHeight, ai.FollowRange = 1.6575, 12
	default:
		ai.FollowRange = 35
		if b.baby {
			ai.EyeHeight = .93
		}
	}
	if a, ok := b.attrs["minecraft:follow_range"]; ok {
		// The follow range of the kind, raised by the bonus it spawned with.
		a.Base = ai.FollowRange
		ai.FollowRange = a.value(0, 2048)
	}
	self := hostileSelf(e, b.kind)
	if hostileRaidMob(b.kind) {
		b.raiderTick(ai.Mob(e, self))
	}
	if b.drawingBow {
		b.useTicks++
	}
	if hostileSkeleton(b.kind) && ai.started && (!b.weaponSet || b.weaponBow != b.holdingBow()) {
		b.reassessWeaponGoal(ai.Mob(e, self))
	}
	mov := ai.Tick(e, self, b.registerGoals)
	if !b.dead {
		switch b.kind {
		case "minecraft:ravager":
			b.tickRavager(ai.Mob(e, self))
		case "minecraft:vex":
			b.tickVexLife(ai.Mob(e, self))
		}
	}
	return mov
}

// registerGoals adds the goals of the mob, as the registerGoals of its Java
// class.
func (b *hostileState) registerGoals(m *Mob) {
	switch b.kind {
	case "minecraft:zombie", "minecraft:husk", "minecraft:zombie_villager":
		registerZombieGoals(m, b)
	case "minecraft:pillager":
		registerPillagerGoals(m, b)
	case "minecraft:vindicator":
		registerVindicatorGoals(m, b)
	case "minecraft:evoker":
		registerEvokerGoals(m, b)
	case "minecraft:vex":
		registerVexGoals(m, b)
	case "minecraft:ravager":
		registerRavagerGoals(m, b)
	default:
		if hostileSkeleton(b.kind) {
			registerSkeletonGoals(m, b)
		}
	}
}

// registerSkeletonGoals ports AbstractSkeleton.registerGoals and the
// override of WitherSkeleton, which also targets piglins.
func registerSkeletonGoals(m *Mob, b *hostileState) {
	ai := m.AI
	ai.Goals.Add(2, &RestrictSunGoal{})
	ai.Goals.Add(3, NewFleeSunGoal(1))
	ai.Goals.Add(3, NewAvoidEntityTypesGoal(entityTypes{"minecraft:wolf"}, 6, 1, 1.2))
	ai.Goals.Add(5, NewWaterAvoidingRandomStrollGoal(1))
	ai.Goals.Add(6, NewLookAtPlayerGoal(8))
	ai.Goals.Add(6, NewRandomLookAroundGoal())
	if b.kind == "minecraft:wither_skeleton" {
		ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:piglin", "minecraft:piglin_brute"}, true))
	}
	ai.Targets.Add(1, NewHurtByTargetGoal())
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:iron_golem"}, true))
	turtles := NewNearestAttackableTypesGoal(entityTypes{"minecraft:turtle"}, true)
	turtles.Selector = func(m *Mob, target world.Entity) bool {
		baby, ok := target.(interface{ Baby() bool })
		return ok && baby.Baby() && !pathWater(m.tx, cube.PosFromVec3(target.Position()))
	}
	ai.Targets.Add(3, turtles)
	b.reassessWeaponGoal(m)
}

// reassessWeaponGoal ports AbstractSkeleton.reassessWeaponGoal: a skeleton
// with a bow shoots, one without fights in melee.
func (b *hostileState) reassessWeaponGoal(m *Mob) {
	ai := m.AI
	if b.weaponGoal != nil {
		old := b.weaponGoal
		ai.Goals.Remove(m, func(g Goal) bool { return g == old })
	}
	b.weaponSet, b.weaponBow = true, b.holdingBow()
	if b.weaponBow {
		hard, normal := 20, 40
		if b.kind == "minecraft:bogged" || b.kind == "minecraft:parched" {
			hard, normal = 50, 70
		}
		interval := normal
		if m.tx.World().Difficulty() == world.DifficultyHard {
			interval = hard
		}
		b.weaponGoal = NewRangedBowAttackGoal(1, interval, 15)
	} else {
		b.weaponGoal = NewMeleeAttackGoal(1.2, false)
	}
	ai.Goals.Add(4, b.weaponGoal)
}

func (b *hostileState) holdingBow() bool {
	_, ok := b.main.Item().(item.Bow)
	return ok && !b.main.Empty()
}

func (b *hostileState) usingItem() bool { return b.drawingBow }

func (b *hostileState) ticksUsingItem() int { return b.useTicks }

func (b *hostileState) startUsingItem(m *Mob) {
	if !b.drawingBow {
		b.drawingBow, b.useTicks = true, 0
		m.updateState()
	}
}

func (b *hostileState) stopUsingItem(m *Mob) {
	if b.drawingBow {
		b.drawingBow, b.useTicks = false, 0
		m.updateState()
	}
}

func (b *hostileState) headEmpty() bool { return b.head.Empty() }

// performRangedAttack ports AbstractSkeleton.performRangedAttack with the
// arrows of its kinds: strays slow, bogged poison and parched weaken.
func (b *hostileState) performRangedAttack(m *Mob, target world.Entity, power float64) {
	pos := mgl64.Vec3{m.data.Pos[0], m.EyeY() - .1, m.data.Pos[2]}
	targetY := target.Position()[1] + target.H().Type().BBox(target).Height()/3
	d := mgl64.Vec3{target.Position()[0] - pos[0], targetY - pos[1], target.Position()[2] - pos[2]}
	d[1] += math.Hypot(d[0], d[2]) * .2
	vel := shootDirection(m, d, 1.6, rangedAttackUncertainty(m))
	id, _ := world.DifficultyID(m.tx.World().Difficulty())
	r := m.Random()
	damage := power*2 + float64(id)*.11 + .57425*(r.NextDouble()-r.NextDouble())
	opts := world.EntitySpawnOpts{Position: pos, Velocity: vel}
	var arrow *world.EntityHandle
	switch b.kind {
	case "minecraft:stray":
		arrow = NewTippedArrowWithDamage(opts, damage, m.Self, potion.Slowness())
	case "minecraft:bogged":
		arrow = NewTippedArrowWithDamage(opts, damage, m.Self, potion.Poison())
	case "minecraft:parched":
		arrow = NewTippedArrowWithDamage(opts, damage, m.Self, potion.LongWeakness())
	default:
		arrow = NewArrowWithDamage(opts, damage, m.Self)
	}
	e := m.tx.AddEntity(arrow)
	if b.kind == "minecraft:wither_skeleton" {
		if f, ok := e.(interface{ SetOnFire(time.Duration) }); ok {
			f.SetOnFire(100 * time.Second)
		}
	}
	m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.skeleton.shoot", Volume: 1, Pitch: 1 / (float64(r.NextFloat())*.4 + .8)})
}

// registerZombieGoals ports Zombie.registerGoals and addBehaviourGoals.
func registerZombieGoals(m *Mob, b *hostileState) {
	ai := m.AI
	ai.Goals.Add(8, NewLookAtPlayerGoal(8))
	ai.Goals.Add(8, NewRandomLookAroundGoal())
	attack := NewMeleeAttackGoal(1, false)
	raiseArmTicks := 0
	attack.OnTick = func(m *Mob, g *MeleeAttackGoal) {
		raiseArmTicks++
		m.SetAggressive(raiseArmTicks >= 5 && g.TicksUntilNextAttack() < g.AttackInterval()/2)
	}
	ai.Goals.Add(2, newSpearUseGoal(1, 1, 10, 2))
	ai.Goals.Add(3, &zombieAttackGoal{MeleeAttackGoal: attack, raiseArmTicks: &raiseArmTicks})
	ai.Goals.Add(7, NewWaterAvoidingRandomStrollGoal(1))
	if b.canBreakDoors {
		ai.nav.SetCanOpenDoors(true)
		ai.Goals.Add(1, NewBreakDoorGoal(func(d world.Difficulty) bool { return d == world.DifficultyHard }))
	}
	hurt := NewHurtByTargetGoal()
	hurt.AlertOthers, hurt.AlertIgnore = true, entityIs("minecraft:zombified_piglin")
	ai.Targets.Add(1, hurt)
	ai.Targets.Add(2, NewNearestAttackableTargetGoal(nil, true))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:villager", "minecraft:wandering_trader"}, false))
	ai.Targets.Add(3, NewNearestAttackableTypesGoal(entityTypes{"minecraft:iron_golem"}, true))
	turtles := NewNearestAttackableTypesGoal(entityTypes{"minecraft:turtle"}, true)
	turtles.Selector = func(m *Mob, target world.Entity) bool {
		baby, ok := target.(interface{ Baby() bool })
		return ok && baby.Baby() && !pathWater(m.tx, cube.PosFromVec3(target.Position()))
	}
	ai.Targets.Add(5, turtles)
}

// zombieAttackGoal is Java's ZombieAttackGoal: a melee attack that lowers
// the arms of the zombie when it stops.
type zombieAttackGoal struct {
	*MeleeAttackGoal
	raiseArmTicks *int
}

func (g *zombieAttackGoal) Start(m *Mob) {
	g.MeleeAttackGoal.Start(m)
	*g.raiseArmTicks = 0
}

func (g *zombieAttackGoal) Stop(m *Mob) {
	g.MeleeAttackGoal.Stop(m)
	m.SetAggressive(false)
}

// walkTargetValue ports Monster.getWalkTargetValue.
func (b *hostileState) walkTargetValue(m *Mob, pos cube.Pos) float64 {
	return monsterWalkTarget(m, pos)
}

// doHurtTarget ports Mob.doHurtTarget with the overrides of Zombie and Husk:
// a burning zombie with empty hands may set its target alight and a husk
// makes it hungry.
func (b *hostileState) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	damage := b.attack
	if w, ok := b.main.Item().(item.Weapon); ok {
		// The attack damage of a weapon counts the point a bare hand deals,
		// which the attribute of a mob replaces.
		damage += w.AttackDamage() - 1
	}
	if b.kind == "minecraft:ravager" {
		b.ravagerAttack = 10
		m.tx.PlaySound(m.data.Pos, sound.Custom{Name: "entity.ravager.attack", Volume: 1, Pitch: 1})
	}
	dealt, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self})
	if !hurt {
		return false
	}
	if b.kind == "minecraft:ravager" {
		// The ATTACK_KNOCKBACK of 1.5 adds to the knockback of the hit.
		l.KnockBack(m.data.Pos, .95, .4)
	} else {
		l.KnockBack(m.data.Pos, .4, .4)
	}
	switch b.kind {
	case "minecraft:zombie", "minecraft:zombie_villager", "minecraft:husk":
		difficulty := float64(m.tx.LocalDifficulty(m.BlockPos()).Effective)
		if b.main.Empty() && m.OnFireDuration() > 0 && float64(m.Random().NextFloat()) < difficulty*.3 {
			if f, ok := target.(interface{ SetOnFire(time.Duration) }); ok {
				f.SetOnFire(time.Duration(2*int(difficulty)) * time.Second)
			}
		}
		if l.Dead() {
			(&HostileMob{Ent: m.Ent}).convertVillager(m.tx, target)
		}
		if b.kind == "minecraft:husk" && b.main.Empty() && dealt > 0 {
			ticks := 140 * int(difficulty)
			l.AddEffect(effect.New(effect.Hunger, 1, time.Duration(ticks)*time.Second/20))
		}
	case "minecraft:wither_skeleton":
		l.AddEffect(effect.New(effect.Wither, 1, 10*time.Second))
	}
	return true
}

// BreakDoorGoal makes a mob break the wooden door in its way, ported from
// Java's BreakDoorGoal and DoorInteractGoal.
type BreakDoorGoal struct {
	goalFlags
	validDifficulty func(world.Difficulty) bool

	door              doorInteract
	breakTime         int
	lastBreakProgress int
}

// doorInteract ports DoorInteractGoal: the door a mob walks into and
// whether it passed it.
type doorInteract struct {
	pos             cube.Pos
	hasDoor, passed bool
	dirX, dirZ      float64
}

// NewBreakDoorGoal returns a BreakDoorGoal for the difficulties valid
// returns true for.
func NewBreakDoorGoal(valid func(world.Difficulty) bool) *BreakDoorGoal {
	return &BreakDoorGoal{validDifficulty: valid, lastBreakProgress: -1}
}

const doorBreakTime = 240

// woodenDoor reports if a position holds a door a mob opens by hand.
func woodenDoor(tx *world.Tx, pos cube.Pos) bool {
	switch tx.Block(pos).(type) {
	case block.WoodDoor, block.CopperDoor:
		return true
	}
	return false
}

func doorOpen(tx *world.Tx, pos cube.Pos) (open, ok bool) {
	switch d := tx.Block(pos).(type) {
	case block.WoodDoor:
		return d.Open, true
	case block.CopperDoor:
		return d.Open, true
	}
	return false, false
}

// setDoorOpen ports DoorBlock.setOpen: it opens or closes the wooden door
// at a position, with its sound.
func setDoorOpen(tx *world.Tx, pos cube.Pos, open bool) {
	if current, ok := doorOpen(tx, pos); !ok || current == open {
		return
	}
	switch d := tx.Block(pos).(type) {
	case block.WoodDoor:
		d.Activate(pos, cube.FaceUp, tx, nil, &item.UseContext{})
	case block.CopperDoor:
		d.Activate(pos, cube.FaceUp, tx, nil, &item.UseContext{})
	}
	event := world.GameEventBlockClose
	if open {
		event = world.GameEventBlockOpen
	}
	tx.EmitGameEvent(event, pos.Vec3Centre(), world.GameEventContext{})
}

// open ports DoorInteractGoal.isOpen.
func (d *doorInteract) open(m *Mob) bool {
	if !d.hasDoor {
		return false
	}
	open, ok := doorOpen(m.tx, d.pos)
	if !ok {
		d.hasDoor = false
		return false
	}
	return open
}

// find ports DoorInteractGoal.canUse: the wooden door in the next nodes of
// the path of a mob that ran into something, or right above its feet.
func (d *doorInteract) find(m *Mob) bool {
	if !m.AI.collidedHorizontally {
		return false
	}
	path := m.AI.nav.Path()
	if path == nil || path.Done() {
		return false
	}
	for i := 0; i < min(path.next+2, path.NodeCount()); i++ {
		n := path.nodes[i]
		d.pos = cube.Pos{n.x, n.y + 1, n.z}
		dx, dz := float64(d.pos.X())-m.data.Pos[0], float64(d.pos.Z())-m.data.Pos[2]
		if dx*dx+dz*dz > 2.25 {
			continue
		}
		if d.hasDoor = woodenDoor(m.tx, d.pos); d.hasDoor {
			return true
		}
	}
	d.pos = m.BlockPos().Side(cube.FaceUp)
	d.hasDoor = woodenDoor(m.tx, d.pos)
	return d.hasDoor
}

// start ports DoorInteractGoal.start.
func (d *doorInteract) start(m *Mob) {
	d.passed = false
	d.dirX = float64(d.pos.X()) + .5 - m.data.Pos[0]
	d.dirZ = float64(d.pos.Z()) + .5 - m.data.Pos[2]
}

// tick ports DoorInteractGoal.tick: the mob passed the door once it is on
// its other side.
func (d *doorInteract) tick(m *Mob) {
	dx, dz := float64(d.pos.X())+.5-m.data.Pos[0], float64(d.pos.Z())+.5-m.data.Pos[2]
	if d.dirX*dx+d.dirZ*dz < 0 {
		d.passed = true
	}
}

func (g *BreakDoorGoal) CanUse(m *Mob) bool {
	if !g.door.find(m) || mobGriefingDisabled(m.tx) {
		return false
	}
	return g.validDifficulty(m.tx.World().Difficulty()) && !g.door.open(m)
}

func (g *BreakDoorGoal) CanContinueToUse(m *Mob) bool {
	return !g.door.passed && g.breakTime <= doorBreakTime && !g.door.open(m) &&
		g.door.pos.Vec3Centre().Sub(m.data.Pos).LenSqr() < 4 && g.validDifficulty(m.tx.World().Difficulty())
}

func (g *BreakDoorGoal) Start(m *Mob) {
	g.door.start(m)
	g.breakTime = 0
	viewers := m.tx.Viewers(g.door.pos.Vec3Centre())
	for _, v := range viewers {
		v.ViewBlockAction(g.door.pos, block.StartCrackAction{BreakTime: doorBreakTime * time.Second / 20})
	}
	m.tx.ReleaseViewers(viewers)
}

func (g *BreakDoorGoal) Stop(m *Mob) {
	viewers := m.tx.Viewers(g.door.pos.Vec3Centre())
	for _, v := range viewers {
		v.ViewBlockAction(g.door.pos, block.StopCrackAction{})
	}
	m.tx.ReleaseViewers(viewers)
	g.lastBreakProgress = -1
}

func (g *BreakDoorGoal) RequiresUpdateEveryTick() bool { return true }

func (g *BreakDoorGoal) Tick(m *Mob) {
	g.door.tick(m)
	if m.Random().NextInt(20) == 0 {
		m.tx.PlaySound(g.door.pos.Vec3Centre(), sound.Custom{Name: "entity.zombie.attack_wooden_door", Volume: 1, Pitch: 1})
		m.swing()
	}
	g.breakTime++
	if g.breakTime == doorBreakTime && g.validDifficulty(m.tx.World().Difficulty()) {
		door := m.tx.Block(g.door.pos)
		lower := g.door.pos
		switch d := door.(type) {
		case block.WoodDoor:
			if d.Top {
				lower = g.door.pos.Side(cube.FaceDown)
			}
		case block.CopperDoor:
			if d.Top {
				lower = g.door.pos.Side(cube.FaceDown)
			}
		}
		m.tx.SetBlock(lower, nil, nil)
		m.tx.SetBlock(lower.Side(cube.FaceUp), nil, nil)
		m.tx.PlaySound(g.door.pos.Vec3Centre(), sound.Custom{Name: "entity.zombie.break_wooden_door", Volume: 1, Pitch: 1})
		m.tx.AddParticle(g.door.pos.Vec3Centre(), particle.BlockBreak{Block: door})
	}
}

// attackDamageBase returns the attack damage attribute of the mob, without
// its weapon.
func (b *hostileState) attackDamageBase() float64 { return b.attack }
