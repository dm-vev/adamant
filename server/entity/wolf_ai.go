package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// This file ports the goals of Java's Wolf.

func (b *WolfBehaviour) tamableState() *tamable { return &b.tamable }

// registerGoals ports Wolf.registerGoals.
func (b *WolfBehaviour) registerGoals(m *Mob) {
	ai := m.AI
	ai.SetPathfindingMalus(PathPowderSnow, -1)
	ai.SetPathfindingMalus(PathOnTopOfPowderSnow, -1)
	// TamableAnimalPanicGoal panics from #panic_environmental_causes only.
	b.panic = NewPanicGoal(1.5)
	b.panic.Causes = panicEnvironmentalCause
	ai.Goals.Add(1, NewFloatGoal(m))
	ai.Goals.Add(1, tamableAnimalPanicGoal{b.panic})
	ai.Goals.Add(2, newSitWhenOrderedToGoal())
	llamas := NewAvoidEntityGoal(func(e world.Entity) bool {
		return e.H().Type() == LlamaType || e.H().Type() == TraderLlamaType
	}, 24, 1.5, 1.5)
	ai.Goals.Add(3, wolfAvoidLlamaGoal{llamas, b})
	ai.Goals.Add(4, newLeapAtTargetGoal(.4))
	ai.Goals.Add(5, NewMeleeAttackGoal(1, true))
	ai.Goals.Add(6, newFollowOwnerGoal(1, 10, 2))
	ai.Goals.Add(7, NewBreedGoal(1))
	ai.Goals.Add(8, NewWaterAvoidingRandomStrollGoal(1))
	ai.Goals.Add(9, &begGoal{goalFlags: goalFlags{GoalLook}, wolf: b})
	ai.Goals.Add(10, NewLookAtPlayerGoal(8))
	ai.Goals.Add(10, NewRandomLookAroundGoal())
	ai.Targets.Add(1, newOwnerHurtByTargetGoal())
	ai.Targets.Add(2, newOwnerHurtTargetGoal())
	hurt := NewHurtByTargetGoal()
	hurt.AlertOthers = true
	ai.Targets.Add(3, hurt)
	angry := NewNearestAttackableTargetGoal(nil, true)
	angry.Selector = func(m *Mob, target world.Entity) bool { return b.angryAt(m, target) }
	ai.Targets.Add(4, angry)
	// Wolf.PREY_SELECTOR: sheep, rabbits and foxes.
	ai.Targets.Add(5, newNonTameRandomTargetGoal(func(e world.Entity) bool {
		switch e.H().Type().EncodeEntity() {
		case "minecraft:sheep", "minecraft:rabbit", "minecraft:fox":
			return true
		}
		return false
	}, false, nil))
	ai.Targets.Add(6, newNonTameRandomTargetGoal(func(e world.Entity) bool { return e.H().Type() == TurtleType }, false, turtleBabyOnLand))
	ai.Targets.Add(7, NewNearestAttackableTypesGoal(abstractSkeletonTypes, false))
	ai.Targets.Add(8, &resetUniversalAngerTargetGoal{lastHurtByPlayer: -1, alertSameType: true})
}

// isAbstractSkeleton reports if an entity extends Java's AbstractSkeleton.
func isAbstractSkeleton(e world.Entity) bool {
	switch e.H().Type().EncodeEntity() {
	case "minecraft:skeleton", "minecraft:stray", "minecraft:wither_skeleton", "minecraft:bogged", "minecraft:parched":
		return true
	}
	return false
}

// turtleBabyOnLand ports Turtle.BABY_ON_LAND_SELECTOR.
func turtleBabyOnLand(m *Mob, e world.Entity) bool {
	t, ok := e.(*Turtle)
	if !ok || t.b().age >= 0 {
		return false
	}
	_, inWater := m.tx.Liquid(cube.PosFromVec3(t.Position()))
	return !inWater
}

// customServerAiStep runs what Wolf.aiStep does after the AI: a wet wolf
// on the ground that does not walk anywhere shakes itself dry, and its
// anger runs out.
func (b *WolfBehaviour) customServerAiStep(m *Mob) {
	if b.wet && !b.shaking && m.AI.nav.IsDone() && m.OnGround() {
		b.shaking, b.shakeAnim = true, 0
		(&Wolf{Ent: m.Ent}).viewAction(WolfShakeAction{})
	}
	b.updatePersistentAnger(m, true)
}

// updateShownState shows viewers the flags of the wolf once a tick when
// they changed, as SynchedEntityData sends what changed at the end of a
// tick.
func (b *WolfBehaviour) updateShownState(w *Wolf) {
	shown := wolfShownState{angry: b.angry(), sitting: b.inSittingPose, interested: b.interested}
	if shown != b.shown {
		b.shown = shown
		w.updateState()
	}
}

// wolfShownState are the flags of a wolf viewers were last shown.
type wolfShownState struct{ angry, sitting, interested bool }

// tickWetness ports the part of Wolf.tick that wets a wolf in water or
// rain and dries it when it shakes for two seconds.
func (b *WolfBehaviour) tickWetness(m *Mob) {
	top := m.data.Pos.Add(mgl64.Vec3{0, m.BBox().Height()})
	if m.InWater() || m.tx.RainingAt(m.BlockPos()) || m.tx.RainingAt(cube.PosFromVec3(top)) {
		b.wet = true
		if b.shaking {
			b.shaking, b.shakeAnim = false, 0
		}
		return
	}
	if !b.shaking {
		return
	}
	if b.shakeAnim == 0 {
		m.tx.EmitGameEvent(world.GameEventEntityAction, m.data.Pos, world.GameEventContext{Source: m.Self})
	}
	old := b.shakeAnim
	b.shakeAnim += .05
	if old >= 2 {
		b.wet, b.shaking, b.shakeAnim = false, false, 0
	}
}

// doHurtTarget ports Mob.doHurtTarget for wolves, which bite for 4 damage.
func (b *WolfBehaviour) doHurtTarget(m *Mob, target world.Entity) bool {
	l, ok := target.(Living)
	if !ok {
		return false
	}
	if _, hurt := l.Hurt(4, AttackDamageSource{Attacker: m.Self}); !hurt {
		return false
	}
	l.KnockBack(m.data.Pos, .4, .4)
	return true
}

// canAttackEntity ports TamableAnimal.canAttack: a pet never attacks its
// owner.
func (b *WolfBehaviour) canAttackEntity(_ *Mob, target world.Entity) bool {
	return !b.ownedBy(target)
}

// alliedTo ports TamableAnimal.considersEntityAsAlly: a tamed wolf is
// allied to its owner.
func (b *WolfBehaviour) alliedTo(_ *Mob, target world.Entity) bool {
	return b.ownedBy(target)
}

// wantsToAttack ports Wolf.wantsToAttack: a wolf leaves creepers, ghasts,
// armour stands, tamed horses and the pets of its owner alone.
func (b *WolfBehaviour) wantsToAttack(_ *Mob, target, owner world.Entity) bool {
	switch target.H().Type().EncodeEntity() {
	case "minecraft:creeper", "minecraft:ghast", "minecraft:armor_stand":
		return false
	}
	if w, ok := target.(*Wolf); ok {
		t := w.behaviour()
		return !t.tamed || owner == nil || t.ownerUUID != owner.H().UUID().String()
	}
	if m := mountFromEntity(target); m != nil && m.b().tamed {
		return false
	}
	if t, ok := target.(interface{ Tamed() bool }); ok && t.Tamed() {
		return false
	}
	return true
}

// wolfAvoidLlamaGoal ports Wolf.WolfAvoidEntityGoal: an untamed wolf flees
// from a llama the stronger the llama is, forgetting its target.
type wolfAvoidLlamaGoal struct {
	*AvoidEntityGoal
	wolf *WolfBehaviour
}

func (g wolfAvoidLlamaGoal) CanUse(m *Mob) bool {
	if !g.AvoidEntityGoal.CanUse(m) || g.wolf.tamed {
		return false
	}
	e, ok := g.toAvoid.Entity(m.tx)
	if !ok {
		return false
	}
	llama := mountFromEntity(e)
	return llama != nil && llama.b().strength >= int(m.Random().NextInt(5))
}

func (g wolfAvoidLlamaGoal) Start(m *Mob) {
	m.SetTarget(nil)
	g.AvoidEntityGoal.Start(m)
}

func (g wolfAvoidLlamaGoal) Tick(m *Mob) {
	m.SetTarget(nil)
	g.AvoidEntityGoal.Tick(m)
}

// leapAtTargetGoal ports LeapAtTargetGoal: a mob on the ground two to four
// blocks from its target now and then leaps at it.
type leapAtTargetGoal struct {
	goalFlags
	yd     float64
	target *world.EntityHandle
}

func newLeapAtTargetGoal(yd float64) *leapAtTargetGoal {
	return &leapAtTargetGoal{goalFlags: goalFlags{GoalJump | GoalMove}, yd: yd}
}

func (g *leapAtTargetGoal) CanUse(m *Mob) bool {
	target := m.Target()
	if target == nil {
		return false
	}
	d := m.DistanceToSqr(target)
	if d < 4 || d > 16 || !m.OnGround() {
		return false
	}
	g.target = target.H()
	return m.Random().NextInt(int32(reducedTickDelay(5))) == 0
}

func (g *leapAtTargetGoal) CanContinueToUse(m *Mob) bool { return !m.OnGround() }

func (g *leapAtTargetGoal) Start(m *Mob) {
	target, ok := g.target.Entity(m.tx)
	if !ok {
		return
	}
	pos := target.Position()
	delta := mgl64.Vec3{pos[0] - m.data.Pos[0], 0, pos[2] - m.data.Pos[2]}
	if delta.LenSqr() > 1e-7 {
		delta = delta.Normalize().Mul(.4).Add(m.data.Vel.Mul(.2))
	}
	m.data.Vel = mgl64.Vec3{delta[0], g.yd, delta[2]}
}

// begGoal ports BegGoal: a wolf tilts its head at a player within eight
// blocks holding a bone or wolf food.
type begGoal struct {
	goalFlags
	wolf     *WolfBehaviour
	player   *world.EntityHandle
	lookTime int
}

func (g *begGoal) interesting(m *Mob, p world.Entity) bool {
	h, ok := p.(interface {
		HeldItems() (item.Stack, item.Stack)
	})
	if !ok {
		return false
	}
	main, off := h.HeldItems()
	w := &Wolf{Ent: m.Ent}
	for _, s := range []item.Stack{main, off} {
		if _, bone := s.Item().(item.Bone); bone || w.breedingFood(s) {
			return true
		}
	}
	return false
}

func (g *begGoal) CanUse(m *Mob) bool {
	c := forNonCombat()
	c.rangeLimit = 8
	p := nearestPlayer(m, c)
	if p == nil {
		return false
	}
	g.player = p.H()
	return g.interesting(m, p)
}

func (g *begGoal) CanContinueToUse(m *Mob) bool {
	p, ok := g.player.Entity(m.tx)
	if !ok || !entityAlive(p) || m.DistanceToSqr(p) > 64 {
		return false
	}
	return g.lookTime > 0 && g.interesting(m, p)
}

func (g *begGoal) Start(m *Mob) {
	g.wolf.interested = true
	g.lookTime = adjustedTickDelay(g, 40+int(m.Random().NextInt(40)))
}

func (g *begGoal) Stop(*Mob) {
	g.wolf.interested = false
	g.player = nil
}

func (g *begGoal) Tick(m *Mob) {
	if p, ok := g.player.Entity(m.tx); ok {
		m.AI.look.SetLookAtWith(p.Position()[0], entityEyeY(p), p.Position()[2], 10, m.AI.MaxHeadPitch)
	}
	g.lookTime--
}
