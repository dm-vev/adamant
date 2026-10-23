package entity

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
)

// This file holds the goals of the hostile mobs driven by a MobAI, ported
// from the registerGoals methods of their Java classes.

func (b *hostileState) mobAI() *MobAI { return &b.ai }

// hostileUsesAI reports if the mobs of a kind are driven by a MobAI.
func hostileUsesAI(kind string) bool {
	switch kind {
	case "minecraft:zombie", "minecraft:husk", "minecraft:zombie_villager":
		return true
	}
	return false
}

// hostileSelf returns the entity of a hostile mob for the species of kind.
func hostileSelf(e *Ent, kind string) world.Entity {
	m := &HostileMob{Ent: e}
	switch kind {
	case "minecraft:zombie":
		return &Zombie{m}
	case "minecraft:zombie_villager":
		return &ZombieVillager{m}
	case "minecraft:husk":
		return &Husk{m}
	}
	return m
}

// aiTick runs the MobAI of a hostile mob.
func (b *hostileState) aiTick(e *Ent) *Movement {
	ai := &b.ai
	ai.MovementSpeed = b.speed
	ai.FollowRange = 35
	ai.EyeHeight = 1.74
	if b.baby {
		ai.EyeHeight = .93
	}
	return ai.Tick(e, hostileSelf(e, b.kind), b.registerGoals)
}

// registerGoals adds the goals of the mob, as the registerGoals of its Java
// class.
func (b *hostileState) registerGoals(m *Mob) {
	switch b.kind {
	case "minecraft:zombie", "minecraft:husk", "minecraft:zombie_villager":
		registerZombieGoals(m, b)
	}
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
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(entityIs("minecraft:villager", "minecraft:wandering_trader"), false))
	ai.Targets.Add(3, NewNearestAttackableTargetGoal(entityIs("minecraft:iron_golem"), true))
	turtles := NewNearestAttackableTargetGoal(entityIs("minecraft:turtle"), true)
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
		damage += w.AttackDamage()
	}
	dealt, hurt := l.Hurt(damage, AttackDamageSource{Attacker: m.Self})
	if !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
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
	}
	return true
}

// BreakDoorGoal makes a mob break the wooden door in its way, ported from
// Java's BreakDoorGoal and DoorInteractGoal.
type BreakDoorGoal struct {
	goalFlags
	validDifficulty func(world.Difficulty) bool

	doorPos           cube.Pos
	hasDoor, passed   bool
	doorDirX          float64
	doorDirZ          float64
	breakTime         int
	lastBreakProgress int
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

// isOpen ports DoorInteractGoal.isOpen.
func (g *BreakDoorGoal) isOpen(m *Mob) bool {
	if !g.hasDoor {
		return false
	}
	open, ok := doorOpen(m.tx, g.doorPos)
	if !ok {
		g.hasDoor = false
		return false
	}
	return open
}

// findDoor ports DoorInteractGoal.canUse.
func (g *BreakDoorGoal) findDoor(m *Mob) bool {
	if !m.AI.collidedHorizontally {
		return false
	}
	path := m.AI.nav.Path()
	if path == nil || path.Done() {
		return false
	}
	for i := 0; i < min(path.next+2, path.NodeCount()); i++ {
		n := path.nodes[i]
		g.doorPos = cube.Pos{n.x, n.y + 1, n.z}
		dx, dz := float64(g.doorPos.X())-m.data.Pos[0], float64(g.doorPos.Z())-m.data.Pos[2]
		if dx*dx+dz*dz > 2.25 {
			continue
		}
		if g.hasDoor = woodenDoor(m.tx, g.doorPos); g.hasDoor {
			return true
		}
	}
	g.doorPos = m.BlockPos().Side(cube.FaceUp)
	g.hasDoor = woodenDoor(m.tx, g.doorPos)
	return g.hasDoor
}

func (g *BreakDoorGoal) CanUse(m *Mob) bool {
	if !g.findDoor(m) || mobGriefingDisabled(m.tx) {
		return false
	}
	return g.validDifficulty(m.tx.World().Difficulty()) && !g.isOpen(m)
}

func (g *BreakDoorGoal) CanContinueToUse(m *Mob) bool {
	return !g.passed && g.breakTime <= doorBreakTime && !g.isOpen(m) &&
		g.doorPos.Vec3Centre().Sub(m.data.Pos).LenSqr() < 4 && g.validDifficulty(m.tx.World().Difficulty())
}

func (g *BreakDoorGoal) Start(m *Mob) {
	g.passed = false
	g.doorDirX = float64(g.doorPos.X()) + .5 - m.data.Pos[0]
	g.doorDirZ = float64(g.doorPos.Z()) + .5 - m.data.Pos[2]
	g.breakTime = 0
	viewers := m.tx.Viewers(g.doorPos.Vec3Centre())
	for _, v := range viewers {
		v.ViewBlockAction(g.doorPos, block.StartCrackAction{BreakTime: doorBreakTime * time.Second / 20})
	}
	m.tx.ReleaseViewers(viewers)
}

func (g *BreakDoorGoal) Stop(m *Mob) {
	viewers := m.tx.Viewers(g.doorPos.Vec3Centre())
	for _, v := range viewers {
		v.ViewBlockAction(g.doorPos, block.StopCrackAction{})
	}
	m.tx.ReleaseViewers(viewers)
	g.lastBreakProgress = -1
}

func (g *BreakDoorGoal) RequiresUpdateEveryTick() bool { return true }

func (g *BreakDoorGoal) Tick(m *Mob) {
	dx, dz := float64(g.doorPos.X())+.5-m.data.Pos[0], float64(g.doorPos.Z())+.5-m.data.Pos[2]
	if g.doorDirX*dx+g.doorDirZ*dz < 0 {
		g.passed = true
	}
	if m.Random().NextInt(20) == 0 {
		m.tx.PlaySound(g.doorPos.Vec3Centre(), sound.Custom{Name: "entity.zombie.attack_wooden_door", Volume: 1, Pitch: 1})
		m.swing()
	}
	g.breakTime++
	if g.breakTime == doorBreakTime && g.validDifficulty(m.tx.World().Difficulty()) {
		door := m.tx.Block(g.doorPos)
		lower := g.doorPos
		switch d := door.(type) {
		case block.WoodDoor:
			if d.Top {
				lower = g.doorPos.Side(cube.FaceDown)
			}
		case block.CopperDoor:
			if d.Top {
				lower = g.doorPos.Side(cube.FaceDown)
			}
		}
		m.tx.SetBlock(lower, nil, nil)
		m.tx.SetBlock(lower.Side(cube.FaceUp), nil, nil)
		m.tx.PlaySound(g.doorPos.Vec3Centre(), sound.Custom{Name: "entity.zombie.break_wooden_door", Volume: 1, Pitch: 1})
		m.tx.AddParticle(g.doorPos.Vec3Centre(), particle.BlockBreak{Block: door})
	}
}
