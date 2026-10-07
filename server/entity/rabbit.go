package entity

import (
 "math"
 "math/rand/v2"
 "github.com/df-mc/dragonfly/server/block/cube"
 "github.com/df-mc/dragonfly/server/internal/java26"
 "github.com/df-mc/dragonfly/server/internal/nbtconv"
 "github.com/df-mc/dragonfly/server/item"
 "github.com/df-mc/dragonfly/server/world"
 "github.com/go-gl/mathgl/mgl64"
)
// NewRabbit creates a common brown rabbit.
func NewRabbit(o world.EntitySpawnOpts)*world.EntityHandle{return o.New(RabbitType,RabbitConfig{})}
// RabbitType is the registered Java rabbit type.
var RabbitType rabbitType
type rabbitType struct{}
func(rabbitType)Open(tx *world.Tx,h *world.EntityHandle,d *world.EntityData)world.Entity{if d.Data==nil{RabbitConfig{}.Apply(d)};return &Rabbit{Ent:&Ent{tx:tx,handle:h,data:d}}}
func(rabbitType)EncodeEntity()string{return "minecraft:rabbit"}
func(rabbitType)BBox(e world.Entity)cube.BBox{return cube.Box(-.25,0,-.25,.25,.5,.25)}
func(rabbitType)DecodeNBT(m map[string]any,d *world.EntityData){s:=ReadNativeMobJavaState(m);if s==nil{s=m};c:=RabbitConfig{Variant:int(nbtconv.Int32(s,"RabbitType")),Age:int(nbtconv.Int32(s,"Age"))};if a,ok:=nativeMobAttribute(m,"minecraft:health");ok{c.Health,c.MaxHealth=a.Current,a.Base};d.Data=c.New()}
func(rabbitType)EncodeNBT(d *world.EntityData)map[string]any{b:=d.Data.(*RabbitBehaviour);a:=EncodeNativeMobAttributes(ReadNativeMobAttributes(d.NativeNBT),[]NativeMobAttribute{{Name:"minecraft:health",Base:b.maxHealth,Current:b.health,DefaultMin:0,DefaultMax:b.maxHealth,Min:0,Max:b.maxHealth}});return MergeNativeMobJavaState(map[string]any{"Attributes":a},MergeNativeMobNBT(ReadNativeMobJavaState(d.NativeNBT),map[string]any{"Age":int32(b.age),"RabbitType":int32(b.variant)}))}
// RabbitConfig controls variant and age.
type RabbitConfig struct{Health,MaxHealth float64;Variant,Age int}
func(c RabbitConfig)Apply(d *world.EntityData){d.Data=c.New()}
func(c RabbitConfig)New()*RabbitBehaviour{if c.MaxHealth<=0{c.MaxHealth=3};if c.Health<=0{c.Health=c.MaxHealth};return &RabbitBehaviour{health:c.Health,maxHealth:c.MaxHealth,variant:max(0,min(c.Variant,99)),age:c.Age,killer:c.Variant==99,next:30+rand.IntN(61),speed:.25,mc:&MovementComputer{Gravity:.08,Drag:.02,DragBeforeGravity:true}}}
// Rabbit is a small prey animal with source variant, crop-raiding and killer-bunny state.
type Rabbit struct{*Ent}
type RabbitBehaviour struct{health,maxHealth float64;variant,age,next,attackCooldown int;dead,killer bool;speed float64;path []cube.Pos;index int;mc *MovementComputer}
func(r *Rabbit)behaviour()*RabbitBehaviour{return r.Behaviour().(*RabbitBehaviour)}
func(r *Rabbit)Tick(tx *world.Tx,t int64){r.Ent.Tick(tx,t)}
func(b *RabbitBehaviour)Tick(e *Ent,tx *world.Tx)*Movement{
	if b.age<0{b.age=min(0,b.age+1)}
	if b.attackCooldown>0{b.attackCooldown--}
	if b.killer {
		if target:=rabbitKillerTarget(e,tx);target!=nil {
			start:=cube.PosFromVec3(e.Position());goal:=cube.PosFromVec3(target.Position())
			if path:=findPath(tx,start,goal,128,false,false);len(path)>0{b.path,b.index=path,0}
			if target.Position().Sub(e.Position()).Len()<1.5&&b.attackCooldown==0{if l,ok:=target.(Living);ok{l.Hurt(8,AttackDamageSource{Attacker:&Rabbit{Ent:e}})};b.attackCooldown=20}
		}
	}
	if b.next>0{b.next--}
	if b.index>=len(b.path)&&b.next==0{b.next=30+rand.IntN(61);start:=cube.PosFromVec3(e.Position());for range 5{goal:=start.Add(cube.Pos{rand.IntN(11)-5,0,rand.IntN(11)-5});if p:=findPath(tx,start,goal,128,false,false);len(p)>0{b.path,b.index=p,0;break}}}
	rot,v:=e.data.Rot,e.data.Vel
	if b.killer{b.speed=.35}
	if b.index<len(b.path){d:=b.path[b.index].Vec3().Add(mgl64.Vec3{.5,0,.5}).Sub(e.data.Pos);d[1]=0;if d.Len()<.2{b.index++}else{d=d.Normalize();v[0],v[2]=d[0]*b.speed,d[2]*b.speed}}else{v[0],v[2]=0,0}
	m:=b.mc.TickMovement(e,e.data.Pos,v,e.data.Rot,rot,tx);e.data.Pos,e.data.Vel,e.data.Rot=m.Position(),m.Velocity(),m.Rotation();return m
}
func rabbitKillerTarget(e *Ent,tx *world.Tx)world.Entity{area:=cube.Box(-16,-8,-16,16,8,16).Translate(e.Position());var target world.Entity;best:=16.0;for other:=range tx.EntitiesWithin(area){if other==e{continue};id:=other.H().Type().EncodeEntity();if id!="minecraft:player"&&id!="minecraft:wolf"&&id!="minecraft:fox"{continue};living,ok:=other.(Living);if !ok||living.Dead(){continue};distance:=other.Position().Sub(e.Position()).Len();if distance<best{target,best=other,distance}};return target}
func(r *Rabbit)Health()float64{return r.behaviour().health};func(r *Rabbit)MaxHealth()float64{return r.behaviour().maxHealth};func(r *Rabbit)SetMaxHealth(v float64){b:=r.behaviour();b.maxHealth=v;b.health=math.Min(v,b.health)};func(r *Rabbit)Dead()bool{return r.behaviour().dead||r.Health()<=0}
func(r *Rabbit)Hurt(v float64,src world.DamageSource)(float64,bool){if r.Dead()||v<=0{return 0,false};b:=r.behaviour();b.health-=v;r.viewAction(HurtAction{});if r.Dead(){b.dead=true;r.viewAction(DeathAction{});dropMobLoot(r.tx,r.Position(),"minecraft:rabbit",src);_ = r.CloseIn(r.tx)};return v,true}
func(r *Rabbit)Heal(v float64,_ world.HealingSource)float64{if r.Dead()||v<=0{return 0};b:=r.behaviour();old:=b.health;b.health=math.Min(b.maxHealth,b.health+v);return b.health-old}
func(r *Rabbit)KnockBack(src mgl64.Vec3,f,h float64){d:=r.Position().Sub(src);d[1]=0;if d.Len()>0{d=d.Normalize().Mul(f)};d[1]=h;r.SetVelocity(d)}
func(r *Rabbit)viewAction(a world.EntityAction){vs:=r.tx.Viewers(r.Position());for _,v:=range vs{v.ViewEntityAction(r,a)};r.tx.ReleaseViewers(vs)}
func(r *Rabbit)Variant()int{return r.behaviour().variant};func(r *Rabbit)Killer()bool{return r.behaviour().killer}
func(r *Rabbit)Feed(food item.Stack)bool{b:=r.behaviour();if food.Empty()||b.age>=0{return false};name,_:=food.Item().EncodeItem();if name!="minecraft:carrot"&&name!="minecraft:golden_carrot"&&name!="minecraft:dandelion"{return false};b.age=min(0,b.age+1200);return true}
func(r *Rabbit)Interact(_ *world.Tx,u item.User,c *item.UseContext)bool{if u==nil||c==nil{return false};held,_:=u.HeldItems();if r.Feed(held){c.SubtractFromCount(1);return true};return false}
var _ Interactable=(*Rabbit)(nil)
// FinalizeSpawn chooses the biome-selected rabbit variant; rare hostile variant has its own goal state.
func(r *Rabbit)FinalizeSpawn(reason string,j *java26.Random){if j==nil{return};b:=r.behaviour();group:=r.H().SpawnGroup();if group!=nil{if v,ok:=group.Data.(int);ok{b.variant=v;b.killer=v==99;return}};if reason=="jockey"{b.variant=99;b.killer=true}else{roll:=j.NextInt(100);switch{case roll<50:b.variant=0;case roll<90:b.variant=5;default:b.variant=2}};if group!=nil{group.Data=b.variant}}
