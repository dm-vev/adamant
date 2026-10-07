package entity

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type PiglinBruteType struct{}
type PiglinBrute struct{*Piglin}
func(PiglinBruteType)EncodeEntity()string{return "minecraft:piglin_brute"}
func(PiglinBruteType)Open(tx *world.Tx,h *world.EntityHandle,d *world.EntityData)world.Entity{if d.Data==nil{(piglinConfig{health:50,main:bruteAxe()}).Apply(d)};return &PiglinBrute{&Piglin{&Ent{tx:tx,handle:h,data:d}}}}
func(PiglinBruteType)BBox(world.Entity)cube.BBox{return cube.Box(-.3,0,-.3,.3,1.95,.3)}
func(PiglinBruteType)DecodeNBT(n map[string]any,d *world.EntityData){j:=ReadNativeMobJavaState(n);if j==nil{j=n};eq:=ReadNativeMobEquipment(n);c:=piglinConfig{health:50,target:nbtconv.String(j,"AngerTarget"),attackCooldown:int(nbtconv.Int32(j,"AttackCooldown")),main:piglinStackFromNative(eq.Mainhand),homeSet:nbtconv.Bool(j,"HomeSet")};if x,ok:=nativeMobAttribute(n,"minecraft:health");ok{c.health=x.Current};if pos,ok:=j["HomePos"].([]int32);ok&&len(pos)==3{c.home=cube.Pos{int(pos[0]),int(pos[1]),int(pos[2])};c.homeSet=true};c.Apply(d)}
func(PiglinBruteType)EncodeNBT(d *world.EntityData)map[string]any{return encodePiglinNBT(d,50,true)}
func NewPiglinBrute(o world.EntitySpawnOpts)*world.EntityHandle{return o.New(PiglinBruteType{},piglinConfig{health:50,main:bruteAxe()})}
func bruteAxe()item.Stack{return item.NewStack(item.Axe{Tier:item.ToolTierGold},1)}
func(b *PiglinBrute)SpeciesType()world.EntityType{return PiglinBruteType{}}
func(b *PiglinBrute)FinalizeSpawn(_ string,_ *java26.Random){}
var _ world.EntityType=PiglinBruteType{}
