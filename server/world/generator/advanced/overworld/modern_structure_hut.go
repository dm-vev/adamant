package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("swamp_hut", buildModernSwampHut) }

type modernSwampHutPiece struct {
	box javastructure.Box
	direction string
	seed int64
}

func (p modernSwampHutPiece) Bounds() javastructure.Box { return p.box }
func (p modernSwampHutPiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	out := javastructure.Placement{}
	put := func(x, y, z int32, block string, props map[string]any) {
		pos := p.worldPos(x, y, z)
		if pos[0] < clip.Min[0] || pos[0] > clip.Max[0] || pos[1] < clip.Min[1] || pos[1] > clip.Max[1] || pos[2] < clip.Min[2] || pos[2] > clip.Max[2] { return }
		out.Blocks = append(out.Blocks, javastructure.Block{Pos:pos, State:world.BlockState{Name:"minecraft:"+block, Properties:props}})
	}
	box := func(x0,y0,z0,x1,y1,z1 int32, name string) { for y:=y0; y<=y1; y++ { for z:=z0; z<=z1; z++ { for x:=x0; x<=x1; x++ { put(x,y,z,name,nil) } } } }
	box(1,1,1,5,1,7,"spruce_planks")
	box(1,4,2,5,4,7,"spruce_planks")
	box(2,1,0,4,1,0,"spruce_planks")
	box(2,2,2,3,3,2,"spruce_planks")
	box(1,2,3,1,3,6,"spruce_planks")
	box(5,2,3,5,3,6,"spruce_planks")
	box(2,2,7,4,3,7,"spruce_planks")
	for _, v := range [][3]int32{{1,0,2},{5,0,2},{1,0,7},{5,0,7}} { box(v[0],v[1],v[2],v[0],3,v[2],"oak_log") }
	for _, v := range [][3]int32{{2,3,2},{3,3,7},{1,2,1},{5,2,1}} { put(v[0],v[1],v[2],"oak_fence",nil) }
	for _, v := range [][3]int32{{1,3,4},{5,3,4},{5,3,5}} { put(v[0],v[1],v[2],"air",nil) }
	put(1,3,5,"potted_red_mushroom",nil)
	put(3,2,6,"crafting_table",nil); put(4,2,6,"cauldron",nil)
	stairs := []struct{x,y,z int32; facing,shape string}{
		{0,4,1,"north","outer_right"},{1,4,1,"north","straight"},{2,4,1,"north","straight"},{3,4,1,"north","straight"},{4,4,1,"north","straight"},{5,4,1,"north","straight"},{6,4,1,"north","outer_left"},
		{0,4,2,"east","straight"},{0,4,3,"east","straight"},{0,4,4,"east","straight"},{0,4,5,"east","straight"},{0,4,6,"east","straight"},{0,4,7,"east","straight"},
		{6,4,2,"west","straight"},{6,4,3,"west","straight"},{6,4,4,"west","straight"},{6,4,5,"west","straight"},{6,4,6,"west","straight"},{6,4,7,"west","straight"},
		{0,4,8,"south","outer_left"},{1,4,8,"south","straight"},{2,4,8,"south","straight"},{3,4,8,"south","straight"},{4,4,8,"south","straight"},{5,4,8,"south","straight"},{6,4,8,"south","outer_right"},
	}
	for _, s := range stairs { put(s.x,s.y,s.z,"spruce_stairs",map[string]any{"facing":p.rotateFacing(s.facing),"half":"bottom","shape":s.shape,"waterlogged":false}) }
	for _, z := range []int32{2,7} { for _, x := range []int32{1,5} { for y:=int32(-1); p.box.Min[1]+y > -64; y-- { pos:=p.worldPos(x,y,z); if pos[1]<clip.Min[1] || pos[1]>clip.Max[1] { continue }; if env.BaseBlock!=nil { name,_:=env.BaseBlock(pos[0],pos[1],pos[2]).EncodeBlock(); if name=="minecraft:air" || name=="minecraft:cave_air" || name=="minecraft:water" || name=="minecraft:lava" { put(x,y,z,"oak_log",map[string]any{"axis":"y"}); continue }; break }; put(x,y,z,"oak_log",map[string]any{"axis":"y"}) } } }
	mobPos:=p.worldPos(2,2,5)
	if mobPos[0]>=clip.Min[0]&&mobPos[0]<=clip.Max[0]&&mobPos[1]>=clip.Min[1]&&mobPos[1]<=clip.Max[1]&&mobPos[2]>=clip.Min[2]&&mobPos[2]<=clip.Max[2] {
		for _,id:=range []string{"minecraft:witch","minecraft:cat"}{out.Entities=append(out.Entities,javastructure.Entity{Pos:[3]float64{float64(mobPos[0])+0.5,float64(mobPos[1]),float64(mobPos[2])+0.5},BlockPos:mobPos,NBT:map[string]any{"id":id,"PersistenceRequired":true,"Rotation":[]any{float32(0),float32(0)}},Seed:p.seed})}
	}
	return out,nil
}
func (p modernSwampHutPiece) worldPos(x,y,z int32) [3]int32 {
	b:=p.box
	switch p.direction { case "north": return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Max[2]-z}; case "south": return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Min[2]+z}; case "west": return [3]int32{b.Max[0]-z,b.Min[1]+y,b.Min[2]+x}; default: return [3]int32{b.Min[0]+z,b.Min[1]+y,b.Min[2]+x} }
}
func (p modernSwampHutPiece) rotateFacing(face string) string { order:=[]string{"north","east","south","west"}; index:=0; for i,v:=range order { if v==face { index=i; break } }; turns:=map[string]int{"north":2,"east":1,"south":0,"west":3}[p.direction]; return order[(index+turns)%4] }

func buildModernSwampHut(in modernStructureBuildInput) (modernStructureStart, bool, error) {
	if in.random==nil || in.source==nil { return modernStructureStart{},false,fmt.Errorf("swamp hut requires structure random and world source") }
	directions:=[]string{"north","east","south","west"}; direction:=directions[in.random.NextInt(4)]
	minX,minZ:=in.chunkX*16,in.chunkZ*16; width,depth:=int32(7),int32(9); if direction=="east"||direction=="west" { width,depth=depth,width }
	box:=javastructure.Box{Min:[3]int32{minX,64,minZ},Max:[3]int32{minX+width-1,70,minZ+depth-1}}
	total:=int32(0); for z:=box.Min[2];z<=box.Max[2];z++ { for x:=box.Min[0];x<=box.Max[0];x++ { total+=in.source.BaseHeight(x,z,false) } }
	height:=total/(width*depth); box.Min[1]=height;box.Max[1]=height+6
	piece:=modernSwampHutPiece{box:box,direction:direction,seed:in.seed}
	return modernStructureStart{name:in.name,pieces:[]modernStructurePiece{piece},box:box,terrain:terrainAdjustment(stringValue(in.config["terrain_adaptation"]))},true,nil
}

