package entity

import (

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var axolotlAlwaysHostile = map[string]struct{}{
	"minecraft:drowned": {}, "minecraft:guardian": {}, "minecraft:elder_guardian": {},
}
var axolotlHuntTargets = map[string]struct{}{
	"minecraft:tropical_fish": {}, "minecraft:pufferfish": {}, "minecraft:salmon": {},
	"minecraft:cod": {}, "minecraft:squid": {}, "minecraft:glow_squid": {}, "minecraft:tadpole": {},
}
var tropicalFishCommonVariants = [...]int32{
	0x07010101, 0x07070001, 0x0b070001, 0x07000501, 0x070b0100, 0x00010000,
	0x03060500, 0x040a0301, 0x0e000500, 0x04000500, 0x07000201, 0x01000501,
	0x06090300, 0x03050400, 0x000e0401, 0x0e070200, 0x000e0301, 0x04000001,
	0x000e0000, 0x00070100, 0x04090300, 0x04040001,
}
var tropicalFishPatterns = [...]int32{0, 0x100, 0x200, 0x300, 0x400, 0x500, 1, 0x101, 0x201, 0x301, 0x401, 0x501}

// AcceptItem ports Animal.mobInteract and Axolotl.usePlayerItem for
// axolotls: a bucket of tropical fish feeds them and leaves a water bucket.
func (b *AquaticMobBehaviour) AcceptItem(e *Ent, from world.Entity, _ *world.Tx, ctx *item.UseContext) bool {
	user, ok := from.(item.User)
	if b.id != "minecraft:axolotl" || ctx == nil || !ok {
		return false
	}
	a := &Axolotl{&AquaticMob{Ent: e}}
	held, _ := user.HeldItems()
	if !feedBreeder(a, user, ctx) {
		return false
	}
	if a.breedingFood(held) {
		ctx.NewItem = item.NewStack(item.Bucket{Content: item.LiquidBucketContent(block.Water{Still: true, Depth: 8})}, 1)
		ctx.ReplaceHeldItem = true
	}
	return true
}

// InLove reports whether the axolotl is in love.
func (a *Axolotl) InLove() bool { return a.behaviour().breeding.InLove() }

func (a *Axolotl) breedingState() *breeding { return &a.behaviour().breeding }
func (a *Axolotl) ageTicks() *int           { return &a.behaviour().age }

// breedingFood reports whether the stack is in #axolotl_food, a bucket of
// tropical fish.
func (a *Axolotl) breedingFood(stack item.Stack) bool {
	bucket, ok := stack.Item().(item.FishBucket)
	return ok && !stack.Empty() && bucket.Entity == "minecraft:tropical_fish"
}

// breedOffspring ports Axolotl.getBreedOffspring: the baby takes the variant
// of either parent, or is blue with a chance of 1 in 1200.
func (a *Axolotl) breedOffspring(_ *world.Tx, partner breeder) *world.EntityHandle {
	b := a.behaviour()
	variant := randomParent(b.variant, partner.(*Axolotl).behaviour().variant)
	if b.random.NextInt(1200) == 0 {
		variant = 4
	}
	return babyAt(a).New(AxolotlType, AquaticMobConfig{ID: "minecraft:axolotl", MaxHealth: 14, Variant: variant, Age: babyStartAge})
}

type aquaticSchoolGroupData struct {
	leader *world.EntityHandle
	size int
}
type tropicalFishSchoolGroupData struct {
	school *aquaticSchoolGroupData
	variant int
}
type axolotlSpawnGroupData struct {
	variants [2]int
	size int
}

func aquaticSchoolSize(b *AquaticMobBehaviour) int {
	if b.schoolLeader != nil || b.schoolGroup == nil {
		return 1
	}
	return b.schoolGroup.size
}

func (b *AquaticMobBehaviour) finalizeSchoolSpawn(e *AquaticMob) *aquaticSchoolGroupData {
	group:=e.H().SpawnGroup()
	if group==nil { return b.schoolGroup }
	switch data:=group.Data.(type) {
	case *aquaticSchoolGroupData:
		b.schoolGroup=data
	case *tropicalFishSchoolGroupData:
		b.schoolGroup=data.school
	default:
		b.schoolGroup=&aquaticSchoolGroupData{leader:e.H(),size:1}
		group.Data=b.schoolGroup
	}
	if b.schoolGroup.leader!=e.H() {
		b.schoolLeader=b.schoolGroup.leader
		b.schoolGroup.size++
	}
	return b.schoolGroup
}

// schoolingLeader ports FollowFlockLeaderGoal and AbstractSchoolingFish's
// 8-block formation search, 11-block leash, and bounded school sizes.
func (b *AquaticMobBehaviour) schoolingLeader(e *Ent, tx *world.Tx) world.Entity {
	if b.id!="minecraft:cod"&&b.id!="minecraft:salmon"&&b.id!="minecraft:tropical_fish" { return nil }
	if b.schoolLeader!=nil {
		current,ok:=b.schoolLeader.Entity(tx)
		if ok&&current.H().Type().EncodeEntity()==b.id&&current.Position().Sub(e.Position()).LenSqr()<=121 { return current }
		if b.schoolGroup!=nil { b.schoolGroup.size-- }
		b.schoolLeader=nil
		b.nextSchoolTick=200+int(b.random.NextInt(200)%20)
	}
	if aquaticSchoolSize(b)>1 { return nil }
	if b.nextSchoolTick>0 { b.nextSchoolTick--;return nil }
	b.nextSchoolTick=200+int(b.random.NextInt(200)%20)
	limit:=8
	if b.id=="minecraft:salmon" { limit=5 }
	box:=cube.Box(-8,-8,-8,8,8,8).Translate(e.Position())
	fish:=make([]world.Entity,0,limit)
	var leader world.Entity
	for candidate:=range tx.EntitiesWithin(box) {
		if candidate.H().Type().EncodeEntity()!=b.id { continue }
		other,ok:=candidate.(interface{behaviour()*AquaticMobBehaviour})
		if !ok||other.behaviour().schoolLeader!=nil { continue }
		fish=append(fish,candidate)
		if leader==nil&&aquaticSchoolSize(other.behaviour())>1&&aquaticSchoolSize(other.behaviour())<limit { leader=candidate }
	}
	if leader==nil { leader=aquaticWorldEntity(e) }
	lb:=leader.(interface{behaviour()*AquaticMobBehaviour}).behaviour()
	if lb.schoolGroup==nil { lb.schoolGroup=&aquaticSchoolGroupData{leader:leader.H(),size:1} }
	if lb.schoolGroup.leader==nil { lb.schoolGroup.leader=leader.H() }
	for _,other:=range fish {
		if other.H()==leader.H()||aquaticSchoolSize(lb)>=limit { continue }
		otherB:=other.(interface{behaviour()*AquaticMobBehaviour}).behaviour()
		otherB.schoolLeader=leader.H()
		otherB.schoolGroup=lb.schoolGroup
		lb.schoolGroup.size++
	}
	if leader.H()==e.handle { return nil }
	return leader
}

// tickAxolotl ports aquatic target sensing, attack cadence, and breeding behavior.
func (b *AquaticMobBehaviour) tickAxolotl(e *Ent, tx *world.Tx, velocity, partner mgl64.Vec3, inLove bool) mgl64.Vec3 {
	if b.attackCooldown > 0 { b.attackCooldown-- }
	if b.huntingCooldown > 0 { b.huntingCooldown-- }
	if b.playDeadTicks > 0 { return velocity }

	var target world.Entity
	if b.target != nil {
		if current, ok := b.target.Entity(tx); ok { target = current }
	}
	if living, ok := target.(Living); !ok || living.Dead() || !axolotlCanAttack(target, b.huntingCooldown == 0, tx) {
		b.target = nil
		target = nil
	}
	if target == nil {
		box := cube.Box(-8, -8, -8, 8, 8, 8).Translate(e.Position())
		best := 64.0
		for candidate := range tx.EntitiesWithin(box) {
			living, ok := candidate.(Living)
			if !ok || living.Dead() || candidate.H() == e.handle || !axolotlCanAttack(candidate, b.huntingCooldown == 0, tx) { continue }
			delta := candidate.Position().Sub(e.Position())
			distance := delta.LenSqr()
			if distance < best { target, best = candidate, distance }
		}
		if target != nil { b.target = target.H() }
	}
	if target == nil {
		if inLove {
			// AnimalMakeLove: the axolotl swims to its partner at a third
			// of the speed it chases prey with.
			return aquaticSteer(b, e, tx, partner, .1*.2/.6)
		}
		return velocity
	}

	delta := target.Position().Sub(e.Position())
	distance := delta.Len()
	if distance > 1.5 { return aquaticSteer(b,e,tx,target.Position(),.1) }
	if b.attackCooldown == 0 {
		living := target.(Living)
		living.Hurt(2, AttackDamageSource{Attacker: &AquaticMob{Ent: e}})
		b.attackCooldown = 20
		if living.Dead() {
			if _, hunts := axolotlHuntTargets[target.H().Type().EncodeEntity()]; hunts { b.huntingCooldown = 2400 }
			b.target = nil
		}
	}
	return velocity
}

func axolotlCanAttack(target world.Entity, huntAllowed bool, tx *world.Tx) bool {
	id := target.H().Type().EncodeEntity()
	_, hostile := axolotlAlwaysHostile[id]
	_, huntTarget := axolotlHuntTargets[id]
	if !hostile && (!huntAllowed || !huntTarget) { return false }
	_, inWater := tx.Liquid(cube.PosFromVec3(target.Position()))
	return inWater
}

func frogVariantAt(tx *world.Tx, pos mgl64.Vec3) int {
	for _, tag := range tx.Biome(cube.PosFromVec3(pos)).Tags() {
		if tag == "spawns_warm_variant_frogs" { return 1 }
		if tag == "spawns_cold_variant_frogs" { return 2 }
	}
	return 0
}
// FinalizeSpawn applies the shared AgeableMob group variant selection.
func (a *Axolotl) FinalizeSpawn(reason string, random *java26.Random) {
	if reason=="bucket"||random==nil { return }
	group:=a.H().SpawnGroup()
	var data *axolotlSpawnGroupData
	if group!=nil {
		data,_=group.Data.(*axolotlSpawnGroupData)
	}
	if data==nil {
		data=&axolotlSpawnGroupData{variants:[2]int{int(random.NextInt(4)),int(random.NextInt(4))}}
		if group!=nil { group.Data=data }
	}
	if data.size>=2 { a.behaviour().age=-24000 }
	a.behaviour().variant=data.variants[random.NextInt(2)]
	data.size++
}

func (c *Cod) FinalizeSpawn(_ string, _ *java26.Random) {
	c.behaviour().finalizeSchoolSpawn(c.AquaticMob)
}

// FinalizeSpawn chooses Salmon's weighted size variant.
func (s *Salmon) FinalizeSpawn(_ string, random *java26.Random) {
	if random==nil { return }
	roll:=random.NextInt(95)
	switch {
	case roll<30: s.behaviour().variant=0
	case roll<80: s.behaviour().variant=1
	default: s.behaviour().variant=2
	}
	s.behaviour().finalizeSchoolSpawn(s.AquaticMob)
}

// FinalizeSpawn preserves Tropical Fish's spawn-cohort variant and schooling leader.
func (f *TropicalFish) FinalizeSpawn(reason string, random *java26.Random) {
	if reason=="bucket"||random==nil { return }
	school:=f.behaviour().finalizeSchoolSpawn(f.AquaticMob)
	group:=f.H().SpawnGroup()
	if group!=nil {
		if data,ok:=group.Data.(*tropicalFishSchoolGroupData);ok {
			f.behaviour().variant=data.variant
			return
		}
	}
	variant:=0
	if random.NextFloat()<.9 {
		variant=int(tropicalFishCommonVariants[random.NextInt(int32(len(tropicalFishCommonVariants)))])
		if group!=nil { group.Data=&tropicalFishSchoolGroupData{school:school,variant:variant} }
	} else {
		pattern:=tropicalFishPatterns[random.NextInt(int32(len(tropicalFishPatterns)))]
		base,colour:=random.NextInt(16),random.NextInt(16)
		variant=int(pattern|base<<16|colour<<24)
	}
	f.behaviour().variant=variant
}
func randomAquaticGoal(b *AquaticMobBehaviour, e *Ent, tx *world.Tx) (mgl64.Vec3,bool) {
	box:=e.H().Type().BBox(aquaticWorldEntity(e))
	start:=cube.PosFromVec3(e.Position())
	for range 16 {
		p:=start.Add(cube.Pos{int(b.random.NextInt(21))-10,int(b.random.NextInt(15))-7,int(b.random.NextInt(21))-10})
		if aquaticNodeOpen(tx,p,box.Width(),box.Height()) { return p.Vec3().Add(mgl64.Vec3{.5,0,.5}),true }
	}
	return mgl64.Vec3{},false
}

func aquaticFlopSound(id string) string {
	switch id {
	case "minecraft:pufferfish": return "entity.puffer_fish.flop"
	case "minecraft:tropical_fish": return "entity.tropical_fish.flop"
	default: return "entity."+id[len("minecraft:"):]+".flop"
	}
}
func aquaticFleeTarget(b *AquaticMobBehaviour, e *Ent, tx *world.Tx) (mgl64.Vec3,float64,bool) {
	if b.id=="minecraft:axolotl" { return mgl64.Vec3{},0,false }
	if b.panicTicks>0 {
		b.panicTicks--
		direction:=e.Position().Sub(b.panicFrom)
		direction[1]=0
		if !b.panicFromSet||direction.LenSqr()==0 { direction=mgl64.Vec3{float64(b.random.NextFloat())*2-1,0,float64(b.random.NextFloat())*2-1} }
		if direction.LenSqr()==0 { return mgl64.Vec3{},0,false }
		goal:=e.Position().Add(direction.Normalize().Mul(8))
		bbox:=e.H().Type().BBox(aquaticWorldEntity(e))
		if !aquaticNodeOpen(tx,cube.PosFromVec3(goal),bbox.Width(),bbox.Height()) {
			if random,ok:=randomAquaticGoal(b,e,tx);ok { goal=random } else { return mgl64.Vec3{},0,false }
		}
		return goal,1.25,true
	}
	box:=cube.Box(-8,-8,-8,8,8,8).Translate(e.Position())
	var nearest world.Entity
	best:=64.0
	for candidate:=range tx.EntitiesWithin(box) {
		if candidate.H().Type().EncodeEntity()!="minecraft:player" { continue }
		if mode,ok:=candidate.(interface{GameMode() world.GameMode});ok&&!mode.GameMode().HasCollision() { continue }
		distance:=candidate.Position().Sub(e.Position()).LenSqr()
		if distance<best { nearest,best=candidate,distance }
	}
	if nearest==nil { return mgl64.Vec3{},0,false }
	direction:=e.Position().Sub(nearest.Position())
	direction[1]=0
	if direction.LenSqr()==0 { direction=mgl64.Vec3{1,0,0} }
	goal:=e.Position().Add(direction.Normalize().Mul(8))
	bbox:=e.H().Type().BBox(aquaticWorldEntity(e))
	if !aquaticNodeOpen(tx,cube.PosFromVec3(goal),bbox.Width(),bbox.Height()) {
		if random,ok:=randomAquaticGoal(b,e,tx);ok { goal=random } else { return mgl64.Vec3{},0,false }
	}
	speed:=1.6
	if best<16 { speed=1.4 }
	return goal,speed,true
}

func aquaticDamageAttacker(src world.DamageSource) world.Entity {
	switch damage:=src.(type) {
	case AttackDamageSource: return damage.Attacker
	case ProjectileDamageSource:
		if damage.Owner!=nil { return damage.Owner }
		return damage.Projectile
	case ExplosionDamageSource:
		entity, _ := damage.Source.(world.Entity)
		return entity
	default: return nil
	}
}
func pufferTargetAllowed(target world.Entity) bool {
	if target.H().Type().EncodeEntity()!="minecraft:player" { return true }
	if mode,ok:=target.(interface{GameMode() world.GameMode});ok {
		return !mode.GameMode().CreativeInventory()&&mode.GameMode().HasCollision()
	}
	return true
}
