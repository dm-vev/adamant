package entity

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/gamedata"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/recipe"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

const (
	// babyStartAge is AgeableMob.BABY_START_AGE: babies grow up after 20
	// minutes.
	babyStartAge = -24000
	// parentAgeAfterBreeding is Animal.PARENT_AGE_AFTER_BREEDING: parents
	// cannot fall in love again for 5 minutes.
	parentAgeAfterBreeding = 6000
	// loveDuration is how long an animal stays in love after being fed.
	loveDuration = 600
)

// breeding is the love and growth state Java's Animal and AgeableMob keep.
// The age itself is kept by the species, as negative ticks until a baby grows
// up or positive ticks until a parent may breed again.
type breeding struct {
	inLove, forcedAge, forcedAgeTimer int
	loveCause                         uuid.UUID
	// ageLocked keeps a baby from growing up, as a golden dandelion does.
	ageLocked    bool
	ageLockTimer int
	// partner and loveTime are the state of the running BreedGoal.
	partner  *world.EntityHandle
	loveTime int
}

// breeder is a mob that breeds like a Java Animal.
type breeder interface {
	Living
	// InLove reports whether the animal is in love, looking for a partner.
	InLove() bool
	currentTx() *world.Tx
	breedingState() *breeding
	// ageTicks returns the species' age field.
	ageTicks() *int
	// breedingFood reports whether the stack is in the species' food tag.
	breedingFood(stack item.Stack) bool
	// breedOffspring returns the baby of the breeder and its partner, ported
	// from getBreedOffspring, or nil if none is born.
	breedOffspring(tx *world.Tx, partner breeder) *world.EntityHandle
}

// mateChecker is a breeder with conditions beyond both partners being of the
// same type and in love, such as tamed cats and wolves.
type mateChecker interface {
	canMateWith(partner breeder) bool
}

// childSpawner is a breeder that replaces spawning the offspring entity, as
// the sniffer laying an egg does.
type childSpawner interface {
	spawnChildFromBreeding(tx *world.Tx, partner breeder)
}

// InLove reports whether the animal is in love, looking for a partner.
func (b *breeding) InLove() bool { return b.inLove > 0 }

// feedBreeder ports Animal.mobInteract and AgeableMob.mobInteract: food puts
// an adult that can fall in love in love when a player feeds it and lets a
// growing baby grow up a tenth of its remaining time faster, and a golden
// dandelion pauses or resumes the growth of a baby. The item is used up.
func feedBreeder(a breeder, user item.User, ctx *item.UseContext) bool {
	held, _ := user.HeldItems()
	if ctx == nil || held.Empty() {
		return false
	}
	b, age := a.breedingState(), a.ageTicks()
	if a.breedingFood(held) {
		player, isPlayer := user.(interface{ UUID() uuid.UUID })
		isPlayer = isPlayer && user.H() != nil && user.H().Type().EncodeEntity() == "minecraft:player"
		switch {
		case *age == 0 && b.inLove <= 0 && isPlayer && fallsInLove(a):
			ctx.SubtractFromCount(1)
			b.inLove, b.loveCause = loveDuration, player.UUID()
			viewBreederAction(a, LoveHeartsAction{})
			viewBreederState(a)
			return true
		case *age < 0 && !b.ageLocked:
			ctx.SubtractFromCount(1)
			ageUp(a, int(float32(-*age/20)*0.1), true)
			return true
		}
	}
	if itemNamed(held, "minecraft:golden_dandelion") && *age < 0 && b.ageLockTimer == 0 {
		// AgeableMob.setAgeLockedData: the baby starts growing anew.
		b.ageLocked, b.ageLockTimer = !b.ageLocked, 40
		setAge(a, babyStartAge)
		ctx.SubtractFromCount(1)
		a.currentTx().PlaySound(a.Position(), sound.MobGrowth{Paused: b.ageLocked})
		return true
	}
	return false
}

// fallsInLove ports Animal.canFallInLove beyond not being in love yet, which
// species like hoglins restrict further.
func fallsInLove(a breeder) bool {
	c, ok := a.(interface{ canFallInLove() bool })
	return !ok || c.canFallInLove()
}

// ageUp ports AgeableMob.ageUp: the age moves the seconds passed towards
// adulthood. Forced growth from feeding is remembered and becomes the
// cooldown of the grown-up animal.
func ageUp(a breeder, seconds int, forced bool) {
	b, age := a.breedingState(), a.ageTicks()
	old := *age
	setAge(a, min(0, *age+seconds*20))
	if forced {
		b.forcedAge += *age - old
		if b.forcedAgeTimer == 0 {
			b.forcedAgeTimer = 40
		}
	}
	if *age == 0 {
		setAge(a, b.forcedAge)
	}
}

// setAge sets the age of the breeder, updating viewers when it grows up or
// becomes a baby.
func setAge(a breeder, v int) {
	age := a.ageTicks()
	wasBaby := *age < 0
	*age = v
	if wasBaby != (v < 0) {
		viewBreederState(a)
	}
}

// tickBreeding ports the ageing of AgeableMob.aiStep, the love countdown of
// Animal.aiStep and BreedGoal. It returns the partner to walk to while the
// breeder is looking to mate.
func tickBreeding(a breeder, tx *world.Tx) (mgl64.Vec3, bool) {
	b, age := a.breedingState(), a.ageTicks()
	if b.forcedAgeTimer > 0 {
		if b.forcedAgeTimer%4 == 0 {
			tx.AddParticle(a.Position().Add(mgl64.Vec3{0, 0.5}), particle.BoneMeal{})
		}
		b.forcedAgeTimer--
	}
	if b.ageLockTimer > 0 {
		if b.ageLockTimer%2 == 0 {
			tx.AddParticle(a.Position().Add(mgl64.Vec3{0, a.H().Type().BBox(a).Height() + 0.2}), particle.MobGrowth{Paused: b.ageLocked})
		}
		b.ageLockTimer--
	}
	if !a.Dead() {
		if *age < 0 && !b.ageLocked {
			setAge(a, *age+1)
		} else if *age > 0 {
			setAge(a, *age-1)
		}
	}
	if *age != 0 {
		b.inLove = 0
	}
	if b.inLove > 0 {
		if b.inLove--; b.inLove == 0 {
			viewBreederState(a)
		}
	}
	if panicking(a) {
		// PanicGoal outranks BreedGoal, which stops and forgets its partner.
		b.partner, b.loveTime = nil, 0
		return mgl64.Vec3{}, false
	}
	return breedGoal(a, tx)
}

// resetLove ends the love mode of an animal, as being hurt does.
func resetLove(a breeder) {
	b := a.breedingState()
	if b.inLove > 0 {
		b.inLove = 0
		viewBreederState(a)
	}
}

// breedGoal ports BreedGoal: an animal in love looks for the nearest partner
// in love within 8 blocks, walks to it, and breeds once it spent 3 seconds
// doing so and is within 3 blocks.
func breedGoal(a breeder, tx *world.Tx) (mgl64.Vec3, bool) {
	b := a.breedingState()
	// Sitting pets and camels, and rolling pandas, keep their move flag busy,
	// so the goal cannot run.
	if s, ok := a.(interface{ moveFlagHeld() bool }); ok && s.moveFlagHeld() {
		b.partner, b.loveTime = nil, 0
		return mgl64.Vec3{}, false
	}
	partner := b.goalPartner(tx)
	if partner != nil && (!partner.InLove() || b.loveTime >= 60 || panicking(partner)) {
		b.partner, b.loveTime, partner = nil, 0, nil
	}
	if partner == nil {
		if !b.InLove() {
			return mgl64.Vec3{}, false
		}
		if partner = freePartner(a, tx); partner == nil {
			return mgl64.Vec3{}, false
		}
		if s, ok := a.(breedGoalStarter); ok && !s.canStartBreeding(tx) {
			return mgl64.Vec3{}, false
		}
		b.partner = partner.H()
	}
	b.loveTime++
	if b.loveTime >= 60 && a.Position().Sub(partner.Position()).LenSqr() < 9 {
		breed(a, partner, tx)
		b.partner, b.loveTime = nil, 0
		return mgl64.Vec3{}, false
	}
	return partner.Position(), true
}

// breedGoalStarter is a breeder whose BreedGoal has conditions to start
// beyond finding a partner, like pandas that need bamboo nearby.
type breedGoalStarter interface {
	canStartBreeding(tx *world.Tx) bool
}

// goalPartner returns the loaded, living partner of the running BreedGoal.
func (b *breeding) goalPartner(tx *world.Tx) breeder {
	if b.partner == nil {
		return nil
	}
	e, ok := b.partner.Entity(tx)
	if !ok {
		return nil
	}
	p, ok := e.(breeder)
	if !ok || p.Dead() {
		return nil
	}
	return p
}

// freePartner ports BreedGoal.getFreePartner: the nearest animal that the
// breeder can mate with and is not panicking.
func freePartner(a breeder, tx *world.Tx) breeder {
	box := a.H().Type().BBox(a).Translate(a.Position()).Grow(8)
	var best breeder
	bestDist := 0.0
	for e := range tx.EntitiesWithin(box.Grow(2)) {
		p, ok := e.(breeder)
		if !ok || !canMate(a, p) || panicking(p) || !p.H().Type().BBox(p).Translate(p.Position()).IntersectsWith(box) {
			continue
		}
		if d := a.Position().Sub(p.Position()).LenSqr(); best == nil || d < bestDist {
			best, bestDist = p, d
		}
	}
	return best
}

// canMate ports Animal.canMate: two different animals of the same type that
// are both in love, plus any conditions of the species. Species that override
// canMate entirely, like horses mating with donkeys, decide themselves.
func canMate(a, partner breeder) bool {
	if partner.H() == a.H() || partner.Dead() {
		return false
	}
	if m, ok := a.(crossBreeder); ok {
		return m.matesWith(partner)
	}
	if partner.H().Type() != a.H().Type() {
		return false
	}
	if c, ok := a.(mateChecker); ok && !c.canMateWith(partner) {
		return false
	}
	return a.breedingState().InLove() && partner.breedingState().InLove()
}

// crossBreeder is a breeder that picks its partners itself, like horses
// that mate with donkeys.
type crossBreeder interface {
	matesWith(partner breeder) bool
}

// panicking reports whether a breeder is fleeing from danger.
func panicking(a breeder) bool {
	p, ok := a.(interface{ Panicking() bool })
	return ok && p.Panicking()
}

// breed ports Animal.spawnChildFromBreeding: the baby appears at the
// breeder, after which both parents rest.
func breed(a, partner breeder, tx *world.Tx) {
	if s, ok := a.(childSpawner); ok {
		s.spawnChildFromBreeding(tx, partner)
		return
	}
	child := a.breedOffspring(tx, partner)
	finishBreeding(a, partner, tx)
	if child != nil {
		tx.AddEntity(child)
	}
}

// finishBreeding ports Animal.finalizeSpawnChildFromBreeding: both parents
// stop being in love and cannot breed for 5 minutes, and an experience orb
// worth 1 to 7 appears while mobs drop loot.
func finishBreeding(a, partner breeder, tx *world.Tx) {
	setAge(a, parentAgeAfterBreeding)
	setAge(partner, parentAgeAfterBreeding)
	resetLove(a)
	resetLove(partner)
	viewBreederAction(a, LoveHeartsAction{})
	if tx.World().GameRuleBool("mob_drops") {
		for _, orb := range NewExperienceOrbs(a.Position(), 1+rand.IntN(7)) {
			tx.AddEntity(orb)
		}
	}
}

// babyAt returns spawn options for a baby born at the breeder, facing south
// as Java snaps it to a zero rotation. Like EntityType.create for breeding,
// the baby skips the spawn initialisation of natural spawns.
func babyAt(a breeder) world.EntitySpawnOpts {
	return world.EntitySpawnOpts{Position: a.Position(), SpawnReason: "BREEDING", SpawnFinalized: true}
}

// randomParent returns either of the two values with equal chance.
func randomParent[T any](a, b T) T {
	if rand.IntN(2) == 0 {
		return a
	}
	return b
}

// navigateToPartner keeps the navigator routed to the partner the breed goal
// walks to.
func navigateToPartner(n *Navigator, e *Ent, tx *world.Tx, target mgl64.Vec3) {
	n.MoveTo(tx, cube.PosFromVec3(e.Position()), cube.PosFromVec3(target), GroundProfile{MaxVisitedNodes: 128}, navigatorTick(e))
}

// encodeNBT adds the Java InLove, LoveCause, ForcedAge and AgeLocked tags.
func (b *breeding) encodeNBT(m map[string]any) map[string]any {
	m["InLove"], m["ForcedAge"], m["AgeLocked"] = int32(b.inLove), int32(b.forcedAge), boolByte(b.ageLocked)
	if b.loveCause != uuid.Nil {
		m["LoveCause"] = javaUUIDIntArray(b.loveCause.String())
	}
	return m
}

// decodeNBT reads the Java InLove, LoveCause, ForcedAge and AgeLocked tags.
func (b *breeding) decodeNBT(m map[string]any) {
	b.inLove, b.forcedAge = int(nbtconv.Int32(m, "InLove")), int(nbtconv.Int32(m, "ForcedAge"))
	b.ageLocked = nbtconv.Bool(m, "AgeLocked")
	if id, err := uuid.Parse(javaUUIDString(m["LoveCause"])); err == nil {
		b.loveCause = id
	}
}

func viewBreederAction(a breeder, action world.EntityAction) {
	tx := a.currentTx()
	viewers := tx.Viewers(a.Position())
	for _, v := range viewers {
		v.ViewEntityAction(a, action)
	}
	tx.ReleaseViewers(viewers)
}

func viewBreederState(a breeder) {
	tx := a.currentTx()
	viewers := tx.Viewers(a.Position())
	for _, v := range viewers {
		v.ViewEntityState(a)
	}
	tx.ReleaseViewers(viewers)
}

// itemNamed reports whether the stack holds an item with one of the names.
func itemNamed(stack item.Stack, names ...string) bool {
	if stack.Empty() {
		return false
	}
	name, _ := stack.Item().EncodeItem()
	for _, n := range names {
		if name == n {
			return true
		}
	}
	return false
}

// mixedDyeColour ports DyeColor.getMixedColor: the colour of the dye a
// crafting recipe of exactly two dyes of the colours passed makes, or either
// colour if no recipe does.
func mixedDyeColour(a, b item.Colour) item.Colour {
	for _, r := range recipe.Recipes() {
		s, ok := r.(recipe.Shapeless)
		if !ok || s.Block() != "crafting_table" || len(s.Input()) != 2 || len(s.Output()) != 1 {
			continue
		}
		out, ok := s.Output()[0].Item().(item.Dye)
		if !ok {
			continue
		}
		c0, ok0 := recipeDyeColour(s.Input()[0])
		c1, ok1 := recipeDyeColour(s.Input()[1])
		if ok0 && ok1 && (c0 == a && c1 == b || c0 == b && c1 == a) {
			return out.Colour
		}
	}
	return randomParent(a, b)
}

// recipeDyeColour returns the colour of a recipe ingredient that is a dye.
func recipeDyeColour(i recipe.Item) (item.Colour, bool) {
	s, ok := i.(item.Stack)
	if !ok {
		return item.Colour{}, false
	}
	d, ok := s.Item().(item.Dye)
	return d.Colour, ok
}

// blockItemInTag reports whether the stack holds the item of a block in the
// vanilla block tag, as item tags copied from block tags hold.
func blockItemInTag(stack item.Stack, tag string) bool {
	b, ok := stack.Item().(world.Block)
	return ok && !stack.Empty() && blockInTag(b, tag)
}

// blockInTag reports whether the block is in the vanilla block tag.
func blockInTag(b world.Block, tag string) bool {
	state, err := world.CanonicalStateOf(b)
	if err != nil {
		return false
	}
	in, _ := gamedata.BlockTagContains(tag, state.Name)
	return in
}
