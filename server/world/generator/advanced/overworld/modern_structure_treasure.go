package overworld

import (
	"fmt"
	"strings"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("buried_treasure", buildModernBuriedTreasure) }

type modernBuriedTreasurePiece struct { pos [3]int32 }
func (p modernBuriedTreasurePiece) Bounds() javastructure.Box { return javastructure.Box{Min:p.pos, Max:p.pos} }
func (p modernBuriedTreasurePiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement,error) {
	if env.BaseBlock==nil || env.BaseHeight==nil || env.Random==nil { return javastructure.Placement{}, fmt.Errorf("buried treasure requires base block, ocean-floor height, and placement random queries") }
	x,z:=p.pos[0],p.pos[2]
	y:=env.BaseHeight(x,z,true)
	for ; y>clip.Min[1]; y-- {
		below,current:=env.BaseBlock(x,y-1,z),env.BaseBlock(x,y,z)
		belowName,_:=below.EncodeBlock()
		if belowName!="minecraft:sandstone" && belowName!="minecraft:stone" && belowName!="minecraft:andesite" && belowName!="minecraft:granite" && belowName!="minecraft:diorite" { continue }
		name,_:=current.EncodeBlock()
		soft:=world.BlockState{Name:"minecraft:sand"}
		if name!="minecraft:air" && name!="minecraft:water" && name!="minecraft:lava" { blockName,properties:=current.EncodeBlock(); soft=world.BlockState{Name:blockName,Properties:properties} }
		out:=javastructure.Placement{}
		put:=func(px,py,pz int32,state world.BlockState,nbt map[string]any) { if px>=clip.Min[0]&&px<=clip.Max[0]&&py>=clip.Min[1]&&py<=clip.Max[1]&&pz>=clip.Min[2]&&pz<=clip.Max[2] { out.Blocks=append(out.Blocks,javastructure.Block{Pos:[3]int32{px,py,pz},State:state,NBT:nbt}) } }
		if x>=clip.Min[0]&&x<=clip.Max[0]&&y>=clip.Min[1]&&y<=clip.Max[1]&&z>=clip.Min[2]&&z<=clip.Max[2]&&name!="minecraft:chest" {
			facing:=modernStructureChestFacing(x,y,z,env.BaseBlock)
			seed:=env.Random.NextLong()
			put(x,y,z,world.BlockState{Name:"minecraft:chest",Properties:map[string]any{"facing":facing,"type":"single","waterlogged":false}},map[string]any{"id":"minecraft:chest","LootTable":"minecraft:chests/buried_treasure","LootTableSeed":seed})
		}
		for _,d:=range [][3]int32{{0,-1,0},{0,1,0},{0,0,-1},{0,0,1},{-1,0,0},{1,0,0}} {
			rx,ry,rz:=x+d[0],y+d[1],z+d[2]
			neighbor:=env.BaseBlock(rx,ry,rz); nn,_:=neighbor.EncodeBlock()
			if nn!="minecraft:air"&&nn!="minecraft:water"&&nn!="minecraft:lava" { continue }
			belowNeighbor:=env.BaseBlock(rx,ry-1,rz); bn,_:=belowNeighbor.EncodeBlock()
			if bn=="minecraft:air"||bn=="minecraft:water"||bn=="minecraft:lava" { if d[1]!=1 { fillName,properties:=below.EncodeBlock();put(rx,ry,rz,world.BlockState{Name:fillName,Properties:properties},nil) } } else { put(rx,ry,rz,soft,nil) }
		}
		return out,nil
	}
	return javastructure.Placement{},nil
}

func buildModernBuriedTreasure(input modernStructureBuildInput) (modernStructureStart,bool,error) {
	if input.source==nil{return modernStructureStart{},false,fmt.Errorf("buried treasure requires world source")}
	ok,err:=modernStructureBiomeAllowed(input,stringValue(input.config["biomes"]),true);if err!=nil{return modernStructureStart{},false,err};if !ok{return modernStructureStart{},false,nil}
	pos:=[3]int32{input.chunkX*16+9,90,input.chunkZ*16+9}
	piece:=modernBuriedTreasurePiece{pos:pos}
	return modernStructureStart{name:input.name,pieces:[]modernStructurePiece{piece},box:piece.Bounds(),terrain:javastructure.TerrainNone},true,nil
}

func modernStructureBiomeAllowed(input modernStructureBuildInput,tag string,oceanFloor bool)(bool,error){
	x,z:=input.chunkX*16+8,input.chunkZ*16+8
	y:=input.source.BaseHeight(x,z,oceanFloor)
	biome:=normalizeJavaResource(input.source.JavaNoiseBiomeAt(x>>2,y>>2,z>>2));if !strings.Contains(biome,":"){biome="minecraft:"+biome}
	return javadata.BiomeTagContains(tag,biome)
}
type modernStructureBlockSource func(cube.Pos) world.Block

func (source modernStructureBlockSource) Block(pos cube.Pos) world.Block {
	return source(pos)
}

func modernStructureChestFacing(x,y,z int32,baseBlock func(x,y,z int32)world.Block)string {
	offsets:=[4][3]int32{{0,0,-1},{1,0,0},{0,0,1},{-1,0,0}}
	facings:=[4]string{"north","east","south","west"}
	source:=modernStructureBlockSource(func(pos cube.Pos)world.Block{return baseBlock(int32(pos[0]),int32(pos[1]),int32(pos[2]))})
	solid:=func(px,py,pz int32)bool{
		block:=baseBlock(px,py,pz);if block==nil{return false}
		pos:=cube.Pos{int(px),int(py),int(pz)}
		for _,face:=range cube.Faces(){if !block.Model().FaceSolid(pos,face,source){return false}}
		return true
	}
	for _,offset:=range offsets{
		block:=baseBlock(x+offset[0],y+offset[1],z+offset[2])
		if block==nil{continue}
		name,_:=block.EncodeBlock();if name=="minecraft:chest"{return "north"}
	}
	solidDirection:=-1
	for direction,offset:=range offsets{
		if !solid(x+offset[0],y+offset[1],z+offset[2]){continue}
		if solidDirection>=0{solidDirection=-2;break}
		solidDirection=direction
	}
	if solidDirection>=0{return facings[(solidDirection+2)%len(facings)]}
	direction:=0
	for _,step:=range []int{2,1,2}{
		if solid(x+offsets[direction][0],y,z+offsets[direction][2]){direction=(direction+step)%len(offsets)}
	}
	return facings[direction]
}
