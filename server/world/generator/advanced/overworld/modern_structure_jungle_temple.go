package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init(){registerModernStructureBuilder("jungle_temple",buildModernJungleTemple)}
type modernJungleTemple struct{box javastructure.Box;dir int}
func(p modernJungleTemple)Bounds()javastructure.Box{return p.box}
func(p modernJungleTemple)pos(x,y,z int32)[3]int32{b:=p.box;switch p.dir{case 0:return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Max[2]-z};case 1:return [3]int32{b.Min[0]+z,b.Min[1]+y,b.Min[2]+x};case 2:return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Min[2]+z};default:return [3]int32{b.Max[0]-z,b.Min[1]+y,b.Min[2]+x}}}
func(p modernJungleTemple)face(v string)string{switch p.dir{case 1:switch v{case "north":return "east";case "east":return "south";case "south":return "west";default:return "north"};case 2:switch v{case "north":return "south";case "south":return "north"};case 3:switch v{case "north":return "west";case "east":return "south";case "south":return "east";default:return "north"}};return v}
func(p modernJungleTemple)Place(clip javastructure.Bounds,env javastructure.ProcessorEnvironment)(javastructure.Placement,error){
	out:=javastructure.Placement{}
	if env.Random==nil{return javastructure.Placement{},fmt.Errorf("jungle temple placement requires Java structure random")}
	rng:=env.Random
	inside:=func(pos [3]int32)bool{return pos[0]>=clip.Min[0]&&pos[0]<=clip.Max[0]&&pos[1]>=clip.Min[1]&&pos[1]<=clip.Max[1]&&pos[2]>=clip.Min[2]&&pos[2]<=clip.Max[2]}
	put:=func(x,y,z int32,name string,props map[string]any,nbt map[string]any){pos:=p.pos(x,y,z);if inside(pos){out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:world.BlockState{Name:"minecraft:"+name,Properties:props},NBT:nbt})}}
	loot:=func(x,y,z int32,name string,props map[string]any,table string){pos:=p.pos(x,y,z);if inside(pos){out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:world.BlockState{Name:"minecraft:"+name,Properties:props},NBT:map[string]any{"id":"minecraft:"+name,"LootTable":"minecraft:chests/"+table,"LootTableSeed":rng.NextLong()}})}}
	box:=func(x0,y0,z0,x1,y1,z1 int32,name string){for y:=y0;y<=y1;y++{for x:=x0;x<=x1;x++{for z:=z0;z<=z1;z++{put(x,y,z,name,nil,nil)}}}}
	stone:=func(x0,y0,z0,x1,y1,z1 int32){for y:=y0;y<=y1;y++{for x:=x0;x<=x1;x++{for z:=z0;z<=z1;z++{name:="mossy_cobblestone";if rng.NextFloat()<0.4{name="cobblestone"};put(x,y,z,name,nil,nil)}}}}
	air:=func(x0,y0,z0,x1,y1,z1 int32){box(x0,y0,z0,x1,y1,z1,"air")}
	stair:=func(x,y,z int32,facing string){put(x,y,z,"cobblestone_stairs",map[string]any{"facing":p.face(facing),"half":"bottom","shape":"straight","waterlogged":false},nil)}
	stone(0,-4,0,11,0,14)
	stone(2,1,2,9,2,2);stone(2,1,12,9,2,12);stone(2,1,3,2,2,11);stone(9,1,3,9,2,11)
	stone(1,3,1,10,6,1);stone(1,3,13,10,6,13);stone(1,3,2,1,6,12);stone(10,3,2,10,6,12);stone(2,3,2,9,3,12);stone(2,6,2,9,6,12);stone(3,7,3,8,7,11);stone(4,8,4,7,8,10)
	air(3,1,3,8,2,11);air(4,3,6,7,3,9);air(2,4,2,9,5,12);air(4,6,5,7,6,9);air(5,7,6,6,7,8);air(5,1,2,6,2,2);air(5,2,12,6,2,12);air(5,5,1,6,5,1);air(5,5,13,6,5,13)
	for _,v:=range [][3]int32{{1,5,5},{10,5,5},{1,5,9},{10,5,9}}{put(v[0],v[1],v[2],"air",nil,nil)}
	for _,z:=range []int32{0,14}{for _,x:=range []int32{2,4,7,9}{stone(x,4,z,x,5,z)}};stone(5,6,0,6,6,0)
	for _,x:=range []int32{0,11}{for z:=int32(2);z<=12;z+=2{stone(x,4,z,x,5,z)};stone(x,6,5,x,6,5);stone(x,6,9,x,6,9)}
	for _,v:=range [][3]int32{{2,2,2},{9,2,2},{2,2,12},{9,2,12}}{stone(v[0],7,v[2],v[0],9,v[2])}
	for _,v:=range [][3]int32{{4,9,4},{7,9,4},{4,9,10},{7,9,10}}{stone(v[0],v[1],v[2],v[0],v[1],v[2])}
	for _,v:=range [][3]int32{{5,9,6},{6,9,6},{5,9,8},{6,9,8}}{f:="north";if v[2]==8{f="south"};stair(v[0],v[1],v[2],f)}
	for x:=int32(4);x<=7;x++{stair(x,0,0,"north")}
	for _,x:=range []int32{4,7}{for i:=int32(1);i<=3;i++{stair(x,i,7+i,"north")}}
	stone(4,1,9,4,1,9);stone(7,1,9,7,1,9);stone(4,1,10,7,2,10)
	stone(5,4,5,6,4,5);put(4,4,5,"cobblestone_stairs",map[string]any{"facing":p.face("east"),"half":"bottom","shape":"straight","waterlogged":false},nil);put(7,4,5,"cobblestone_stairs",map[string]any{"facing":p.face("west"),"half":"bottom","shape":"straight","waterlogged":false},nil)
	for x:=int32(5);x<=6;x++{for i:=int32(0);i<4;i++{stair(x,-i,6+i,"south");air(x,-i,7+i,x,-i,9+i)}}
	air(1,-3,12,10,-1,13);air(1,-3,1,3,-1,13);air(1,-3,1,9,-1,5)
	for z:=int32(1);z<=13;z+=2{stone(1,-3,z,1,-2,z)};for z:=int32(2);z<=12;z+=2{stone(1,-1,z,3,-1,z)}
	stone(2,-2,1,5,-2,1);stone(7,-2,1,9,-2,1);stone(6,-3,1,6,-3,1);stone(6,-1,1,6,-1,1)
	put(1,-3,8,"tripwire_hook",map[string]any{"facing":p.face("east"),"attached":true,"powered":false},nil);put(4,-3,8,"tripwire_hook",map[string]any{"facing":p.face("west"),"attached":true,"powered":false},nil)
	for _,x:=range []int32{2,3}{put(x,-3,8,"tripwire",map[string]any{"east":true,"west":true,"attached":true},nil)}
	wireNS:=map[string]any{"north":"side","south":"side","east":"none","west":"none","power":0}
	for z:=int32(7);z>=2;z--{put(5,-3,z,"redstone_wire",wireNS,nil)}
	put(5,-3,1,"redstone_wire",map[string]any{"north":"side","west":"side","east":"none","south":"none","power":0},nil);put(4,-3,1,"redstone_wire",map[string]any{"east":"side","west":"side","north":"none","south":"none","power":0},nil);put(3,-3,1,"mossy_cobblestone",nil,nil)
	loot(3,-2,1,"dispenser",map[string]any{"facing":p.face("north"),"triggered":false},"jungle_temple_dispenser")
	put(3,-2,2,"vine",map[string]any{"south":true},nil)
	put(7,-3,1,"tripwire_hook",map[string]any{"facing":p.face("north"),"attached":true,"powered":false},nil);put(7,-3,5,"tripwire_hook",map[string]any{"facing":p.face("south"),"attached":true,"powered":false},nil)
	for z:=int32(2);z<=4;z++{put(7,-3,z,"tripwire",map[string]any{"north":true,"south":true,"attached":true},nil)}
	put(8,-3,6,"redstone_wire",map[string]any{"east":"side","west":"side","north":"none","south":"none","power":0},nil);put(9,-3,6,"redstone_wire",map[string]any{"west":"side","south":"side","east":"none","north":"none","power":0},nil);put(9,-3,5,"redstone_wire",map[string]any{"north":"side","south":"up","east":"none","west":"none","power":0},nil);put(9,-3,4,"mossy_cobblestone",nil,nil);put(9,-2,4,"redstone_wire",wireNS,nil)
	loot(9,-2,3,"dispenser",map[string]any{"facing":p.face("west"),"triggered":false},"jungle_temple_dispenser")
	put(8,-1,3,"vine",map[string]any{"east":true},nil);put(8,-2,3,"vine",map[string]any{"east":true},nil);loot(8,-3,3,"chest",map[string]any{"facing":p.face("north"),"type":"single","waterlogged":false},"jungle_temple")
	for _,v:=range [][3]int32{{9,-3,2},{8,-3,1},{4,-3,5},{5,-2,5},{5,-1,5},{6,-3,5},{7,-2,5},{7,-1,5},{8,-3,5}}{put(v[0],v[1],v[2],"mossy_cobblestone",nil,nil)}
	stone(9,-1,1,9,-1,5);air(8,-3,8,10,-1,10)
	for x:=int32(8);x<=10;x++{put(x,-2,11,"chiseled_stone_bricks",nil,nil);put(x,-2,12,"lever",map[string]any{"face":"wall","facing":p.face("north"),"powered":false},nil)}
	stone(8,-3,8,8,-3,10);stone(10,-3,8,10,-3,10);put(10,-2,9,"mossy_cobblestone",nil,nil);put(8,-2,9,"redstone_wire",wireNS,nil);put(8,-2,10,"redstone_wire",wireNS,nil)
	put(10,-1,9,"redstone_wire",map[string]any{"north":"side","south":"side","east":"side","west":"side","power":0},nil);put(9,-2,8,"sticky_piston",map[string]any{"facing":"up","extended":false},nil);put(10,-2,8,"sticky_piston",map[string]any{"facing":p.face("west"),"extended":false},nil);put(10,-1,8,"sticky_piston",map[string]any{"facing":p.face("west"),"extended":false},nil);put(10,-2,10,"repeater",map[string]any{"facing":p.face("north"),"delay":1,"locked":false,"powered":false},nil)
	loot(9,-3,10,"chest",map[string]any{"facing":p.face("north"),"type":"single","waterlogged":false},"jungle_temple")
	return out,nil
}

func buildModernJungleTemple(in modernStructureBuildInput)(modernStructureStart,bool,error){
	if in.random==nil||in.source==nil{return modernStructureStart{},false,fmt.Errorf("jungle temple requires structure random and world source")}
	dir:=int(in.random.NextInt(4));x,z:=in.chunkX*16,in.chunkZ*16
	minX,maxX,minZ,maxZ:=x,x+11,z,z+14
	if dir==1||dir==3{maxX=x+14;maxZ=z+11}
	startY:=int32(0);for xx:=minX;xx<=maxX;xx++{for zz:=minZ;zz<=maxZ;zz++{startY+=in.source.BaseHeight(xx,zz,false)}}
	startY/=180
	box:=javastructure.Box{Min:[3]int32{minX,startY,minZ},Max:[3]int32{maxX,startY+9,maxZ}}
	p:=modernJungleTemple{box:box,dir:dir}
	return modernStructureStart{name:in.name,pieces:[]modernStructurePiece{p},box:box,terrain:terrainAdjustment(stringValue(in.config["terrain_adaptation"]))},true,nil
}
