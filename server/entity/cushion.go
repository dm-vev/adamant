package entity

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// NewCushion creates a worldless cushion entity handle with a dye colour id.
func NewCushion(opts world.EntitySpawnOpts, color int) *world.EntityHandle {
	return opts.New(CushionType, CushionBehaviourConfig{Color: uint8(max(0, min(color, 15)))})
}

var CushionType cushionType
type cushionType struct{}
func (cushionType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	if data.Data == nil { CushionBehaviourConfig{}.Apply(data) }
	return &Cushion{Ent: &Ent{tx:tx, handle:handle, data:data}}
}
func (cushionType) EncodeEntity() string { return "minecraft:cushion" }
func (cushionType) BBox(world.Entity) cube.BBox { return cube.Box(-0.5,0,-0.5,0.5,0.25,0.5) }
func (cushionType) DecodeNBT(m map[string]any, data *world.EntityData) {
	conf := CushionBehaviourConfig{}
	if raw, ok := m["color"]; ok {
		switch v := raw.(type) {
		case string:
			for i, c := range item.Colours() { if c.String() == v { conf.Color = uint8(i); break } }
		case int8: conf.Color = uint8(v)
		case int32: conf.Color = uint8(v)
		}
	}
	data.Data = conf.New()
}
func (cushionType) EncodeNBT(data *world.EntityData) map[string]any {
	return map[string]any{"color": item.Colours()[data.Data.(*CushionBehaviour).color].String()}
}

// CushionBehaviourConfig configures the cushion's dye colour.
type CushionBehaviourConfig struct { Color uint8 }
func (c CushionBehaviourConfig) Apply(data *world.EntityData) { data.Data = c.New() }
func (c CushionBehaviourConfig) New() *CushionBehaviour { return &CushionBehaviour{stationary: StationaryBehaviourConfig{}.New(), color: min(c.Color, 15)} }
type CushionBehaviour struct { stationary *StationaryBehaviour; color uint8; passenger *world.EntityHandle }
func (b *CushionBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if !cushionHasSupport(cushionType{}.BBox(e).Translate(e.Position()),tx) {
		b.DismountAll(e,tx)
		tx.AddEntity(NewItem(world.EntitySpawnOpts{Position:e.Position()},item.NewStack(item.Cushion{Colour:item.Colours()[int(b.color)]},1)))
		_ = e.CloseIn(tx)
		return nil
	}
	if b.passenger != nil {
		passenger, ok := b.passenger.Entity(tx)
		if !ok { b.passenger = nil } else if rider, ok := passenger.(Rider); !ok || rider.Riding() != e.H() { b.passenger = nil } else if mover, ok := passenger.(interface{ Teleport(mgl64.Vec3) }); ok {
			mover.Teleport(e.Position().Add(mgl64.Vec3{0,0.25,0}))
		}
	}
	return b.stationary.Tick(e,tx)
}
func cushionHasSupport(box cube.BBox,tx *world.Tx) bool {
	anchor:=cube.Box(box.Min().X(),box.Min().Y()-0.125,box.Min().Z(),box.Max().X(),box.Min().Y(),box.Max().Z())
	minPos,maxPos:=anchor.Min(),anchor.Max()
	for x:=int(math.Floor(minPos[0]));x<=int(math.Floor(maxPos[0]));x++ { for y:=int(math.Floor(minPos[1]));y<=int(math.Floor(maxPos[1]));y++ { for z:=int(math.Floor(minPos[2]));z<=int(math.Floor(maxPos[2]));z++ {
		pos:=cube.Pos{x,y,z}
		for _,support:=range tx.Block(pos).Model().BBox(pos,tx) { if support.Translate(pos.Vec3()).IntersectsWith(anchor) { return true } }
	} } }
	return false
}
func (b *CushionBehaviour) DismountAll(e *Ent, tx *world.Tx) {
	if b.passenger == nil { return }
	h := b.passenger
	b.passenger = nil
	if passenger, ok := h.Entity(tx); ok { if r, ok := passenger.(Rider); ok { r.SetRiding(nil) } }
	viewers := tx.Viewers(e.Position())
	for _, v := range viewers { if lv,ok:=v.(interface{ViewEntityLinkType(*world.EntityHandle,*world.EntityHandle,LinkType,bool)});ok { lv.ViewEntityLinkType(e.H(),h,LinkPassenger,false) } else if lv,ok:=v.(interface{ViewEntityLink(*world.EntityHandle,*world.EntityHandle,bool)});ok { lv.ViewEntityLink(e.H(),h,false) } }
	tx.ReleaseViewers(viewers)
}

// Cushion is a small rideable decorative entity.
type Cushion struct { *Ent }
func (c *Cushion) Color() uint8 { return c.Behaviour().(*CushionBehaviour).color }
func (c *Cushion) Interact(tx *world.Tx, user item.User, _ *item.UseContext) bool {
	return c.Mount(tx,user)
}
func (c *Cushion) Mount(tx *world.Tx, passenger world.Entity) bool {
	b := c.Behaviour().(*CushionBehaviour)
	if passenger == nil || b.passenger != nil { return false }
	r, ok := passenger.(Rider)
	if !ok || r.Riding() != nil { return false }
	b.passenger = passenger.H()
	r.SetRiding(c.H())
	c.broadcastLink(tx,passenger.H(),true)
	if mover,ok:=passenger.(interface{Teleport(mgl64.Vec3)});ok { mover.Teleport(c.Position().Add(mgl64.Vec3{0,0.25,0})) }
	return true
}
func (c *Cushion) Dismount(tx *world.Tx, passenger world.Entity) {
	b:=c.Behaviour().(*CushionBehaviour)
	if passenger==nil || b.passenger==nil || passenger.H()!=b.passenger { return }
	h:=b.passenger; b.passenger=nil
	if r,ok:=passenger.(Rider);ok { r.SetRiding(nil) }
	c.broadcastLink(tx,h,false)
}
func (c *Cushion) Passenger() *world.EntityHandle { return c.Behaviour().(*CushionBehaviour).passenger }
func (c *Cushion) Links() []Link {
	if h:=c.Passenger();h!=nil { return []Link{{Ridden:c.H(),Rider:h,Type:LinkPassenger}} }
	return nil
}
func (c *Cushion) broadcastLink(tx *world.Tx, rider *world.EntityHandle, mounted bool) {
	viewers:=tx.Viewers(c.Position())
	for _,v:=range viewers { if lv,ok:=v.(interface{ViewEntityLinkType(*world.EntityHandle,*world.EntityHandle,LinkType,bool)});ok { lv.ViewEntityLinkType(c.H(),rider,LinkPassenger,mounted) } else if lv,ok:=v.(interface{ViewEntityLink(*world.EntityHandle,*world.EntityHandle,bool)});ok { lv.ViewEntityLink(c.H(),rider,mounted) } }
	tx.ReleaseViewers(viewers)
}
func (c *Cushion) Destroy(tx *world.Tx, _ world.DamageSource, _ world.Entity) bool {
	b := c.Behaviour().(*CushionBehaviour)
	b.DismountAll(c.Ent, tx)
	tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: c.Position()}, item.NewStack(item.Cushion{Colour: item.Colours()[int(b.color)]}, 1)))
	_ = c.CloseIn(tx)
	return true
}
