package block

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

var hashTurtleEgg = NextHash()

// TurtleEgg stores the clutch size and hatch stage of Java turtle eggs.
type TurtleEgg struct {
	transparent
	sourceWaterDisplacer
	// Eggs is the clutch size in the range 1..4.
	Eggs uint8
	// Hatch is the crack stage in the range 0..2.
	Hatch uint8
}

func (e TurtleEgg) normalized() TurtleEgg {
	if e.Eggs < 1 { e.Eggs = 1 }
	if e.Eggs > 4 { e.Eggs = 4 }
	if e.Hatch > 2 { e.Hatch = 2 }
	return e
}
func (e TurtleEgg) Permutations() []world.Block {
	states := make([]world.Block, 0, 12)
	for eggs := uint8(1); eggs <= 4; eggs++ { for hatch := uint8(0); hatch <= 2; hatch++ { states = append(states, TurtleEgg{Eggs:eggs,Hatch:hatch}) } }
	return states
}
func (e TurtleEgg) Model() world.BlockModel { return turtleEggModel{multiple:e.normalized().Eggs>1} }
type turtleEggModel struct{multiple bool}
func (m turtleEggModel) BBox(cube.Pos,world.BlockSource)[]cube.BBox { if m.multiple{return []cube.BBox{cube.Box(.0625,0,.0625,.9375,.4375,.9375)}};return []cube.BBox{cube.Box(.1875,0,.1875,.75,.4375,.75)} }
func (turtleEggModel) FaceSolid(cube.Pos,cube.Face,world.BlockSource)bool{return false}
func (e TurtleEgg) BreakInfo() BreakInfo { return newBreakInfo(.75,alwaysHarvestable,nothingEffective,silkTouchOnlyDrop(e.normalized())) }
func (TurtleEgg) EncodeItem()(string,int16){return "minecraft:turtle_egg",0}
func (e TurtleEgg) EncodeBlock()(string,map[string]any){e=e.normalized();counts:=[...]string{"","one_egg","two_egg","three_egg","four_egg"};cracks:=[...]string{"no_cracks","cracked","max_cracked"};return "minecraft:turtle_egg",map[string]any{"turtle_egg_count":counts[e.Eggs],"cracked_state":cracks[e.Hatch]}}
func (e TurtleEgg) Hash()(uint64,uint64){e=e.normalized();return hashTurtleEgg,uint64(e.Eggs-1)|uint64(e.Hatch)<<2}
func (e TurtleEgg) NeighbourUpdateTick(pos,_ cube.Pos,tx *world.Tx){if _,ok:=tx.Block(pos.Side(cube.FaceDown)).(Sand);!ok{breakBlock(e,pos,tx)}}
func (e TurtleEgg) RandomTick(pos cube.Pos, tx *world.Tx, r *rand.Rand) {
	if _, ok := tx.Block(pos.Side(cube.FaceDown)).(Sand); !ok {
		return
	}
	if r.Float32() >= turtleEggHatchChance(tx.World().Time(), tx.World().Dimension()) {
		return
	}
	e = e.normalized()
	if e.Hatch<2 { e.Hatch++;tx.SetBlock(pos,e,nil);tx.PlaySound(pos.Vec3Centre(),sound.Custom{Name:"block.turtle_egg.crack",Volume:.7,Pitch:.95+float64(r.Float32())*.1});return }
	tx.SetBlock(pos,nil,nil)
	tx.PlaySound(pos.Vec3Centre(),sound.Custom{Name:"block.turtle_egg.hatch",Volume:.7,Pitch:.95+float64(r.Float32())*.1})
	for i:=range int(e.Eggs){opts:=world.EntitySpawnOpts{Position:pos.Vec3().Add(mgl64.Vec3{.3+float64(i)*.2,0,.3}),SpawnReason:"breeding"}
		h:=tx.World().EntityRegistry().Config().SpawnMob("minecraft:turtle",opts)
		if h==nil{continue};tx.AddEntity(h);if spawned,ok:=h.Entity(tx);ok{if ageable,ok:=spawned.(interface{SetAge(int)});ok{ageable.SetAge(-24000)};if turtle,ok:=spawned.(interface{SetHomePos(cube.Pos)});ok{turtle.SetHomePos(pos)}}
	}
}
func (e TurtleEgg) EntityStepOn(pos cube.Pos,tx *world.Tx,entity world.Entity){
	if entity==nil||entity.H().Type().EncodeEntity()=="minecraft:turtle"||entity.H().Type().EncodeEntity()=="minecraft:bat"{return}
	if entity.H().Type().EncodeEntity()=="minecraft:player"{if sneaky,ok:=entity.(interface{Sneaking()bool});ok&&sneaky.Sneaking(){return};if rand.IntN(100)!=0{return}}else{return}
	e=e.normalized();if e.Eggs>1{e.Eggs--;tx.SetBlock(pos,e,nil)}else{tx.SetBlock(pos,nil,nil)};tx.PlaySound(pos.Vec3Centre(),sound.Custom{Name:"block.turtle_egg.break",Volume:.7,Pitch:.9+rand.Float64()*.2})
}
func (TurtleEgg) ReplaceableBy(b world.Block)bool{return false}
// turtleEggHatchChance is the overworld day-timeline value for
// EnvironmentAttributes.TURTLE_EGG_HATCH_CHANCE. The source track is a
// constant maximum of 1 from 21062 until 21905, then the default .002.
func turtleEggHatchChance(time int, dimension world.Dimension) float32 {
	if dimension != world.Overworld {
		return .002
	}
	dayTime := time % 24000
	if dayTime < 0 {
		dayTime += 24000
	}
	if dayTime >= 21062 && dayTime < 21905 {
		return 1
	}
	return .002
}

var _ world.RandomTicker=TurtleEgg{}
var _ world.NeighbourUpdateTicker=TurtleEgg{}
var _ EntityStepper=TurtleEgg{}
var _ world.BlockModel=turtleEggModel{}
func init() {
	for _, state := range (TurtleEgg{}).Permutations() {
		world.RegisterBlock(state)
	}
	world.RegisterItem(TurtleEgg{Eggs: 1})
}
