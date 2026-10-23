package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/vanilla"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// HappyGhastType is the source happy ghast species. Babies use the same native
// actor identity; age and all lifecycle state are persisted as Java state.
var HappyGhastType world.EntityType = happyGhastType{}

type happyGhastType struct{}

func (happyGhastType) EncodeEntity() string { return "minecraft:happy_ghast" }
func (happyGhastType) BBox(e world.Entity) cube.BBox {
	baby := false
	if h, ok := e.(*HappyGhast); ok {
		baby = h.state().age < 0
	}
	w, high := 4.0, 4.0
	if baby {
		w, high = .95, .95
	}
	return cube.Box(-w/2, 0, -w/2, w/2, high, w/2)
}
func (happyGhastType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		happyGhastConfig{}.Apply(d)
	}
	return &HappyGhast{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (happyGhastType) DecodeNBT(n map[string]any, d *world.EntityData) {
	c := happyGhastConfig{}
	s := ReadNativeMobJavaState(n)
	if s == nil {
		s = n
	}
	c.age = int(nbtconv.Int32(s, "Age"))
	c.aiTick = int(nbtconv.Int32(s, "AiTick"))
	c.hasTarget = nbtconv.Bool(s, "HasTarget")
	c.target = mgl64.Vec3{float64(nbtconv.Float32(s, "TargetX")), float64(nbtconv.Float32(s, "TargetY")), float64(nbtconv.Float32(s, "TargetZ"))}
	c.harness = nbtconv.Bool(s, "Harnessed")
	c.harnessName = nbtconv.String(s, "HarnessName")
	if c.harness && c.harnessName == "" {
		c.harnessName = "minecraft:white_harness"
	}
	c.still = int(nbtconv.Int32(s, "StillTimeout"))
	c.healTick = int(nbtconv.Int32(s, "HealTick"))
	c.home = cube.Pos{int(nbtconv.Int32(s, "HomeX")), int(nbtconv.Int32(s, "HomeY")), int(nbtconv.Int32(s, "HomeZ"))}
	c.hasHome = nbtconv.Bool(s, "HasHome")
	if _, ok := s["Health"]; ok {
		c.health = float64(nbtconv.Float32(s, "Health"))
	} else if a, ok := nativeMobAttribute(n, "minecraft:health"); ok {
		c.health = a.Current
	}
	c.Apply(d)
}
func (happyGhastType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*happyGhastBehaviour)
	h := b.health
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: 20, Current: h.Health(), DefaultMin: 0, DefaultMax: 20, Min: 0, Max: 20}, {Name: "minecraft:movement", Base: .05, Current: .05, DefaultMin: 0, DefaultMax: .05, Min: 0, Max: .05}})
	m := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	state := map[string]any{"Age": int32(b.age), "AiTick": int32(b.aiTick), "Harnessed": byte(hgBool(b.harness)), "HarnessName": b.harnessName, "StillTimeout": int32(b.still), "HealTick": int32(b.healTick), "HasHome": byte(hgBool(b.hasHome)), "HomeX": int32(b.home[0]), "HomeY": int32(b.home[1]), "HomeZ": int32(b.home[2]), "HasTarget": byte(hgBool(b.hasTarget)), "TargetX": float32(b.target[0]), "TargetY": float32(b.target[1]), "TargetZ": float32(b.target[2]), "Health": float32(h.Health())}
	return MergeNativeMobJavaState(m, state)
}

type happyGhastConfig struct {
	age, still, healTick, aiTick int
	harness, hasHome, hasTarget  bool
	harnessName                  string
	home                         cube.Pos
	target                       mgl64.Vec3
	health                       float64
}

func (c happyGhastConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c happyGhastConfig) New() *happyGhastBehaviour {
	if c.health <= 0 {
		c.health = 20
	}
	return &happyGhastBehaviour{health: NewHealthManager(c.health, 20), effects: NewEffectManager(), move: PassiveBehaviourConfig{Gravity: 0, Drag: .01}.New(), speed: .05, age: c.age, still: c.still, healTick: c.healTick, aiTick: c.aiTick, harness: c.harness, harnessName: c.harnessName, home: c.home, hasHome: c.hasHome, target: c.target, hasTarget: c.hasTarget}
}

type happyGhastBehaviour struct {
	health                       *HealthManager
	effects                      *EffectManager
	move                         *PassiveBehaviour
	speed                        float64
	age, still, healTick, aiTick int
	harness                      bool
	harnessName                  string
	riders                       [4]*world.EntityHandle
	strafe, forward, up          float64
	home                         cube.Pos
	hasHome, hasTarget           bool
	target                       mgl64.Vec3
}
type HappyGhast struct{ *Ent }

func NewHappyGhast(o world.EntitySpawnOpts) *world.EntityHandle {
	return o.New(HappyGhastType, happyGhastConfig{})
}
func (*HappyGhast) SpeciesType() world.EntityType { return HappyGhastType }

// FinalizeSpawn initializes the source-created ghastling as a baby before it is exposed to viewers.
func (h *HappyGhast) FinalizeSpawn(reason string, _ *vanilla.Random) {
	if reason == "breeding" {
		h.SetAge(-24000)
	}
}
func (h *HappyGhast) state() *happyGhastBehaviour { return h.Behaviour().(*happyGhastBehaviour) }
func (h *HappyGhast) Tick(tx *world.Tx, n int64)  { h.state().effects.Tick(h, tx); h.Ent.Tick(tx, n) }
func (b *happyGhastBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.age < 0 {
		b.age++
		if b.age >= 0 {
			b.age = 0
		}
	}
	b.aiTick++
	if b.still > 0 {
		b.still--
	}
	if b.harness {
		b.restoreRiders(e, tx)
	}
	if !b.hasHome && !b.hasRiders() {
		b.home = cube.PosFromVec3(e.Position())
		b.hasHome = true
	}
	if !b.hasRiders() {
		b.followNearbyAdult(e, tx)
	}
	if !b.hasRiders() && (!b.hasTarget || (b.age >= 0 && b.aiTick%800 == 0)) {
		b.pickIdlePoint()
	}
	if !b.hasRiders() {
		b.steer(e)
	} else {
		b.followControl(e, tx)
	}
	if b.healTick > 0 {
		b.healTick--
	}
	interval := 600
	if tx.RainingAt(cube.PosFromVec3(e.Position())) {
		interval = 20
	}
	if b.health.Health() < b.health.MaxHealth() && b.healTick <= 0 {
		b.health.AddHealth(1)
		b.healTick = interval
	}
	m := b.move.Tick(e, tx)
	b.updateRiders(e, tx)
	return m
}
func (b *happyGhastBehaviour) followNearbyAdult(e *Ent, tx *world.Tx) {
	if b.age >= 0 {
		return
	}
	box := cube.Box(-32, -16, -32, 32, 16, 32).Translate(e.Position())
	var adult world.Entity
	best := math.MaxFloat64
	for candidate := range tx.EntitiesWithin(box) {
		h, ok := candidate.(*HappyGhast)
		if !ok || h.H() == e.H() || h.Baby() {
			continue
		}
		d := h.Position().Sub(e.Position()).LenSqr()
		if d < best {
			best = d
			adult = h
		}
	}
	if adult != nil {
		b.target = adult.Position().Add(mgl64.Vec3{0, -3, 0})
		b.hasTarget = true
	}
}
func (b *happyGhastBehaviour) pickIdlePoint() {
	if !b.hasHome {
		return
	}
	t := float64(b.aiTick) / 800
	radius := 12.0
	if b.age < 0 {
		radius = 8
	}
	b.target = b.home.Vec3Centre().Add(mgl64.Vec3{math.Sin(t) * radius, math.Sin(t*.7) * 2, math.Cos(t) * radius})
	b.hasTarget = true
}
func (b *happyGhastBehaviour) steer(e *Ent) {
	if !b.hasTarget {
		return
	}
	d := b.target.Sub(e.Position())
	if d.LenSqr() < .5 {
		return
	}
	accel := d.Normalize().Mul(b.speed * .06)
	e.data.Vel = e.data.Vel.Add(accel)
	maxSpeed := b.speed * 6
	if e.data.Vel.LenSqr() > maxSpeed*maxSpeed {
		e.data.Vel = e.data.Vel.Normalize().Mul(maxSpeed)
	}
	yaw := math.Atan2(d[0], d[2]) * 180 / math.Pi
	delta := math.Mod(yaw-e.data.Rot[0]+540, 360) - 180
	e.data.Rot[0] += delta * .08
}
func (b *happyGhastBehaviour) hasRiders() bool {
	for _, h := range b.riders {
		if h != nil {
			return true
		}
	}
	return false
}
func (b *happyGhastBehaviour) restoreRiders(e *Ent, tx *world.Tx) {
	if b.hasRiders() {
		return
	}
	box := cube.Box(-9, 0, -9, 9, 6, 9).Translate(e.Position())
	for rider := range tx.EntitiesWithin(box) {
		r, ok := rider.(Rider)
		if !ok || r.Riding() != e.H() {
			continue
		}
		for i := range b.riders {
			if b.riders[i] == nil {
				b.riders[i] = rider.H()
				break
			}
		}
	}
}
func (b *happyGhastBehaviour) updateRiders(e *Ent, tx *world.Tx) {
	for i, h := range b.riders {
		if h == nil {
			continue
		}
		p, ok := h.Entity(tx)
		if !ok {
			b.riders[i] = nil
			continue
		}
		r, ok := p.(Rider)
		if !ok || r.Riding() != e.H() {
			b.riders[i] = nil
			continue
		}
		angle := float64(i) * math.Pi / 2
		off := mgl64.Vec3{-math.Sin(angle) * 1.7, 4, math.Cos(angle) * 1.7}
		if t, ok := p.(interface{ Teleport(mgl64.Vec3) }); ok {
			t.Teleport(e.Position().Add(off))
		}
	}
}
func (b *happyGhastBehaviour) followControl(e *Ent, tx *world.Tx) {
	if b.riders[0] == nil {
		return
	}
	r, ok := b.riders[0].Entity(tx)
	if !ok {
		return
	}
	rot := e.Rotation()
	yaw := rot[0] * math.Pi / 180
	forward := mgl64.Vec3{math.Sin(yaw), 0, math.Cos(yaw)}
	side := mgl64.Vec3{forward[2], 0, -forward[0]}
	e.data.Vel = e.data.Vel.Add(side.Mul(b.strafe * .02).Add(forward.Mul(b.forward * .02)).Add(mgl64.Vec3{0, b.up * .02, 0}))
	if e.data.Vel.LenSqr() > .09 {
		e.data.Vel = e.data.Vel.Normalize().Mul(.3)
	}
	targetYaw := r.Rotation().Yaw()
	delta := math.Mod(targetYaw-e.data.Rot[0]+540, 360) - 180
	e.data.Rot[0] += delta * .08
	e.data.Rot[1] = r.Rotation().Pitch() * .5
	b.strafe, b.forward, b.up = 0, 0, 0
}
func (h *HappyGhast) SetVehicleInputFrom(passenger world.Entity, strafe, forward float64) {
	b := h.state()
	if passenger == nil || !h.ControlsVehicle(passenger) {
		b.strafe, b.forward, b.up = 0, 0, 0
		return
	}
	pitch := passenger.Rotation().Pitch() * math.Pi / 180
	lookForward := math.Cos(pitch)
	up := -math.Sin(pitch)
	if forward < 0 {
		lookForward *= .5
		up *= .5
	}
	b.strafe = strafe
	b.forward = forward * lookForward
	b.up = forward * up
}
func (h *HappyGhast) SetVehicleInput(strafe, forward float64) {
	b := h.state()
	b.strafe = strafe
	b.forward = forward
}
func (h *HappyGhast) ControlsVehicle(passenger world.Entity) bool {
	b := h.state()
	return passenger != nil && b.harness && b.still == 0 && b.riders[0] != nil && b.riders[0] == passenger.H()
}
func (h *HappyGhast) Mount(tx *world.Tx, p world.Entity) bool {
	if p == nil {
		return false
	}
	r, ok := p.(Rider)
	if !ok || r.Riding() != nil {
		return false
	}
	b := h.state()
	if !b.harness || b.age < 0 {
		return false
	}
	for i, x := range b.riders {
		if x == nil {
			b.riders[i] = p.H()
			b.still = 0
			seatRider(r, h)
			h.viewLink(tx, p.H(), true)
			return true
		}
	}
	return false
}
func (h *HappyGhast) Dismount(tx *world.Tx, p world.Entity) {
	if p == nil {
		return
	}
	b := h.state()
	for i, x := range b.riders {
		if x == p.H() {
			b.riders[i] = nil
			if r, ok := p.(Rider); ok {
				r.SetRiding(nil)
			}
			h.viewLink(tx, x, false)
			b.still = 10
		}
	}
}

// happyGhastSeats are the seats of Bedrock's harnessed happy ghast: riders
// fill them front, left, back and right.
var happyGhastSeats = [len(happyGhastBehaviour{}.riders)]mgl64.Vec3{{0, 3.8, 1.7}, {-1.7, 3.8, 0}, {0, 3.8, -1.7}, {1.7, 3.8, 0}}

// RiderSeat returns the seat rider fills on the harness.
func (h *HappyGhast) RiderSeat(rider world.Entity) world.RiderSeat {
	pos := happyGhastSeats[0]
	for i, x := range h.state().riders {
		if x == rider.H() {
			pos = happyGhastSeats[i]
		}
	}
	return seat{pos: pos}.riderSeat(rider)
}
func (h *HappyGhast) viewLink(tx *world.Tx, r *world.EntityHandle, on bool) {
	linkType := LinkPassenger
	if h.state().riders[0] == r {
		linkType = LinkRider
	}
	vs := tx.Viewers(h.Position())
	for _, v := range vs {
		if lv, ok := v.(interface {
			ViewEntityLinkType(*world.EntityHandle, *world.EntityHandle, LinkType, bool)
		}); ok {
			lv.ViewEntityLinkType(h.H(), r, linkType, on)
		} else if lv, ok := v.(interface {
			ViewEntityLink(*world.EntityHandle, *world.EntityHandle, bool)
		}); ok {
			lv.ViewEntityLink(h.H(), r, on)
		}
	}
	tx.ReleaseViewers(vs)
}
func (h *HappyGhast) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	if u == nil {
		return false
	}
	b := h.state()
	held, _ := u.HeldItems()
	if held.Empty() {
		if b.age < 0 {
			return false
		}
		return h.Mount(tx, u)
	}
	name, _ := held.Item().EncodeItem()
	if name == "minecraft:snowball" {
		if b.age < 0 {
			b.age += 2400
			if b.age > 0 {
				b.age = 0
			}
		} else if b.health.Health() < 20 {
			b.health.AddHealth(2)
		} else {
			return false
		}
		if ctx != nil {
			ctx.SubtractFromCount(1)
		}
		return true
	}
	return false
}
func (h *HappyGhast) CanEquipHarness(_ world.Entity) bool {
	return h.state().age >= 0 && !h.state().harness
}
func (h *HappyGhast) EquipHarness(tx *world.Tx, _ world.Entity, i world.Item) bool {
	if i == nil || !h.CanEquipHarness(nil) {
		return false
	}
	name, _ := i.EncodeItem()
	if _, ok := world.ItemByName(name, 0); !ok {
		return false
	}
	b := h.state()
	b.harness = true
	b.harnessName = name
	h.viewArmour(tx)
	return true
}
func (h *HappyGhast) ShearHarness(tx *world.Tx, _ world.Entity) bool {
	b := h.state()
	if !b.harness {
		return false
	}
	for _, passenger := range b.riders {
		if passenger != nil {
			if p, ok := passenger.Entity(tx); ok {
				h.Dismount(tx, p)
			}
		}
	}
	if b.harnessName != "" {
		if it, ok := world.ItemByName(b.harnessName, 0); ok {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: h.Position()}, item.NewStack(it, 1)))
		}
	}
	b.harness = false
	b.harnessName = ""
	h.viewArmour(tx)
	return true
}
func (h *HappyGhast) Harnessed() bool        { return h.state().harness }
func (h *HappyGhast) Baby() bool           { return h.state().age < 0 }
func (h *HappyGhast) AgeTicks() int          { return h.state().age }
func (h *HappyGhast) Health() float64        { return h.state().health.Health() }
func (h *HappyGhast) MaxHealth() float64     { return h.state().health.MaxHealth() }
func (h *HappyGhast) SetMaxHealth(v float64) { h.state().health.SetMaxHealth(v) }
func (h *HappyGhast) Dead() bool             { return h.Health() <= 0 }
func (h *HappyGhast) Heal(v float64, _ world.HealingSource) float64 {
	if v <= 0 || h.Dead() {
		return 0
	}
	old := h.Health()
	h.state().health.AddHealth(v)
	return h.Health() - old
}
func (h *HappyGhast) Hurt(v float64, src world.DamageSource) (float64, bool) {
	b := h.state()
	if v <= 0 || h.Dead() || (src.Fire() && h.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if resistance, ok := b.effects.Effect(effect.Resistance); ok {
		v *= effect.Resistance.Multiplier(src, resistance.Level())
	}
	old := h.Health()
	b.health.AddHealth(-v)
	onMobHurt(h.tx, h)
	if h.Dead() {
		h.viewAction(DeathAction{})
		dropAllDeathLoot(h.tx, h, src)
		_ = h.CloseIn(h.tx)
	} else {
		h.viewAction(HurtAction{})
	}
	return old - h.Health(), true
}
func (h *HappyGhast) KnockBack(src mgl64.Vec3, f, height float64) {
	d := h.Position().Sub(src)
	if d.LenSqr() == 0 {
		return
	}
	h.SetVelocity(h.Velocity().Add(d.Normalize().Mul(f).Add(mgl64.Vec3{0, height, 0})))
}
func (h *HappyGhast) Speed() float64               { return h.state().speed }
func (h *HappyGhast) SetSpeed(v float64)           { h.state().speed = v }
func (h *HappyGhast) AddEffect(e effect.Effect)    { h.state().effects.Add(e, h) }
func (h *HappyGhast) RemoveEffect(t effect.Type)   { h.state().effects.Remove(t, h) }
func (h *HappyGhast) Effects() []effect.Effect     { return h.state().effects.Effects() }
func (h *HappyGhast) hasEffect(t effect.Type) bool { _, ok := h.state().effects.Effect(t); return ok }
func (h *HappyGhast) SetAge(age int) {
	b := h.state()
	if age > 0 {
		age = 0
	}
	b.age = age
}
func (h *HappyGhast) EquipmentArmour() (item.Stack, item.Stack, item.Stack, item.Stack) {
	return item.Stack{}, item.Stack{}, item.Stack{}, item.Stack{}
}
func (h *HappyGhast) BodyItem() item.Stack {
	b := h.state()
	if !b.harness || b.harnessName == "" {
		return item.Stack{}
	}
	i, ok := world.ItemByName(b.harnessName, 0)
	if !ok {
		return item.Stack{}
	}
	return item.NewStack(i, 1)
}
func hgBool(v bool) byte {
	if v {
		return 1
	}
	return 0
}
func (h *HappyGhast) viewAction(a world.EntityAction) {
	vs := h.tx.Viewers(h.Position())
	for _, v := range vs {
		v.ViewEntityAction(h, a)
	}
	h.tx.ReleaseViewers(vs)
}
func (h *HappyGhast) viewArmour(tx *world.Tx) {
	vs := tx.Viewers(h.Position())
	for _, v := range vs {
		v.ViewEntityArmour(h)
	}
	tx.ReleaseViewers(vs)
}

var _ Living = (*HappyGhast)(nil)
var _ VehicleInput = (*HappyGhast)(nil)
var _ VehicleController = (*HappyGhast)(nil)
var _ PassengerDismounter = (*HappyGhast)(nil)
var _ Harnessable = (*HappyGhast)(nil)
var _ Interactable = (*HappyGhast)(nil)
