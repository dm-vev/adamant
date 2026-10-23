package entity

import (
	"hash/fnv"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
)

// FrogType is the Java frog entity type, used as the real destination of tadpole metamorphosis.
var FrogType frogType

type frogType struct{}

func (frogType) EncodeEntity() string { return "minecraft:frog" }
func (frogType) Open(tx *world.Tx, h *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil {
		frogConfig{}.Apply(data)
	}
	if h != nil {
		hash := fnv.New64a()
		_, _ = hash.Write([]byte(h.UUID().String()))
		data.Data.(*frogBehaviour).random = vanilla.NewLegacyRandom(int64(hash.Sum64()))
	}
	return &Frog{Ent: &Ent{tx: tx, handle: h, data: data}}
}
func (frogType) BBox(world.Entity) cube.BBox { return cube.Box(-.25, 0, -.25, .25, .5, .25) }
func (frogType) DecodeNBT(nbt map[string]any, data *world.EntityData) {
	c := frogConfig{}
	if _, ok := nbt["Health"]; ok {
		c.health, c.healthSet = float64(nbtconv.Float32(nbt, "Health")), true
	}
	if _, ok := nbt["Variant"]; ok {
		c.variant = int(nbtconv.Int32(nbt, "Variant"))
	}
	c.age = int(nbtconv.Int32(nbt, "Age"))
	if brain, ok := nbt["Brain"].(map[string]any); ok {
		if memories, ok := brain["memories"].(map[string]any); ok {
			_, c.pregnant = memories["minecraft:is_pregnant"]
		}
	}
	c.Apply(data)
	data.Data.(*frogBehaviour).breeding.decodeNBT(nbt)
}
func (frogType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*frogBehaviour)
	m := b.breeding.encodeNBT(map[string]any{"Health": float32(b.health.Health()), "Variant": int32(b.variant), "Age": int32(b.age)})
	if b.pregnant {
		m["Brain"] = map[string]any{"memories": map[string]any{"minecraft:is_pregnant": map[string]any{"value": map[string]any{}}}}
	}
	return m
}

type frogConfig struct {
	health              float64
	variant, age        int
	healthSet, pregnant bool
}

func (c frogConfig) Apply(data *world.EntityData) {
	h := c.health
	if !c.healthSet {
		h = 10
	}
	data.Data = &frogBehaviour{health: NewHealthManager(h, 10), effects: NewEffectManager(), variant: c.variant, age: c.age, pregnant: c.pregnant, movement: &MovementComputer{Gravity: .08, Drag: .1, DragBeforeGravity: true}, random: vanilla.NewLegacyRandom(int64(c.variant)), speed: 1, nextWander: 1,
		panic: panicState{brain: true}}
}

type frogBehaviour struct {
	health              *HealthManager
	effects             *EffectManager
	movement            *MovementComputer
	random              vanilla.Random
	variant, nextWander int
	speed               float64
	dead                bool
	tongue              frogTongue
	// age is the breeding cooldown: frogs are never babies.
	age      int
	breeding breeding
	// pregnant is set on a frog that bred until it lays frogspawn.
	pregnant     bool
	landTarget   mgl64.Vec3
	hasLand      bool
	nextLandScan int
	panic        panicState
	navigator    Navigator
}

// Frog is a living amphibian runtime entity. Tadpoles convert into this runtime at Java's age threshold.
type Frog struct{ *Ent }

func (f *Frog) behaviour() *frogBehaviour { return f.Behaviour().(*frogBehaviour) }

// LootComponents exposes the frog variant (Bedrock 0 temperate, 1 cold, 2 warm)
// to loot predicates such as the magma cube froglight drop.
func (f *Frog) LootComponents() map[string]any {
	return map[string]any{"minecraft:frog/variant": "minecraft:" + []string{"temperate", "cold", "warm"}[max(0, min(f.behaviour().variant, 2))]}
}
func (f *Frog) Tick(tx *world.Tx, current int64) {
	f.behaviour().effects.Tick(f, tx)
	f.Ent.Tick(tx, current)
}
func (b *frogBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.dead {
		return nil
	}
	frog := &Frog{Ent: e}
	partner, inLove := tickBreeding(frog, tx)
	vel, rot := e.data.Vel, e.data.Rot
	if b.panic.run(e, tx, &b.navigator) {
		// FrogAi's AnimalPanic: SmoothSwimmingMoveControl walks at a tenth
		// of the frog's speed of 1 on land.
		vel, rot, _ = steer(e, &b.navigator, groundSpeed(.1, 2), .5)
	} else if walk, busy := b.tongue.tick(e, tx, navigatorTick(e)); busy {
		vel[0], vel[2] = walk[0], walk[2]
	} else if inLove {
		vel = frogWalkTo(e, vel, partner)
	} else if b.pregnant && b.layOrSeekSpawnSite(frog, tx) {
		if b.hasLand {
			vel = frogWalkTo(e, vel, b.landTarget)
		}
	} else {
		if b.nextWander > 0 {
			b.nextWander--
		}
		if b.nextWander == 0 {
			b.nextWander = 40 + int(b.random.NextInt(41))
			vel = vel.Add(mgl64.Vec3{(float64(b.random.NextFloat())*2 - 1) * b.speed * .1, 0, (float64(b.random.NextFloat())*2 - 1) * b.speed * .1})
		}
	}
	m := b.movement.TickMovement(e, e.Position(), vel, rot, e.data.Rot, tx)
	e.data.Pos, e.data.Vel, e.data.Rot = m.Position(), m.Velocity(), m.Rotation()
	return m
}

// frogWalkTo returns the velocity of a frog walking to target.
func frogWalkTo(e *Ent, vel, target mgl64.Vec3) mgl64.Vec3 {
	d := target.Sub(e.Position())
	d[1] = 0
	if d.Len() > .25 {
		d = d.Normalize().Mul(.1)
		vel[0], vel[2] = d[0], d[2]
	} else {
		vel[0], vel[2] = 0, 0
	}
	return vel
}

// layOrSeekSpawnSite ports the LAY_SPAWN activity of a pregnant frog on
// land: TryLaySpawnOnFluidNearLand lays frogspawn on water next to the block
// the frog stands on, and otherwise TryFindLandNearLiquid walks it, every 2
// seconds, to the nearest land next to water within 8 blocks. It reports
// whether the frog is busy laying.
func (b *frogBehaviour) layOrSeekSpawnSite(frog *Frog, tx *world.Tx) bool {
	pos := cube.PosFromVec3(frog.Position())
	if frogInWater(tx, pos) || !frog.OnGround() {
		return false
	}
	below := pos.Side(cube.FaceDown)
	for _, f := range cube.HorizontalFaces() {
		water := below.Side(f)
		if !frogWater(tx, water) || len(tx.Block(water).Model().BBox(water, tx)) != 0 {
			continue
		}
		site := water.Side(cube.FaceUp)
		if _, air := tx.Block(site).(block.Air); air {
			spawn := block.FrogSpawn{}
			tx.SetBlock(site, spawn, nil)
			tx.EmitGameEvent(world.GameEventBlockPlace, site.Vec3Centre(), world.GameEventContext{Source: frog, AffectedState: spawn})
			tx.PlaySound(frog.Position(), sound.NativeEvent{SoundType: "lay_spawn", EntityType: "minecraft:frog", ExtraData: -1})
			b.pregnant, b.hasLand = false, false
			return true
		}
	}
	if b.nextLandScan > 0 {
		b.nextLandScan--
		return true
	}
	b.nextLandScan = 40
	b.landTarget, b.hasLand = frogLandNearWater(tx, frog, pos)
	return true
}

// frogLandNearWater ports the search of TryFindLandNearLiquid: the closest
// position by Manhattan distance within 8 blocks, horizontally away from the
// frog, that it fits in, stands on a block and is next to an empty block over
// water.
func frogLandNearWater(tx *world.Tx, frog *Frog, pos cube.Pos) (mgl64.Vec3, bool) {
	const r = 8
	for dist := 1; dist <= r*3; dist++ {
		for dx := -r; dx <= r; dx++ {
			for dy := -r; dy <= r; dy++ {
				dz := dist - abs(dx) - abs(dy)
				if dz < 0 || dz > r {
					continue
				}
				for _, z := range []int{-dz, dz}[:1+boolInt(dz != 0)] {
					p := pos.Add(cube.Pos{dx, dy, z})
					if dx == 0 && z == 0 || len(tx.Block(p).Model().BBox(p, tx)) != 0 || len(tx.Block(p.Side(cube.FaceDown)).Model().BBox(p.Side(cube.FaceDown), tx)) == 0 {
						continue
					}
					for _, f := range cube.HorizontalFaces() {
						side := p.Side(f)
						if _, air := tx.Block(side).(block.Air); air && frogWater(tx, side.Side(cube.FaceDown)) {
							return p.Vec3Middle(), true
						}
					}
				}
			}
		}
	}
	return mgl64.Vec3{}, false
}

// frogWater reports whether pos holds water, which supports frogspawn.
func frogWater(tx *world.Tx, pos cube.Pos) bool {
	l, ok := tx.Liquid(pos)
	return ok && l.LiquidType() == "water"
}

func frogInWater(tx *world.Tx, pos cube.Pos) bool { return frogWater(tx, pos) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func (f *Frog) Health() float64        { return f.behaviour().health.Health() }
func (f *Frog) MaxHealth() float64     { return f.behaviour().health.MaxHealth() }
func (f *Frog) SetMaxHealth(v float64) { f.behaviour().health.SetMaxHealth(v) }
func (f *Frog) Dead() bool             { return f.behaviour().dead || f.Health() <= 0 }

// Panicking reports whether the frog runs from danger.
func (f *Frog) Panicking() bool { return f.behaviour().panic.active }
func (f *Frog) Hurt(d float64, src world.DamageSource) (float64, bool) {
	if f.Dead() || d <= 0 {
		return 0, false
	}
	b := f.behaviour()
	before := b.health.Health()
	b.health.AddHealth(-d)
	b.panic.hurt(src, f.tx.CurrentTick())
	resetLove(f)
	dealt := before - b.health.Health()
	f.viewAction(HurtAction{})
	onMobHurt(f.tx, f)
	if f.Dead() {
		b.dead = true
		f.viewAction(DeathAction{})
		dropAllDeathLoot(f.tx, f, src)
		_ = f.CloseIn(f.tx)
	}
	return dealt, true
}
func (f *Frog) Heal(h float64, _ world.HealingSource) float64 {
	if f.Dead() || h <= 0 {
		return 0
	}
	before := f.Health()
	f.behaviour().health.AddHealth(h)
	return f.Health() - before
}
func (f *Frog) KnockBack(src mgl64.Vec3, force, height float64) {
	d := f.Position().Sub(src)
	if d.Len() != 0 {
		d = d.Normalize().Mul(force)
	}
	d[1] = height
	f.SetVelocity(d)
}
func (f *Frog) AddEffect(e effect.Effect)  { f.behaviour().effects.Add(e, f) }
func (f *Frog) RemoveEffect(t effect.Type) { f.behaviour().effects.Remove(t, f) }
func (f *Frog) Effects() []effect.Effect   { return f.behaviour().effects.Effects() }
func (f *Frog) Speed() float64             { return f.behaviour().speed }
func (f *Frog) SetSpeed(v float64)         { f.behaviour().speed = v }
func (f *Frog) viewAction(action world.EntityAction) {
	viewers := f.tx.Viewers(f.Position())
	for _, v := range viewers {
		v.ViewEntityAction(f, action)
	}
	f.tx.ReleaseViewers(viewers)
}

func NewFrog(opts world.EntitySpawnOpts) *world.EntityHandle { return opts.New(FrogType, frogConfig{}) }

var _ world.EntityType = frogType{}
var _ Living = (*Frog)(nil)

// Interact feeds the frog slime balls to breed it.
func (f *Frog) Interact(_ *world.Tx, u item.User, ctx *item.UseContext) bool {
	return feedBreeder(f, u, ctx)
}

// InLove reports whether the frog is in love.
func (f *Frog) InLove() bool { return f.behaviour().breeding.InLove() }

// Pregnant reports whether the frog bred and is yet to lay frogspawn.
func (f *Frog) Pregnant() bool { return f.behaviour().pregnant }

func (f *Frog) breedingState() *breeding { return &f.behaviour().breeding }
func (f *Frog) ageTicks() *int           { return &f.behaviour().age }

func (f *Frog) breedingFood(stack item.Stack) bool { return itemNamed(stack, "minecraft:slime_ball") }

func (f *Frog) breedOffspring(*world.Tx, breeder) *world.EntityHandle { return nil }

// spawnChildFromBreeding ports Frog.spawnChildFromBreeding: instead of a
// baby, the frog becomes pregnant and lays frogspawn later.
func (f *Frog) spawnChildFromBreeding(tx *world.Tx, partner breeder) {
	finishBreeding(f, partner, tx)
	f.behaviour().pregnant = true
}
