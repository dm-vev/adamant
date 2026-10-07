package overworld

import (
	"fmt"
	"math"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("shipwreck", buildModernShipwreck) }

type modernShipwreckPiece struct { template *javastructure.Template; origin [3]int32; rotation javastructure.Rotation; beached bool; seed int64 }
func (p modernShipwreckPiece) Bounds() javastructure.Box {
	pivot:=[3]int32{4,0,15}
	var out=javastructure.Box{Min:[3]int32{int32(^uint32(0)>>1),int32(^uint32(0)>>1),int32(^uint32(0)>>1)},Max:[3]int32{-int32(^uint32(0)>>1)-1,-int32(^uint32(0)>>1)-1,-int32(^uint32(0)>>1)-1}}
	for _,x:=range [2]int32{0,p.template.Size[0]-1} { for _,y:=range [2]int32{0,p.template.Size[1]-1} { for _,z:=range [2]int32{0,p.template.Size[2]-1} { v:=javastructure.TransformPosition([3]int32{x-pivot[0],y,z-pivot[2]},javastructure.NoMirror,p.rotation); for a:=range 3 { v[a]+=p.origin[a]+pivot[a]; if v[a]<out.Min[a]{out.Min[a]=v[a]};if v[a]>out.Max[a]{out.Max[a]=v[a]} } } } }
	return out
}
func (p modernShipwreckPiece) Place(clip javastructure.Bounds,env javastructure.ProcessorEnvironment)(javastructure.Placement,error) {
	if env.Random==nil||env.BaseBlock==nil{return javastructure.Placement{},fmt.Errorf("shipwreck requires base-block and placement random queries")}
	pivot:=[3]int32{4,0,15};transformed:=javastructure.TransformPosition(pivot,javastructure.NoMirror,p.rotation);origin:=p.origin
	for axis:=range 3{origin[axis]+=pivot[axis]-transformed[axis]}
	ignore:=func(_ javastructure.ProcessContext,b javastructure.Block)(javastructure.Block,bool,error){return b,b.State.Name!="minecraft:air"&&b.State.Name!="minecraft:structure_block",nil}
	placed,err:=p.template.Place(0,origin,p.rotation,javastructure.NoMirror,clip,p.seed,ignore);if err!=nil{return javastructure.Placement{},err}
	for i:=range placed.Blocks{javastructure.SeedRandomizableContainerLoot(&placed.Blocks[i],env.Random)}
	inside:=func(pos [3]int32)bool{return pos[0]>=clip.Min[0]&&pos[0]<=clip.Max[0]&&pos[1]>=clip.Min[1]&&pos[1]<=clip.Max[1]&&pos[2]>=clip.Min[2]&&pos[2]<=clip.Max[2]}
	for _,source:=range p.template.Palettes[0]{
		if source.State.Name!="minecraft:structure_block"||source.NBT==nil||source.NBT["mode"]!="DATA"{continue}
		marker,_:=source.NBT["metadata"].(string);var table string
		switch marker{case "map_chest":table="minecraft:chests/shipwreck_map";case "treasure_chest":table="minecraft:chests/shipwreck_treasure";case "supply_chest":table="minecraft:chests/shipwreck_supply"}
		if table==""{continue}
		markerPos:=javastructure.TransformPosition(source.Pos,javastructure.NoMirror,p.rotation);for axis:=range 3{markerPos[axis]+=origin[axis]};if !inside(markerPos){continue}
		pos:=markerPos;pos[1]--;if !inside(pos){continue}
		chestIndex:=-1
		for index:=range placed.Blocks{if placed.Blocks[index].Pos==pos&&placed.Blocks[index].State.Name=="minecraft:chest"{chestIndex=index;break}}
		if chestIndex>=0{
			block:=&placed.Blocks[chestIndex];if block.NBT==nil{block.NBT=map[string]any{"id":"minecraft:chest"}};block.NBT["LootTable"]=table;javastructure.SeedRandomizableContainerLoot(block,env.Random)
			continue
		}
		if env.BaseBlock!=nil{base:=env.BaseBlock(pos[0],pos[1],pos[2]);name,properties:=base.EncodeBlock();if name=="minecraft:chest"{block:=javastructure.Block{Pos:pos,State:world.BlockState{Name:name,Properties:properties},NBT:map[string]any{"id":"minecraft:chest","LootTable":table}};javastructure.SeedRandomizableContainerLoot(&block,env.Random);placed.Blocks=append(placed.Blocks,block)}}
	}
	return placed,nil
}

func buildModernShipwreck(input modernStructureBuildInput)(modernStructureStart,bool,error) {
	if input.source==nil||input.random==nil{return modernStructureStart{},false,fmt.Errorf("shipwreck requires terrain and generation random queries")}
	beached:=input.config["is_beached"]==true
	allowed,err:=modernStructureBiomeAllowed(input,stringValue(input.config["biomes"]),!beached);if err!=nil{return modernStructureStart{},false,err};if !allowed{return modernStructureStart{},false,nil}
	names:=[]string{"with_mast","sideways_full","sideways_fronthalf","sideways_backhalf","rightsideup_full","rightsideup_fronthalf","rightsideup_backhalf","with_mast_degraded","rightsideup_full_degraded","rightsideup_fronthalf_degraded","rightsideup_backhalf_degraded"}
	if !beached { names=[]string{"with_mast","upsidedown_full","upsidedown_fronthalf","upsidedown_backhalf","sideways_full","sideways_fronthalf","sideways_backhalf","rightsideup_full","rightsideup_fronthalf","rightsideup_backhalf","with_mast_degraded","upsidedown_full_degraded","upsidedown_fronthalf_degraded","upsidedown_backhalf_degraded","sideways_full_degraded","sideways_fronthalf_degraded","sideways_backhalf_degraded","rightsideup_full_degraded","rightsideup_fronthalf_degraded","rightsideup_backhalf_degraded"} }
	rotation:=javastructure.Rotation(input.random.NextInt(4))
	template,err:=javastructure.Load("shipwreck/"+names[input.random.NextInt(int32(len(names)))])
	if err!=nil{return modernStructureStart{},false,err}
	piece:=&modernShipwreckPiece{template:template,origin:[3]int32{input.chunkX*16,90,input.chunkZ*16},rotation:rotation,beached:beached,seed:input.seed}
	if template.Size[0]>32||template.Size[1]>32 {
		bounds:=piece.Bounds();count:=int64(bounds.Max[0]-bounds.Min[0]+1)*int64(bounds.Max[2]-bounds.Min[2]+1);var total int64;minY:=int32(math.MaxInt32)
		for z:=bounds.Min[2];z<=bounds.Max[2];z++{for x:=bounds.Min[0];x<=bounds.Max[0];x++{height:=input.source.BaseHeight(x,z,!beached);total+=int64(height);if height<minY{minY=height}}}
		if beached{piece.origin[1]=minY-template.Size[1]/2-input.random.NextInt(3)}else{piece.origin[1]=int32(total/count)}
	} else {
		count:=int64(template.Size[0])*int64(template.Size[2]);var total int64;minY:=int32(math.MaxInt32)
		for dz:=range template.Size[2]{for dx:=range template.Size[0]{x,z:=piece.origin[0]+dx,piece.origin[2]+dz;height:=input.source.BaseHeight(x,z,!beached);total+=int64(height);if height<minY{minY=height}}}
		if beached{planningRandom:=javaFeatureRandom(input.seed,input.chunkX,input.chunkZ,13,4);piece.origin[1]=minY-template.Size[1]/2-planningRandom.NextInt(3)}else{piece.origin[1]=int32(total/count)}
	}
	return modernStructureStart{name:input.name,pieces:[]modernStructurePiece{piece},box:piece.Bounds(),terrain:javastructure.TerrainNone},true,nil
}
