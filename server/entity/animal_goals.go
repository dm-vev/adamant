package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// animalGoals ports the goals Java's land animals share, run in the order of
// priority of their goal selectors: PanicGoal, BreedGoal, TemptGoal,
// FollowParentGoal, WaterAvoidingRandomStrollGoal, LookAtPlayerGoal and
// RandomLookAroundGoal.
type animalGoals struct {
	panic     panicState
	navigator Navigator
	tempt     temptGoal
	parent    followParentGoal
	look      lookGoal
	// noActionTime is Mob.noActionTime: how long no player came within 32
	// blocks. Animals left alone that long stop strolling.
	noActionTime int
	// running is the goal that owns the navigator.
	running animalGoal
}

// animalGoal is a goal moving an animal.
type animalGoal uint8

const (
	goalIdle animalGoal = iota
	goalPanic
	goalBreed
	goalTempt
	goalParent
	goalSpecies
	goalStroll
)

// speciesGoal is a goal of a single species that ranks between following a
// parent and strolling, like the sheep's EatBlockGoal. The animal stands
// still while it runs.
type speciesGoal interface {
	// run starts or continues the goal and reports whether it runs.
	run(e *Ent, tx *world.Tx) bool
	// stop ends the goal when a goal of higher priority runs.
	stop()
}

// animalGoalSpec configures the goals of a species.
type animalGoalSpec struct {
	// speed is the Java movement speed attribute.
	speed float64
	// panic, breed, tempt, parent and stroll are the speed modifiers of the
	// goals. A goal with a zero modifier is left out.
	panic, breed, tempt, parent, stroll float64
	// tempting reports whether a held item tempts the animal.
	tempting func(item.Stack) bool
	// strollInterval is RandomStrollGoal.interval, 120 if zero.
	strollInterval int
	// lookDistance is the range of LookAtPlayerGoal, 6 if zero.
	lookDistance float64
	// reach is how close the animal gets to a path node before it moves on.
	reach float64
	// species is the goal of the species ranked above strolling, if any.
	species speciesGoal
}

// hurt records damage for the panic and, as LivingEntity.hurtServer does,
// resets the time without action.
func (g *animalGoals) hurt(src world.DamageSource, tick int64) {
	g.panic.hurt(src, tick)
	g.noActionTime = 0
}

// tick runs the goals of animal a and returns its horizontal velocity and
// rotation for the tick.
func (g *animalGoals) tick(e *Ent, tx *world.Tx, a breeder, s animalGoalSpec) (mgl64.Vec3, cube.Rotation) {
	g.updateNoActionTime(e, tx)
	goal, modifier := goalIdle, 0.0
	var face mgl64.Vec3
	facing := false
	if s.panic > 0 && g.panic.run(e, tx, &g.navigator) {
		// The panic path replaced the path of any goal that ran before.
		goal, modifier, g.running = goalPanic, s.panic, goalPanic
	}
	partner, breeding := tickBreeding(a, tx)
	if goal == goalIdle && breeding {
		goal, modifier = goalBreed, s.breed
		g.own(goalBreed)
		navigateToPartner(&g.navigator, e, tx, partner)
		face, facing = partner, true
	}
	if goal == goalIdle && s.tempt > 0 {
		if p, ok := g.tempt.next(e, tx, s.tempting, false); ok {
			goal, modifier = goalTempt, s.tempt
			g.own(goalTempt)
			g.tempt.follow(e, tx, &g.navigator, p)
			face, facing = EyePosition(p), true
		}
	} else {
		g.tempt.stop()
	}
	if goal == goalIdle && s.parent > 0 {
		if parent, ok := g.parent.next(e, tx, a); ok {
			goal, modifier = goalParent, s.parent
			g.own(goalParent)
			g.parent.follow(e, tx, &g.navigator, parent)
		}
	} else {
		g.parent.stop()
	}
	if goal == goalIdle && s.species != nil {
		if s.species.run(e, tx) {
			goal = goalSpecies
			g.own(goalSpecies)
			g.look.stop()
		}
	} else if s.species != nil {
		s.species.stop()
	}
	if goal == goalIdle && s.stroll > 0 {
		if g.strolling(e, tx, s) {
			goal, modifier = goalStroll, s.stroll
		}
	}
	if goal == goalIdle {
		g.own(goalIdle)
	}
	vel, rot, moving := steer(e, &g.navigator, groundSpeed(s.speed, modifier), max(s.reach, .6))
	if moving {
		rot[1] = 0
		g.look.stop()
		return vel, rot
	}
	// An idle animal does not push itself: knockback and the like slow down
	// with friction.
	vel = e.data.Vel
	if !facing && goal != goalSpecies {
		face, facing = g.look.next(e, tx, s.lookDistance)
	}
	if facing {
		rot = lookRotation(e, face)
	}
	return vel, rot
}

// own hands the navigator to goal, dropping the path of the goal that had
// it, as Java's goals stop navigating when they stop.
func (g *animalGoals) own(goal animalGoal) {
	if g.running != goal {
		g.navigator.Path, g.navigator.Index, g.navigator.HasTarget = nil, 0, false
		g.running = goal
	}
}

// updateNoActionTime ports the part of Mob.checkDespawn that resets
// noActionTime while a player is within 32 blocks.
func (g *animalGoals) updateNoActionTime(e *Ent, tx *world.Tx) {
	for p := range tx.Players() {
		if p.Position().Sub(e.Position()).LenSqr() < 32*32 {
			g.noActionTime = 0
			return
		}
	}
	g.noActionTime++
}

// strolling ports WaterAvoidingRandomStrollGoal: an animal that is not left
// alone sets off, on average once per interval, to a random spot within ten
// blocks, preferring land, and strolls until it arrives.
func (g *animalGoals) strolling(e *Ent, tx *world.Tx, s animalGoalSpec) bool {
	if g.running == goalStroll && g.navigator.Index < len(g.navigator.Path) {
		return true
	}
	interval := s.strollInterval
	if interval == 0 {
		interval = 120
	}
	if g.noActionTime >= 100 || rand.IntN(interval) != 0 {
		return false
	}
	m := newRandomPosMob(e, tx)
	var goal cube.Pos
	ok := false
	if _, inWater := tx.Liquid(cube.PosFromVec3(e.Position())); inWater {
		goal, ok = m.randomPos(15, 7)
	} else {
		goal, ok = m.randomPos(10, 7)
	}
	if !ok {
		return false
	}
	g.own(goalStroll)
	return g.navigator.MoveTo(tx, cube.PosFromVec3(e.Position()), goal, m.profile, navigatorTick(e))
}

// temptRange is the default of the tempt range attribute.
const temptRange = 10

// temptGoal ports TemptGoal: the animal follows the nearest player within
// ten blocks holding something it is tempted by, stops 2.5 blocks away from
// them and, once it loses interest, ignores temptation for 100 ticks.
type temptGoal struct {
	player   *world.EntityHandle
	calmDown int
	// pos and rot are where the player stood and looked, which a mob that
	// can be scared watches.
	pos mgl64.Vec3
	rot cube.Rotation
}

// next ports TemptGoal.canUse and canContinueToUse and returns the player the
// animal follows. A mob that can be scared, like an ocelot, gives up when the
// player moves or turns within six blocks of it.
func (g *temptGoal) next(e *Ent, tx *world.Tx, tempting func(item.Stack) bool, scare bool) (world.Entity, bool) {
	if g.player != nil && scare {
		if p, ok := g.player.Entity(tx); ok && !g.calm(e, p) {
			g.stop()
			return nil, false
		}
	}
	if g.calmDown > 0 {
		g.calmDown--
		return nil, false
	}
	p := temptingPlayer(e, tx, tempting)
	if p == nil {
		g.stop()
		return nil, false
	}
	if g.player == nil {
		g.pos, g.rot = p.Position(), p.Rotation()
	}
	g.player = p.H()
	return p, true
}

// calm reports whether a player tempting a mob that can be scared kept still.
func (g *temptGoal) calm(e *Ent, p world.Entity) bool {
	if e.Position().Sub(p.Position()).LenSqr() < 36 {
		if p.Position().Sub(g.pos).LenSqr() > .01 {
			return false
		}
		if math.Abs(p.Rotation().Pitch()-g.rot.Pitch()) > 5 || math.Abs(p.Rotation().Yaw()-g.rot.Yaw()) > 5 {
			return false
		}
	} else {
		g.pos = p.Position()
	}
	g.rot = p.Rotation()
	return true
}

// stop ports TemptGoal.stop: a tempted animal calms down for 100 ticks.
func (g *temptGoal) stop() {
	if g.player != nil {
		g.player, g.calmDown = nil, 100
	}
}

// follow walks the animal to the player until it is 2.5 blocks away.
func (g *temptGoal) follow(e *Ent, tx *world.Tx, nav *Navigator, p world.Entity) {
	if e.Position().Sub(p.Position()).LenSqr() < 2.5*2.5 {
		nav.Path, nav.Index, nav.HasTarget = nil, 0, false
		return
	}
	navigateTo(nav, e, tx, p.Position())
}

// temptingPlayer returns the nearest player in range of e holding an item
// that tempts it.
func temptingPlayer(e *Ent, tx *world.Tx, tempting func(item.Stack) bool) world.Entity {
	var nearest world.Entity
	best := math.MaxFloat64
	for p := range tx.Players() {
		if !targetablePlayer(p) {
			continue
		}
		held, ok := p.(item.User)
		if !ok {
			continue
		}
		main, off := held.HeldItems()
		if !tempting(main) && !tempting(off) {
			continue
		}
		d := p.Position().Sub(e.Position()).LenSqr()
		r := max(temptRange*visibilityPercent(p), 2)
		if d <= r*r && d < best {
			nearest, best = p, d
		}
	}
	return nearest
}

// targetablePlayer ports Entity.canBeSeenByAnyone: living players outside
// spectator mode.
func targetablePlayer(p world.Entity) bool {
	if l, ok := p.(Living); ok && l.Dead() {
		return false
	}
	g, ok := p.(interface{ GameMode() world.GameMode })
	return !ok || g.GameMode().HasCollision()
}

// visibilityPercent ports LivingEntity.getVisibilityPercent for an observer
// without a matching mob head: sneaking hides 20% of the range, and
// invisibility all but what worn armour gives away.
func visibilityPercent(target world.Entity) float64 {
	v := 1.0
	if s, ok := target.(interface{ Sneaking() bool }); ok && s.Sneaking() {
		v *= .8
	}
	if i, ok := target.(interface{ Invisible() bool }); ok && i.Invisible() {
		cover := float32(0)
		if a, ok := target.(interface{ Armour() *inventory.Armour }); ok {
			for _, s := range a.Armour().Slots() {
				if !s.Empty() {
					cover += .25
				}
			}
		}
		v *= .7 * float64(max(cover, .1))
	}
	return v
}

// followParentGoal ports FollowParentGoal: a baby walks to the nearest adult
// of its kind within 8 blocks once it is more than 3 blocks away and stops
// following it beyond 16 blocks.
type followParentGoal struct {
	parent           *world.EntityHandle
	timeToRecalcPath int
}

// next ports FollowParentGoal.canUse and canContinueToUse.
func (g *followParentGoal) next(e *Ent, tx *world.Tx, a breeder) (world.Entity, bool) {
	if *a.ageTicks() >= 0 {
		g.stop()
		return nil, false
	}
	if g.parent != nil {
		if parent, ok := g.parent.Entity(tx); ok {
			if l, living := parent.(Living); !living || !l.Dead() {
				if d := e.Position().Sub(parent.Position()).LenSqr(); d >= 9 && d <= 256 {
					return parent, true
				}
			}
		}
		g.stop()
		return nil, false
	}
	box := e.H().Type().BBox(e).Translate(e.Position()).GrowVec3(mgl64.Vec3{8, 4, 8})
	var closest world.Entity
	best := math.MaxFloat64
	for other := range tx.EntitiesWithin(box) {
		adult, ok := other.(breeder)
		if !ok || other.H().Type() != e.H().Type() || *adult.ageTicks() < 0 {
			continue
		}
		if d := e.Position().Sub(other.Position()).LenSqr(); d <= best {
			closest, best = other, d
		}
	}
	if closest == nil || best < 9 {
		return nil, false
	}
	g.parent, g.timeToRecalcPath = closest.H(), 0
	return closest, true
}

// follow walks the baby to its parent, routing anew every 10 ticks.
func (g *followParentGoal) follow(e *Ent, tx *world.Tx, nav *Navigator, parent world.Entity) {
	if g.timeToRecalcPath--; g.timeToRecalcPath <= 0 {
		g.timeToRecalcPath = 10
		nav.Path, nav.Index, nav.HasTarget = nil, 0, false
		navigateTo(nav, e, tx, parent.Position())
	}
}

func (g *followParentGoal) stop() { g.parent = nil }

// lookGoal ports LookAtPlayerGoal and RandomLookAroundGoal: an idle animal
// now and then looks at the nearest player within range for 2 to 4 seconds,
// or otherwise in a random direction for 1 to 2 seconds.
type lookGoal struct {
	player *world.EntityHandle
	// direction is the random direction looked in for ticks ticks.
	direction mgl64.Vec3
	ticks     int
}

// next returns the position an idle animal looks at.
func (g *lookGoal) next(e *Ent, tx *world.Tx, distance float64) (mgl64.Vec3, bool) {
	if distance == 0 {
		distance = 6
	}
	if g.ticks > 0 {
		g.ticks--
		if g.player == nil {
			return EyePosition(e).Add(g.direction), true
		}
		if p, ok := g.player.Entity(tx); ok && targetablePlayer(p) && p.Position().Sub(e.Position()).LenSqr() <= distance*distance {
			return EyePosition(p), true
		}
	}
	g.stop()
	if rand.Float32() < .02 {
		if p := lookTargetPlayer(e, tx, distance); p != nil {
			g.player, g.ticks = p.H(), 40+rand.IntN(40)
			return EyePosition(p), true
		}
	}
	if rand.Float32() < .02 {
		angle := math.Pi * 2 * rand.Float64()
		g.direction, g.ticks = mgl64.Vec3{math.Cos(angle), 0, math.Sin(angle)}, 20+rand.IntN(20)
		return EyePosition(e).Add(g.direction), true
	}
	return mgl64.Vec3{}, false
}

func (g *lookGoal) stop() { g.player, g.ticks = nil, 0 }

// lookTargetPlayer returns the nearest player within distance that does not
// ride the animal.
func lookTargetPlayer(e *Ent, tx *world.Tx, distance float64) world.Entity {
	var nearest world.Entity
	best := math.MaxFloat64
	for p := range tx.Players() {
		if !targetablePlayer(p) {
			continue
		}
		if r, ok := p.(Rider); ok && r.Riding() == e.H() {
			continue
		}
		d := p.Position().Sub(e.Position()).LenSqr()
		r := max(distance*visibilityPercent(p), 2)
		if d <= r*r && d < best {
			nearest, best = p, d
		}
	}
	return nearest
}

// lookRotation returns the rotation of e looking at target from its eyes.
func lookRotation(e *Ent, target mgl64.Vec3) cube.Rotation {
	d := target.Sub(EyePosition(e))
	horizontal := math.Hypot(d[0], d[2])
	if horizontal == 0 && d[1] == 0 {
		return e.data.Rot
	}
	return cube.Rotation{math.Atan2(-d[0], d[2]) * 180 / math.Pi, -math.Atan2(d[1], horizontal) * 180 / math.Pi}
}

// navigateTo routes nav from e to target.
func navigateTo(nav *Navigator, e *Ent, tx *world.Tx, target mgl64.Vec3) {
	box := e.H().Type().BBox(e)
	nav.MoveTo(tx, cube.PosFromVec3(e.Position()), cube.PosFromVec3(target), GroundProfile{MaxVisitedNodes: 128, Width: box.Width(), Height: box.Height()}, navigatorTick(e))
}
