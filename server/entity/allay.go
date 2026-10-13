package entity

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// AllayType is the Java minecraft:allay entity type.
var AllayType allayType

type allayType struct{}

func (allayType) Open(tx *world.Tx, h *world.EntityHandle, d *world.EntityData) world.Entity {
	if d.Data == nil {
		AllayBehaviourConfig{}.Apply(d)
	}
	return &Allay{Ent: &Ent{tx: tx, handle: h, data: d}}
}
func (allayType) EncodeEntity() string        { return "minecraft:allay" }
func (allayType) BBox(world.Entity) cube.BBox { return cube.Box(-.35, 0, -.35, .35, .6, .35) }
func (allayType) DecodeNBT(m map[string]any, d *world.EntityData) {
	c := AllayBehaviourConfig{}
	javaState := ReadNativeMobJavaState(m)
	source := m
	if javaState != nil {
		source = javaState
	}
	if _, ok := source["Health"]; ok {
		c.Health = float64(nbtconv.Float32(source, "Health"))
		c.healthSet = true
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.Health, c.healthSet = h.Current, true
	}
	if _, ok := source["MaxHealth"]; ok {
		c.MaxHealth = float64(nbtconv.Float32(source, "MaxHealth"))
	} else if h, ok := nativeMobAttribute(m, "minecraft:health"); ok {
		c.MaxHealth = h.Base
	}
	c.LikedPlayer = nbtconv.String(source, "LikedPlayer")
	if cooldown, ok := source["DuplicationCooldown"].(int64); ok {
		c.Cooldown = int(cooldown)
	} else {
		c.Cooldown = int(nbtconv.Int32(source, "DuplicationCooldown"))
	}
	c.Dancing = nbtconv.Bool(source, "Dancing")
	if in, ok := source["Inventory"].([]any); ok {
		for _, raw := range in {
			if tag, ok := raw.(map[string]any); ok {
				if s, err := item.StackFromJavaNBT(tag); err == nil && !s.Empty() {
					c.Inventory = append(c.Inventory, s)
				}
			}
		}
	} else {
		for _, native := range ReadNativeMobEquipment(m).ChestItems {
			if s, ok := allayNativeStack(native); ok {
				c.Inventory = append(c.Inventory, s)
			}
		}
	}
	if hand, ok := source["HandItems"].([]any); ok && len(hand) > 0 {
		if tag, ok := hand[0].(map[string]any); ok {
			c.Held, _ = item.StackFromJavaNBT(tag)
		}
	} else if tag, ok := source["HeldItem"].(map[string]any); ok {
		c.Held, _ = item.StackFromJavaNBT(tag)
	} else {
		equipment := ReadNativeMobEquipment(m)
		if len(equipment.Mainhand) > 0 {
			c.Held, _ = allayNativeStack(equipment.Mainhand[0])
		}
	}
	d.Data = c.New()
}
func (allayType) EncodeNBT(d *world.EntityData) map[string]any {
	b := d.Data.(*AllayBehaviour)
	attrs := EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT), []NativeMobAttribute{{Name: "minecraft:health", Base: b.health.MaxHealth(), Current: b.health.Health(), DefaultMin: 0, DefaultMax: b.health.MaxHealth(), Min: 0, Max: b.health.MaxHealth()}, {Name: "minecraft:movement", Base: .1, Current: .1, DefaultMin: 0, DefaultMax: .1, Min: 0, Max: .1}, {Name: "minecraft:attack_damage", Base: 2, Current: 2, DefaultMin: 0, DefaultMax: 2, Min: 0, Max: 2}})
	native := MergeNativeMobNBT(d.NativeNBT, map[string]any{"Attributes": attrs})
	equipment := ReadNativeMobEquipment(d.NativeNBT)
	if len(equipment.Mainhand) == 0 {
		equipment.Mainhand = []NativeMobItem{{}}
	}
	if len(equipment.Offhand) == 0 {
		equipment.Offhand = []NativeMobItem{{}}
	}
	equipment.Mainhand[0] = allayNativeItem(b.held, equipment.Mainhand[0])
	if len(equipment.ChestItems) == 0 {
		equipment.ChestItems = []NativeMobItem{{}}
	}
	slot := int32(0)
	equipment.ChestItems[0].Slot = &slot
	if len(b.inventory) > 0 {
		equipment.ChestItems[0] = allayNativeItem(b.inventory[0], equipment.ChestItems[0])
	} else {
		equipment.ChestItems[0] = allayNativeItem(item.Stack{}, equipment.ChestItems[0])
	}
	equipment.ChestItems[0].Slot = &slot
	native = MergeNativeMobNBT(native, EncodeNativeMobEquipment(equipment))
	inv := make([]any, 0, len(b.inventory))
	for _, s := range b.inventory {
		inv = append(inv, allayWriteStack(s))
	}
	java := MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT), map[string]any{"Health": float32(b.health.Health()), "MaxHealth": float32(b.health.MaxHealth()), "LikedPlayer": b.likedPlayer, "DuplicationCooldown": int64(b.duplicationCooldown), "Dancing": byte(catBool(b.dancing)), "Inventory": inv, "HandItems": []any{allayWriteStack(b.held), map[string]any{}}})
	return MergeNativeMobJavaState(native, java)
}
func allayNativeItem(s item.Stack, previous NativeMobItem) NativeMobItem {
	out := NativeMobItem{Raw: previous.Raw}
	if s.Empty() {
		out.Name = ""
		out.Count = 0
		out.Damage = 0
		out.WasPickedUp = 0
		return out
	}
	name, meta := s.Item().EncodeItem()
	out.Name = name
	out.Count = byte(s.Count())
	out.Damage = int32(meta)
	out.WasPickedUp = 0
	if s.MaxDurability() > 0 {
		out.Damage = int32(s.MaxDurability() - s.Durability())
	}
	return out
}
func allayNativeStack(s NativeMobItem) (item.Stack, bool) {
	if s.Name == "" || s.Count == 0 {
		return item.Stack{}, false
	}
	i, ok := world.ItemByName(s.Name, int16(s.Damage))
	if !ok {
		return item.Stack{}, false
	}
	return item.NewStack(i, int(s.Count)), true
}
func allayWriteStack(s item.Stack) map[string]any {
	raw := item.WriteNBT(s, true)
	id, ok := raw["Name"].(string)
	if !ok || s.Empty() {
		return map[string]any{}
	}
	out := map[string]any{"id": id, "count": int32(s.Count())}
	if tag, ok := raw["tag"].(map[string]any); ok {
		out["tag"] = tag
	}
	if damage, ok := raw["Damage"]; ok {
		out["components"] = map[string]any{"minecraft:damage": damage}
	}
	return out
}

type AllayBehaviourConfig struct {
	Health, MaxHealth float64
	LikedPlayer       string
	Cooldown          int
	Dancing           bool
	Inventory         []item.Stack
	Held              item.Stack
	healthSet         bool
}

func (c AllayBehaviourConfig) Apply(d *world.EntityData) { d.Data = c.New() }
func (c AllayBehaviourConfig) New() *AllayBehaviour {
	if c.MaxHealth <= 0 {
		c.MaxHealth = 20
	}
	if !c.healthSet && c.Health <= 0 {
		c.Health = c.MaxHealth
	}
	return &AllayBehaviour{health: NewHealthManager(c.Health, c.MaxHealth), effects: NewEffectManager(), mc: &MovementComputer{Gravity: 0, Drag: .02, DragBeforeGravity: true}, speed: .1, likedPlayer: c.LikedPlayer, duplicationCooldown: max(0, c.Cooldown), dancing: c.Dancing, inventory: c.Inventory, held: c.Held, nextPickup: 1}
}

type AllayBehaviour struct {
	health                     *HealthManager
	effects                    *EffectManager
	mc                         *MovementComputer
	speed                      float64
	likedPlayer                string
	duplicationCooldown, ticks int
	dancing, dead              bool
	inventory                  []item.Stack
	held                       item.Stack
	nextPickup                 int
	jukebox                    cube.Pos
	hasJukebox                 bool
	navigator                  Navigator
}
type Allay struct{ *Ent }

func NewAllay(opts world.EntitySpawnOpts) *world.EntityHandle {
	return opts.New(AllayType, AllayBehaviourConfig{})
}
func (a *Allay) SpeciesType() world.EntityType { return AllayType }
func (a *Allay) b() *AllayBehaviour            { return a.Behaviour().(*AllayBehaviour) }
func (a *Allay) Tick(tx *world.Tx, n int64) {
	b := a.b()
	b.effects.Tick(a, tx)
	b.ticks++
	if b.ticks%10 == 0 {
		a.Heal(1, nil)
	}
	canDuplicate := b.duplicationCooldown == 0
	if b.duplicationCooldown > 0 {
		b.duplicationCooldown--
	}
	if !canDuplicate && b.duplicationCooldown == 0 {
		a.updateState()
	}
	if b.dancing && a.isPanicking() {
		a.SetDancing(false)
	}
	if b.dancing && b.hasJukebox && a.Position().Sub(b.jukebox.Vec3()).Len() > 32 {
		b.hasJukebox = false
		a.SetDancing(false)
	}
	if b.nextPickup > 0 {
		b.nextPickup--
	}
	if b.nextPickup == 0 {
		b.nextPickup = 5
		a.collectMatching(tx)
	}
	a.deliverToLikedPlayer(tx)
	a.Ent.Tick(tx, n)
}
func (b *AllayBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	var target world.Entity
	if b.likedPlayer != "" {
		for p := range tx.Players() {
			if p.H().UUID().String() == b.likedPlayer {
				target = p
				break
			}
		}
	}
	if target != nil && target.Position().Sub(e.Position()).Len() > 3 {
		b.navigator.MoveTo(tx, cube.PosFromVec3(e.Position()), cube.PosFromVec3(target.Position()), GroundProfile{MaxVisitedNodes: 256, AllowWater: true}, navigatorTick(e))
	} else if b.held.Empty() && b.nextPickup == 0 {
		b.navigator.Path, b.navigator.Index = nil, 0
	}
	v := e.data.Vel
	r := e.data.Rot
	if p, ok := b.navigator.Tick(e.data.Pos, navigatorTick(e), .6); ok {
		p[1] += .5
		d := p.Sub(e.data.Pos)
		if d.Len() < .35 {
			b.navigator.Index++
		} else {
			d = d.Normalize()
			v = d.Mul(b.speed)
			r[0] = math.Atan2(-d[0], d[2]) * 180 / math.Pi
			r[1] = math.Atan2(d[1], math.Hypot(d[0], d[2])) * 180 / math.Pi
		}
	} else {
		v[0], v[2], v[1] = 0, 0, 0
	}
	return b.mc.TickMovement(e, e.data.Pos, v, r, e.data.Rot, tx)
}
func (a *Allay) HeldItems() (item.Stack, item.Stack) { return a.b().held, item.Stack{} }
func (a *Allay) SetHeldItems(main, off item.Stack)   { a.b().held = main; a.updateState() }
func (a *Allay) Health() float64                     { return a.b().health.Health() }
func (a *Allay) MaxHealth() float64                  { return a.b().health.MaxHealth() }
func (a *Allay) SetMaxHealth(v float64)              { a.b().health.SetMaxHealth(v) }
func (a *Allay) Dead() bool                          { return a.b().dead || a.Health() <= 0 }
func (a *Allay) Hurt(d float64, s world.DamageSource) (float64, bool) {
	b := a.b()
	if a.Dead() || d <= 0 || (s.Fire() && a.hasEffect(effect.FireResistance)) {
		return 0, false
	}
	if r, ok := b.effects.Effect(effect.Resistance); ok {
		d *= effect.Resistance.Multiplier(s, r.Level())
	}
	b.health.AddHealth(-d)
	a.viewAction(HurtAction{})
	if a.Dead() {
		a.die(s)
	}
	return d, true
}
func (a *Allay) Heal(h float64, _ world.HealingSource) float64 {
	if a.Dead() || h <= 0 {
		return 0
	}
	old := a.Health()
	a.b().health.AddHealth(h)
	return a.Health() - old
}
func (a *Allay) KnockBack(s mgl64.Vec3, f, h float64) {
	d := a.Position().Sub(s)
	if d.Len() != 0 {
		d = d.Normalize().Mul(f)
	}
	d[1] += h
	a.SetVelocity(d)
}
func (a *Allay) AddEffect(e effect.Effect)    { a.b().effects.Add(e, a) }
func (a *Allay) RemoveEffect(t effect.Type)   { a.b().effects.Remove(t, a) }
func (a *Allay) Effects() []effect.Effect     { return a.b().effects.Effects() }
func (a *Allay) Speed() float64               { return a.b().speed }
func (a *Allay) SetSpeed(v float64)           { a.b().speed = v }
func (a *Allay) hasEffect(t effect.Type) bool { _, ok := a.b().effects.Effect(t); return ok }
func (a *Allay) Dancing() bool                { return a.b().dancing }
func (a *Allay) SetDancing(v bool) {
	if (!v || !a.isPanicking()) && a.b().dancing != v {
		a.b().dancing = v
		a.updateState()
	}
}
func (a *Allay) isPanicking() bool { return a.OnFireDuration() > 0 }
func (a *Allay) SetJukeboxPlaying(pos cube.Pos, playing bool) {
	b := a.b()
	if playing {
		if !b.dancing {
			b.jukebox = pos
			b.hasJukebox = true
			a.SetDancing(true)
		}
	} else if !b.hasJukebox || b.jukebox == pos {
		b.hasJukebox = false
		a.SetDancing(false)
	}
}
func (a *Allay) CanDuplicate() bool { return a.b().duplicationCooldown == 0 }
func (a *Allay) AcceptItem(from world.Entity, tx *world.Tx, ctx *item.UseContext) bool {
	u, ok := from.(item.User)
	if !ok {
		return false
	}
	main, off := u.HeldItems()
	stack := main
	offHand := false
	if stack.Empty() {
		stack = off
		offHand = true
	}
	b := a.b()
	if b.dancing && a.CanDuplicate() && !stack.Empty() {
		if _, ok := stack.Item().(item.AmethystShard); ok {
			ctx.SubtractFromCount(1)
			b.duplicationCooldown = 6000
			a.updateState()
			tx.AddEntity(world.EntitySpawnOpts{Position: a.Position()}.New(AllayType, AllayBehaviourConfig{Cooldown: 6000}))
			return true
		}
	}
	if b.held.Empty() && !stack.Empty() {
		b.held = item.NewStack(stack.Item(), 1)
		b.likedPlayer = from.H().UUID().String()
		ctx.SubtractFromCount(1)
		a.updateState()
		return true
	}
	if !b.held.Empty() && main.Empty() && !offHand {
		ctx.NewItem = b.held
		ctx.ReplaceHeldItem = true
		a.dropInventory(tx)
		b.held = item.Stack{}
		b.likedPlayer = ""
		a.updateState()
		return true
	}
	return false
}
func (a *Allay) Interact(tx *world.Tx, u item.User, ctx *item.UseContext) bool {
	return a.AcceptItem(u, tx, ctx)
}
func (a *Allay) collectMatching(tx *world.Tx) {
	b := a.b()
	if b.held.Empty() {
		return
	}
	for e := range tx.Entities() {
		it, ok := e.(*Ent)
		if !ok || e == a {
			continue
		}
		ib, ok := it.Behaviour().(*ItemBehaviour)
		if !ok || e.Position().Sub(a.Position()).Len() > 1.75 {
			continue
		}
		s := ib.Item()
		if s.Empty() || !b.held.Comparable(s) || allayStoredCount(b)+s.Count() > b.held.MaxCount() {
			continue
		}
		if len(b.inventory) == 0 {
			b.inventory = append(b.inventory, s)
		} else {
			merged, left := b.inventory[0].AddStack(s)
			if !left.Empty() {
				continue
			}
			b.inventory[0] = merged
		}
		tx.RemoveEntity(e)
		return
	}
}
func allayStoredCount(b *AllayBehaviour) int {
	n := 0
	for _, s := range b.inventory {
		n += s.Count()
	}
	return n
}
func (a *Allay) deliverToLikedPlayer(tx *world.Tx) {
	b := a.b()
	if b.likedPlayer == "" || len(b.inventory) == 0 {
		return
	}
	for p := range tx.Players() {
		if p.H().UUID().String() != b.likedPlayer || p.Position().Sub(a.Position()).Len() > 3 {
			return
		}
		for _, s := range b.inventory {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: a.Position()}, s))
		}
		b.inventory = nil
		return
	}
}
func (a *Allay) dropInventory(tx *world.Tx) {
	b := a.b()
	for _, s := range b.inventory {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: a.Position()}, s))
	}
	b.inventory = nil
}
func (a *Allay) dropHeldAndInventory(tx *world.Tx) {
	b := a.b()
	if !b.held.Empty() {
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: a.Position()}, b.held))
		b.held = item.Stack{}
	}
	a.dropInventory(tx)
}
func (a *Allay) viewAction(e world.EntityAction) {
	vs := a.tx.Viewers(a.Position())
	for _, v := range vs {
		v.ViewEntityAction(a, e)
	}
	a.tx.ReleaseViewers(vs)
}
func (a *Allay) die(src world.DamageSource) {
	b := a.b()
	if b.dead {
		return
	}
	b.dead = true
	a.dropHeldAndInventory(a.tx)
	a.viewAction(DeathAction{})
	dropAllDeathLoot(a.tx, a, src)
	_ = a.CloseIn(a.tx)
}
func (a *Allay) DuplicationCooldown() time.Duration {
	return time.Duration(a.b().duplicationCooldown) * time.Second / 20
}

var _ Living = (*Allay)(nil)
var _ item.Carrier = (*Allay)(nil)
var _ ItemAcceptor = (*Allay)(nil)
