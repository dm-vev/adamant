package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Bedrock's horse flags, sent in the actor data that Java keeps the eating
// and standing flags of AbstractHorse in.
const (
	horseFlagEating   = 32
	horseFlagStanding = 128
)

// horseRearing reports whether the species can rear, which
// AbstractHorse.canPerformRearing denies llamas and camels.
func (b *MountBehaviour) horseRearing() bool {
	return !b.llama() && !b.camel()
}

func (b *MountBehaviour) llama() bool { return b.kind == mountLlama || b.kind == mountTraderLlama }
func (b *MountBehaviour) camel() bool { return b.kind == mountCamel || b.kind == mountCamelHusk }

// maxTemper is AbstractHorse.getMaxTemper, lower for llamas.
func (b *MountBehaviour) maxTemper() int {
	if b.llama() {
		return 30
	}
	return 100
}

// mobControlled ports isMobControlled: zombie horses and camel husks ridden
// by a mob let it steer.
func (b *MountBehaviour) mobControlled() bool {
	if (b.kind != mountZombieHorse && b.kind != mountCamelHusk) || b.passengers[0] == nil {
		return false
	}
	return b.passengers[0].Type().EncodeEntity() != "minecraft:player"
}

// controlled reports whether a player steers the mount: a saddled mount
// carrying a player first.
func (b *MountBehaviour) controlled() bool {
	return b.saddled && b.riddenByPlayer()
}

// horseGoals returns the goals of the species, in the order of their
// priorities.
func (b *MountBehaviour) horseGoals(m *Mount) animalGoalSpec {
	s := animalGoalSpec{speed: b.movementSpeed, breed: 1, parent: 1, stroll: .7, reach: 1}
	switch b.kind {
	case mountHorse, mountDonkey, mountMule:
		// AbstractHorse.addBehaviourGoals: MountPanicGoal and TemptGoal.
		s.panic, s.tempt = 1.2, 1.25
		s.tempting = func(stack item.Stack) bool {
			return itemNamed(stack, "minecraft:golden_carrot", "minecraft:golden_apple", "minecraft:enchanted_golden_apple")
		}
	case mountZombieHorse:
		s.tempt, s.tempting = 1.25, m.breedingFood
	case mountLlama:
		s.panic, s.tempt = 1.2, 1.25
		s.tempting = func(stack item.Stack) bool { return itemNamed(stack, "minecraft:hay_block") }
	case mountTraderLlama:
		// TraderLlama adds a PanicGoal at twice the speed above Llama's own.
		s.panic, s.tempt = 2, 1.25
		s.tempting = func(stack item.Stack) bool { return itemNamed(stack, "minecraft:hay_block") }
	}
	if b.mobControlled() {
		s.panic = 0
	}
	if b.horseRearing() || b.camel() {
		s.species = b
	}
	return s
}

// run ports the parts of AbstractHorse.aiStep that keep the horse still: now
// and then a horse on a grass block lowers its head to eat for 50 ticks, and
// RandomStandGoal rears it up from time to time.
func (b *MountBehaviour) run(e *Ent, tx *world.Tx) bool {
	return b.eating || b.standCounter > 0
}

// stop is part of speciesGoal: eating and rearing are not goals in Java and
// run alongside the others.
func (b *MountBehaviour) stop() {}

// tickHorse ports AbstractHorse.tick and aiStep and returns the velocity
// and rotation the horse's own AI gives it.
func (b *MountBehaviour) tickHorse(m *Mount, tx *world.Tx) (mgl64.Vec3, cube.Rotation) {
	e := m.Ent
	if b.standCounter > 0 {
		if b.standCounter--; b.standCounter == 0 {
			m.updateState()
		}
	}
	if rand.IntN(900) == 0 {
		m.Heal(1, nil)
	}
	if !b.llama() {
		below := cube.PosFromVec3(e.Position()).Side(cube.FaceDown)
		if !b.eating && !b.vehicle() && rand.IntN(300) == 0 {
			if _, grass := tx.Block(below).(block.Grass); grass {
				b.eating = true
				m.updateState()
			}
		}
		if b.eating {
			if b.eatingCounter++; b.eatingCounter > 50 {
				b.eatingCounter, b.eating = 0, false
				m.updateState()
			}
		}
	}
	if b.horseRearing() {
		b.randomStand(m)
	}
	spec := b.horseGoals(m)
	crazy := b.runAroundLikeCrazy(m, tx)
	if crazy {
		spec = animalGoalSpec{speed: b.movementSpeed}
	}
	vel, rot := b.goals.tick(e, tx, m, spec)
	if crazy {
		vel, rot, _ = steer(e, &b.crazyNavigator, groundSpeed(b.movementSpeed, 1.2), 1)
	}
	if b.eating || b.standCounter > 0 {
		// AbstractHorse.isImmobile: the horse does not move while it eats
		// or rears.
		vel, rot = e.data.Vel, e.data.Rot
	}
	return vel, rot
}

// vehicle reports whether anything rides the mount.
func (b *MountBehaviour) vehicle() bool { return b.passengers[0] != nil || b.passengers[1] != nil }

// randomStand ports RandomStandGoal: every 20 seconds and more often the
// longer it waited, a horse rears up one time in ten.
func (b *MountBehaviour) randomStand(m *Mount) {
	if b.nextStand++; b.nextStand > 0 && rand.IntN(1000) < b.nextStand {
		b.nextStand = -400
		if !b.eating && b.standCounter == 0 && rand.IntN(10) == 0 {
			b.standIfPossible(m)
		}
	}
}

// standIfPossible ports AbstractHorse.standIfPossible: the horse rears up for
// a second, stopping eating.
func (b *MountBehaviour) standIfPossible(m *Mount) {
	if !b.horseRearing() {
		return
	}
	b.eating, b.eatingCounter, b.standCounter = false, 0, 20
	m.updateState()
}

// makeMad ports AbstractHorse.makeMad: the horse rears up and neighs angrily.
func (b *MountBehaviour) makeMad(m *Mount) {
	if b.standCounter > 0 {
		return
	}
	b.standIfPossible(m)
	m.tx.PlaySound(m.Position(), sound.NativeEvent{SoundType: "mad", EntityType: m.H().Type().EncodeEntity(), ExtraData: -1})
}

// runAroundLikeCrazy ports RunAroundLikeCrazyGoal: an untamed horse carrying
// a player runs around and, every two and a half seconds on average, either
// accepts them with a chance growing with its temper, or bucks them off and
// grows more tempered.
func (b *MountBehaviour) runAroundLikeCrazy(m *Mount, tx *world.Tx) bool {
	if b.tamed || b.mobControlled() || !b.vehicle() {
		b.crazy = false
		return false
	}
	if !b.crazy || b.crazyNavigator.Index >= len(b.crazyNavigator.Path) {
		mob := newRandomPosMob(m.Ent, tx)
		goal, ok := mob.randomPos(5, 4)
		if !ok {
			b.crazy = false
			return false
		}
		b.crazyNavigator = Navigator{}
		if !b.crazyNavigator.MoveTo(tx, cube.PosFromVec3(m.Position()), goal, mob.profile, navigatorTick(m.Ent)) {
			b.crazy = false
			return false
		}
		b.crazy = true
	}
	if rand.IntN(50) == 0 {
		b.tryTaming(m, tx)
	}
	return b.crazy
}

// tryTaming is the taming attempt of RunAroundLikeCrazyGoal.tick.
func (b *MountBehaviour) tryTaming(m *Mount, tx *world.Tx) {
	rider, ok := b.passengers[0].Entity(tx)
	if !ok {
		return
	}
	if p, player := rider.(interface{ UUID() uuid.UUID }); player && rider.H().Type().EncodeEntity() == "minecraft:player" {
		if b.maxTemper() > 0 && rand.IntN(b.maxTemper()) < b.temper {
			b.tameWithName(m, p.UUID())
			return
		}
		b.modifyTemper(5)
	}
	b.DismountAll(m.Ent, tx)
	b.crazy = false
	b.makeMad(m)
	m.viewAction(TameAction{Succeeded: false})
}

// tameWithName ports AbstractHorse.tameWithName.
func (b *MountBehaviour) tameWithName(m *Mount, owner uuid.UUID) {
	b.owner, b.tamed, b.crazy = owner.String(), true, false
	m.viewAction(TameAction{Succeeded: true})
	m.updateState()
}

// modifyTemper ports AbstractHorse.modifyTemper.
func (b *MountBehaviour) modifyTemper(amount int) {
	b.temper = max(0, min(b.temper+amount, b.maxTemper()))
}

// handleEating ports AbstractHorse.handleEating, Llama.handleEating and
// Camel.handleEating: food heals the mount, lets a foal grow up faster and
// calms an untamed one, and golden food or hay puts a tamed adult in love.
func (b *MountBehaviour) handleEating(m *Mount, user item.User, stack item.Stack) bool {
	name, _ := stack.Item().EncodeItem()
	if b.camel() {
		return b.camelEating(m, user)
	}
	heal, grow, temper, love := 0.0, 0, 0, false
	if b.llama() {
		switch name {
		case "minecraft:wheat":
			heal, grow, temper = 2, 10, 3
		case "minecraft:hay_block":
			heal, grow, temper, love = 10, 90, 6, true
		}
	} else {
		switch name {
		case "minecraft:wheat":
			heal, grow, temper = 2, 20, 3
		case "minecraft:sugar":
			heal, grow, temper = 1, 30, 3
		case "minecraft:hay_block":
			heal, grow = 20, 180
		case "minecraft:apple":
			heal, grow, temper = 3, 60, 3
		case "minecraft:red_mushroom":
			heal, temper = 3, 3
		case "minecraft:carrot":
			heal, grow, temper = 3, 60, 3
		case "minecraft:golden_carrot":
			heal, grow, temper, love = 4, 60, 5, true
		case "minecraft:golden_apple", "minecraft:enchanted_golden_apple":
			heal, grow, temper, love = 10, 240, 10, true
		}
	}
	used := false
	if love && b.tamed && b.ageTicks == 0 && b.canFallInLove() {
		used = true
		b.setInLove(m, user)
	}
	if m.Health() < m.MaxHealth() && heal > 0 {
		m.Heal(heal, FoodHealingSource{})
		used = true
	}
	if b.canAgeUp() && grow > 0 {
		ageUp(m, grow, false)
		used = true
	}
	if temper > 0 && (used || !b.tamed) && b.temper < b.maxTemper() {
		b.modifyTemper(temper)
		used = true
	}
	if used {
		m.tx.EmitGameEvent(world.GameEventEat, m.Position(), world.GameEventContext{Source: m})
	}
	return used
}

// camelEating ports Camel.handleEating for its food.
func (b *MountBehaviour) camelEating(m *Mount, user item.User) bool {
	healed := m.Health() < m.MaxHealth()
	if healed {
		m.Heal(2, FoodHealingSource{})
	}
	love := b.tamed && b.ageTicks == 0 && b.canFallInLove()
	if love {
		b.setInLove(m, user)
	}
	grow := b.canAgeUp()
	if grow {
		ageUp(m, 10, false)
	}
	if !healed && !love && !grow {
		return false
	}
	m.tx.EmitGameEvent(world.GameEventEat, m.Position(), world.GameEventContext{Source: m})
	return true
}

// canAgeUp ports AgeableMob.canAgeUp: a baby that is not age locked, unless
// it is a skeleton or zombie horse.
func (b *MountBehaviour) canAgeUp() bool {
	return b.ageTicks < 0 && !b.breeding.ageLocked && b.kind != mountSkeletonHorse && b.kind != mountZombieHorse
}

// canFallInLove ports Animal.canFallInLove, which zombie horses and camel
// husks deny.
func (b *MountBehaviour) canFallInLove() bool {
	return b.breeding.inLove <= 0 && b.kind != mountZombieHorse && b.kind != mountCamelHusk && b.kind != mountSkeletonHorse
}

// setInLove ports Animal.setInLove.
func (b *MountBehaviour) setInLove(m *Mount, user item.User) {
	b.breeding.inLove = loveDuration
	if p, ok := user.(interface{ UUID() uuid.UUID }); ok {
		b.breeding.loveCause = p.UUID()
	}
	viewBreederAction(m, LoveHeartsAction{})
	viewBreederState(m)
}

// canParent ports AbstractHorse.canParent: a tamed adult at full health
// that nothing rides and that is in love.
func (b *MountBehaviour) canParent(m *Mount) bool {
	return !b.vehicle() && b.tamed && b.ageTicks >= 0 && m.Health() >= m.MaxHealth() && b.breeding.InLove()
}

// matesWith ports canMate of the horse species: horses and donkeys mate with
// each other, llamas with llamas including trader llamas, and camels with
// camels, while mules, skeleton and zombie horses and camel husks do not
// mate.
func (m *Mount) matesWith(partner breeder) bool {
	other, ok := partner.(interface{ mount() *Mount })
	if !ok {
		return false
	}
	o := other.mount()
	b, ob := m.b(), o.b()
	switch b.kind {
	case mountHorse, mountDonkey:
		if ob.kind != mountHorse && ob.kind != mountDonkey {
			return false
		}
	case mountLlama, mountTraderLlama:
		if !ob.llama() {
			return false
		}
	case mountCamel:
		if ob.kind != mountCamel {
			return false
		}
	default:
		return false
	}
	return b.canParent(m) && ob.canParent(o)
}

// mount returns the mount of a species wrapper.
func (m *Mount) mount() *Mount { return m }

// Attribute ranges of AbstractHorse.setOffspringAttributes.
var (
	horseHealthRange = [2]float64{15, 15 + 7 + 8}
	horseJumpRange   = [2]float64{.4, .4 + .2*3}
	horseSpeedRange  = [2]float64{.45 * .25, (.45 + .3*3) * .25}
)

// breedOffspring ports getBreedOffspring of the horse species: a mule from a
// horse and a donkey, a foal of its parents' kind otherwise, with attributes
// mixed from both parents.
func (m *Mount) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	o := partner.(interface{ mount() *Mount }).mount()
	b, ob := m.b(), o.b()
	kind := b.kind
	switch {
	case b.kind == mountHorse && ob.kind == mountDonkey, b.kind == mountDonkey && ob.kind == mountHorse:
		kind = mountMule
	case b.llama():
		// Llama.makeNewLlama: the young of trader llamas are llamas too.
		kind = mountLlama
	}
	c := defaultMountConfig(kind)
	c.AgeTicks = babyStartAge
	if kind != mountCamel {
		c.MaxHealth = offspringAttribute(m.MaxHealth(), o.MaxHealth(), horseHealthRange)
		c.Health = c.MaxHealth
		c.JumpStrength = offspringAttribute(b.jumpStrength, ob.jumpStrength, horseJumpRange)
		c.Speed = offspringAttribute(b.movementSpeed, ob.movementSpeed, horseSpeedRange)
	}
	switch {
	case kind == mountHorse:
		c.Variant = horseOffspringLook(b.variant, ob.variant)
	case b.llama():
		strength := rand.IntN(max(b.strength, ob.strength)) + 1
		if rand.Float32() < .03 {
			strength++
		}
		c.Strength = min(strength, 5)
		c.Variant = randomParent(b.variant, ob.variant)
	}
	return babyAt(m).New(mountTypeFor(kind), c)
}

// horseOffspringLook ports the colour and markings choice of
// Horse.getBreedOffspring: four in nine foals take the colour of the parent
// that bred, four of the partner and one a random one, and two in five the
// markings of either parent or one in five random markings.
func horseOffspringLook(own, partner int) int {
	colour := own & 0xff
	switch r := rand.IntN(9); {
	case r >= 8:
		colour = rand.IntN(7)
	case r >= 4:
		colour = partner & 0xff
	}
	markings := own >> 8 & 0xff
	switch r := rand.IntN(5); {
	case r >= 4:
		markings = rand.IntN(5)
	case r >= 2:
		markings = partner >> 8 & 0xff
	}
	return colour | markings<<8
}

// offspringAttribute ports AbstractHorse.createOffspringAttribute: the
// average of the parents, spread by their difference plus 15% of the range
// either way and reflected back into the range.
func offspringAttribute(a, b float64, r [2]float64) float64 {
	a, b = max(r[0], min(a, r[1])), max(r[0], min(b, r[1]))
	margin := .15 * (r[1] - r[0])
	spread := math.Abs(a-b) + margin*2
	quality := (rand.Float64()+rand.Float64()+rand.Float64())/3 - .5
	v := (a+b)/2 + spread*quality
	switch {
	case v > r[1]:
		return r[1] - (v - r[1])
	case v < r[0]:
		return r[0] + (r[0] - v)
	}
	return v
}

// randomizeHorse ports finalizeSpawn and randomizeAttributes of the horse
// species with the random of the spawn.
func (b *MountBehaviour) randomizeHorse(m *Mount, random *java26.Random) {
	group := m.H().SpawnGroup()
	data, grouped := mountSpawnGroup{}, false
	if group != nil {
		data, grouped = group.Data.(mountSpawnGroup)
		grouped = grouped && data.kind == b.kind
	}
	babyChance, babies := float32(.2), true
	switch b.kind {
	case mountHorse:
		// Horse.finalizeSpawn: horses of a group share their colour.
		colour := 0
		if grouped {
			colour = data.variant
		} else {
			colour = int(random.NextInt(7))
		}
		b.variant = colour | int(random.NextInt(5))<<8
		data.variant = colour
	case mountLlama, mountTraderLlama:
		// Llama.setRandomStrength and the shared colour of LlamaGroupData,
		// which keeps the default chance of babies.
		strengths := int32(3)
		if random.NextFloat() < .04 {
			strengths = 5
		}
		b.strength = 1 + int(random.NextInt(strengths))
		if grouped {
			b.variant = data.variant
		} else {
			b.variant = int(random.NextInt(4))
		}
		data.variant, babyChance = b.variant, .05
		if b.kind == mountTraderLlama && !grouped {
			babies = false
		}
	case mountCamelHusk:
		babies = false
	}
	switch b.kind {
	case mountHorse:
		b.health.SetMaxHealth(horseMaxHealth(random))
		b.movementSpeed = (.45 + random.NextDouble()*.3 + random.NextDouble()*.3 + random.NextDouble()*.3) * .25
		b.jumpStrength = horseJumpStrength(random)
	case mountDonkey, mountMule, mountLlama, mountTraderLlama:
		b.health.SetMaxHealth(horseMaxHealth(random))
	case mountSkeletonHorse:
		b.jumpStrength = horseJumpStrength(random)
	case mountZombieHorse:
		b.jumpStrength = .5 + (random.NextDouble()+random.NextDouble()+random.NextDouble())/15
		b.movementSpeed = (9 + random.NextDouble() + random.NextDouble() + random.NextDouble()) / float64(float32(42.16))
	}
	b.health.AddHealth(b.health.MaxHealth())
	// AgeableMob.finalizeSpawn: in a spawn group every mount but the first
	// may be a foal.
	if group != nil {
		if babies && data.size > 0 && random.NextFloat() <= babyChance {
			b.ageTicks = babyStartAge
		}
		data.kind = b.kind
		data.size++
		group.Data = data
	}
}

// horseMaxHealth ports AbstractHorse.generateMaxHealth.
func horseMaxHealth(random *java26.Random) float64 {
	return float64(15 + random.NextInt(8) + random.NextInt(9))
}

// horseJumpStrength ports AbstractHorse.generateJumpStrength.
func horseJumpStrength(random *java26.Random) float64 {
	return float64(float32(.4)) + random.NextDouble()*.2 + random.NextDouble()*.2 + random.NextDouble()*.2
}
