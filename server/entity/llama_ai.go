package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// llamaState is the state of Llama's caravan, targeting and spitting goals.
type llamaState struct {
	// caravanHead is the llama this one follows and caravanTail the llama
	// following this one, as Llama keeps them.
	caravanHead, caravanTail *world.EntityHandle
	caravanSpeed             float64
	caravanCheck             int
	caravanNav               Navigator
	// target is the Mob target the target goals pick, hurtByTarget whether
	// LlamaHurtByTargetGoal picked it.
	target       *world.EntityHandle
	hurtByTarget bool
	// hurtBy and hurtByStamp are LivingEntity.lastHurtByMob and its
	// timestamp, retaliated against once per hurt.
	hurtBy                     *world.EntityHandle
	hurtByStamp, retaliatedFor int64
	defendStamp                int64
	unseen                     int
	// attackTime and seeTime are the state of RangedAttackGoal.
	attackTime, seeTime int
	rangedNav           Navigator
	didSpit             bool
}

// tickLlamaGoals runs the target goals and the goals that outrank panic for
// llamas, LlamaFollowCaravanGoal and RangedAttackGoal, and reports whether
// one of them moves the llama, with its velocity and rotation.
func (b *MountBehaviour) tickLlamaGoals(m *Mount, tx *world.Tx) (mgl64.Vec3, cube.Rotation, bool) {
	b.updateLlamaTarget(m, tx)
	if b.followCaravan(m, tx) {
		vel, rot, _ := steer(m.Ent, &b.llamaState.caravanNav, groundSpeed(b.movementSpeed, b.llamaState.caravanSpeed), 1)
		return vel, rot, true
	}
	return b.rangedAttack(m, tx)
}

// leashed reports whether the llama is on a lead.
func (m *Mount) leashed() bool { return m.HolderLeashID() != 0 }

// leashedToKnot reports whether the llama is tied to a fence.
func (m *Mount) leashedToKnot(tx *world.Tx) bool {
	id := m.HolderLeashID()
	if id == 0 {
		return false
	}
	for e := range tx.Entities() {
		if e.H().UniqueID() == id {
			return e.H().Type() == LeashKnotType
		}
	}
	return false
}

// llamaMount returns the llama behind a caravan link, or nil if it is gone.
func llamaMount(tx *world.Tx, h *world.EntityHandle) *Mount {
	if h == nil {
		return nil
	}
	e, ok := h.Entity(tx)
	if !ok {
		return nil
	}
	m := mountFromEntity(e)
	if m == nil || !m.b().llama() || m.Dead() {
		return nil
	}
	return m
}

// followCaravan ports LlamaFollowCaravanGoal: a free llama joins the end of
// a caravan within nine blocks, or starts one behind a leashed llama, as
// long as a leashed llama leads it, and walks up to two blocks behind the
// llama ahead of it.
func (b *MountBehaviour) followCaravan(m *Mount, tx *world.Tx) bool {
	s := &b.llamaState
	if s.caravanHead != nil {
		head := llamaMount(tx, s.caravanHead)
		if head == nil || !caravanLeashed(tx, m, 0) {
			b.leaveCaravan(tx)
			return false
		}
		if head.Position().Sub(m.Position()).LenSqr() > 676 {
			if s.caravanSpeed <= 3 {
				s.caravanSpeed *= 1.2
				s.caravanCheck = 40
			} else if s.caravanCheck == 0 {
				b.leaveCaravan(tx)
				return false
			}
		}
		if s.caravanCheck > 0 {
			s.caravanCheck--
		}
		b.walkBehind(m, tx, head)
		return true
	}
	if m.leashed() {
		return false
	}
	var closest *Mount
	best := math.MaxFloat64
	box := LlamaType.BBox(m).Translate(m.Position()).GrowVec3(mgl64.Vec3{9, 4, 9})
	for _, pass := range [2]func(*Mount) bool{
		func(o *Mount) bool { return o.b().llamaState.caravanHead != nil },
		func(o *Mount) bool { return o.leashed() },
	} {
		for e := range tx.EntitiesWithin(box) {
			o := mountFromEntity(e)
			if o == nil || e.H() == m.H() || !o.b().llama() || o.b().llamaState.caravanTail != nil || !pass(o) {
				continue
			}
			if d := m.Position().Sub(o.Position()).LenSqr(); d <= best {
				closest, best = o, d
			}
		}
		if closest != nil {
			break
		}
	}
	if closest == nil || best < 4 || !closest.leashed() && !caravanLeashed(tx, closest, 1) {
		return false
	}
	s.caravanHead, s.caravanSpeed, s.caravanCheck = closest.H(), 2.1, 0
	closest.b().llamaState.caravanTail = m.H()
	b.walkBehind(m, tx, closest)
	return true
}

// walkBehind ports LlamaFollowCaravanGoal.tick: the llama heads for a spot
// two blocks short of the llama it follows, unless tied to a fence.
func (b *MountBehaviour) walkBehind(m *Mount, tx *world.Tx, head *Mount) {
	if m.leashedToKnot(tx) {
		return
	}
	d := head.Position().Sub(m.Position())
	if d.Len() == 0 {
		return
	}
	target := m.Position().Add(d.Normalize().Mul(max(d.Len()-2, 0)))
	navigateTo(&b.llamaState.caravanNav, m.Ent, tx, target)
}

// leaveCaravan ports Llama.leaveCaravan.
func (b *MountBehaviour) leaveCaravan(tx *world.Tx) {
	s := &b.llamaState
	if head := llamaMount(tx, s.caravanHead); head != nil {
		head.b().llamaState.caravanTail = nil
	}
	s.caravanHead, s.caravanSpeed, s.caravanNav = nil, 2.1, Navigator{}
}

// caravanLeashed ports LlamaFollowCaravanGoal.firstIsLeashed: whether a
// leashed llama leads the caravan of m within eight links.
func caravanLeashed(tx *world.Tx, m *Mount, depth int) bool {
	for ; depth <= 8; depth++ {
		head := llamaMount(tx, m.b().llamaState.caravanHead)
		if head == nil {
			return false
		}
		if head.leashed() {
			return true
		}
		m = head
	}
	return false
}

// llamaHurt records the attacker of a llama, which LlamaHurtByTargetGoal
// turns on.
func (b *MountBehaviour) llamaHurt(src world.DamageSource, tick int64) {
	if attacker := mobKiller(src); attacker != nil {
		if _, living := attacker.(Living); living {
			b.llamaState.hurtBy, b.llamaState.hurtByStamp = attacker.H(), tick
		}
	}
}

// updateLlamaTarget runs the target goals of llamas: LlamaHurtByTargetGoal,
// TraderLlamaDefendWanderingTraderGoal, the trader llama's
// NearestAttackableTargetGoals for zombies and illagers and
// LlamaAttackWolfGoal.
func (b *MountBehaviour) updateLlamaTarget(m *Mount, tx *world.Tx) {
	s := &b.llamaState
	if target := b.currentTarget(m, tx); target != nil {
		// LlamaHurtByTargetGoal gives up once the llama spat.
		if !(s.hurtByTarget && s.didSpit) {
			return
		}
	}
	s.target, s.hurtByTarget, s.didSpit, s.unseen = nil, false, false, 0
	if s.hurtBy != nil && s.hurtByStamp != s.retaliatedFor {
		s.retaliatedFor = s.hurtByStamp
		if attacker, ok := s.hurtBy.Entity(tx); ok && combatTargetable(attacker) {
			s.target, s.hurtByTarget = attacker.H(), true
			return
		}
	}
	if b.kind == mountTraderLlama {
		if trader, ok := leashedTrader(m.Ent, tx); ok {
			if attacker, stamp, ok := trader.lastAttacker(tx); ok && stamp != s.defendStamp && combatTargetable(attacker) {
				s.defendStamp, s.target = stamp, attacker.H()
				return
			}
		}
	}
	if b.kind == mountTraderLlama && rand.IntN(10) == 0 {
		if target := nearestLlamaTarget(m, tx, 16, true, func(id string, _ world.Entity) bool {
			return zombieClass[id] && id != "minecraft:zombified_piglin" || id == "minecraft:pillager" || id == "minecraft:vindicator" || id == "minecraft:evoker" || id == "minecraft:illusioner"
		}); target != nil {
			s.target = target.H()
			return
		}
	}
	if rand.IntN(16) == 0 {
		// LlamaAttackWolfGoal looks for untamed wolves within a quarter of
		// the follow range.
		if target := nearestLlamaTarget(m, tx, 4, false, func(id string, e world.Entity) bool {
			w, ok := e.(*Wolf)
			return ok && !w.Tamed()
		}); target != nil {
			s.target = target.H()
		}
	}
}

// currentTarget ports TargetGoal.canContinueToUse: the target stays while it
// lives within the follow range of 16 blocks and was seen in the last three
// seconds.
func (b *MountBehaviour) currentTarget(m *Mount, tx *world.Tx) world.Entity {
	s := &b.llamaState
	if s.target == nil {
		return nil
	}
	target, ok := s.target.Entity(tx)
	if !ok || !combatTargetable(target) || target.Position().Sub(m.Position()).LenSqr() > 16*16 {
		return nil
	}
	if lineVisible(m.Ent, tx, target) {
		s.unseen = 0
	} else if s.unseen++; s.unseen > 60 {
		return nil
	}
	return target
}

// combatTargetable ports TargetingConditions.forCombat's rule on targets:
// living and, for players, in a game mode that takes damage.
func combatTargetable(e world.Entity) bool {
	if l, ok := e.(Living); ok && l.Dead() {
		return false
	}
	g, ok := e.(interface{ GameMode() world.GameMode })
	return !ok || g.GameMode().AllowsTakingDamage()
}

// nearestLlamaTarget ports NearestAttackableTargetGoal.findTarget.
func nearestLlamaTarget(m *Mount, tx *world.Tx, r float64, mustSee bool, match func(string, world.Entity) bool) world.Entity {
	var target world.Entity
	best := math.MaxFloat64
	for e := range tx.EntitiesWithin(m.H().Type().BBox(m).Translate(m.Position()).GrowVec3(mgl64.Vec3{r, 4, r})) {
		if e.H() == m.H() || !match(e.H().Type().EncodeEntity(), e) || !combatTargetable(e) {
			continue
		}
		d := e.Position().Sub(m.Position()).LenSqr()
		if d > r*r || d >= best || mustSee && !lineVisible(m.Ent, tx, e) {
			continue
		}
		target, best = e, d
	}
	return target
}

// rangedAttack ports RangedAttackGoal(1.25, 40, 20): the llama walks up to its
// target until it sees it within 20 blocks and spits at it every two seconds.
func (b *MountBehaviour) rangedAttack(m *Mount, tx *world.Tx) (mgl64.Vec3, cube.Rotation, bool) {
	s := &b.llamaState
	target := b.currentTarget(m, tx)
	if target == nil {
		s.attackTime, s.seeTime, s.rangedNav = -1, 0, Navigator{}
		return mgl64.Vec3{}, cube.Rotation{}, false
	}
	distSqr := target.Position().Sub(m.Position()).LenSqr()
	seen := lineVisible(m.Ent, tx, target)
	if seen {
		s.seeTime++
	} else {
		s.seeTime = 0
	}
	vel, rot := m.data.Vel, lookRotation(m.Ent, EyePosition(target))
	if distSqr > 20*20 || s.seeTime < 5 {
		navigateTo(&s.rangedNav, m.Ent, tx, target.Position())
		var moving bool
		if vel, rot, moving = steer(m.Ent, &s.rangedNav, groundSpeed(b.movementSpeed, 1.25), 1); !moving {
			vel = m.data.Vel
		}
	} else {
		s.rangedNav = Navigator{}
	}
	if s.attackTime--; s.attackTime == 0 {
		if seen {
			m.spit(tx, target)
			s.attackTime = 40
		}
	} else if s.attackTime < 0 {
		// The attack interval of llamas is 40 ticks at any distance.
		s.attackTime = 40
	}
	return vel, rot, true
}

// spit ports Llama.spit: a llama spit flies from in front of the llama at a
// third of the target's height with an uncertainty of 10.
func (m *Mount) spit(tx *world.Tx, target world.Entity) {
	yaw := m.Rotation().Yaw() * math.Pi / 180
	width := m.H().Type().BBox(m).Width()
	origin := mgl64.Vec3{m.Position()[0] - (width+1)*.5*math.Sin(yaw), EyePosition(m)[1] - .1, m.Position()[2] + (width+1)*.5*math.Cos(yaw)}
	box := target.H().Type().BBox(target)
	d := mgl64.Vec3{target.Position()[0] - m.Position()[0], target.Position()[1] + box.Height()/3 - origin[1], target.Position()[2] - m.Position()[2]}
	d[1] += math.Hypot(d[0], d[2]) * .2
	if d.Len() == 0 {
		return
	}
	tx.AddEntity(NewLlamaSpit(world.EntitySpawnOpts{Position: origin, Velocity: shootVelocity(d, 1.5, 10)}, m))
	tx.PlaySound(m.Position(), sound.NativeEvent{SoundType: "spit", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
	m.b().llamaState.didSpit = true
}

// shootVelocity ports Projectile.getMovementToShoot: the direction, spread
// by a triangular distribution growing with the uncertainty, at the power
// passed.
func shootVelocity(d mgl64.Vec3, power, uncertainty float64) mgl64.Vec3 {
	spread := .0172275 * uncertainty
	triangle := func() float64 { return spread * (rand.Float64() - rand.Float64()) }
	return d.Normalize().Add(mgl64.Vec3{triangle(), triangle(), triangle()}).Mul(power)
}
