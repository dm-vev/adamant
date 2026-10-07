package overworld

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("ocean_monument", buildModernOceanMonument) }

const (
	monumentDown = iota
	monumentUp
	monumentNorth
	monumentSouth
	monumentWest
	monumentEast
)

type modernMonumentRoom struct {
	index int
	connections [6]int
	hasOpening [6]bool
	claimed bool
	source bool
	scanIndex int
}

type modernOceanMonumentPiece struct {
	west, north int32
	orientation int
	rooms [1004]modernMonumentRoom
	core int
	roomOrder []int
	children []modernMonumentChild
	seed int64
}

func (p modernOceanMonumentPiece) Bounds() javastructure.Box {
	return javastructure.Box{Min:[3]int32{p.west,39,p.north},Max:[3]int32{p.west+57,61,p.north+57}}
}

func (p modernOceanMonumentPiece) worldPos(x,y,z int32)[3]int32 {
	var wx,wz int32
	switch p.orientation {
	case 0: wx,wz=p.west+x,p.north+57-z
	case 1: wx,wz=p.west+z,p.north+x
	case 2: wx,wz=p.west+x,p.north+z
	default: wx,wz=p.west+57-z,p.north+x
	}
	return [3]int32{wx,39+y,wz}
}

func (p modernOceanMonumentPiece) Place(clip javastructure.Bounds,env javastructure.ProcessorEnvironment)(javastructure.Placement,error){
	if env.BaseBlock==nil||env.Random==nil{return javastructure.Placement{},fmt.Errorf("ocean monument requires base-block and placement random queries")}
	blocks,err:=p.modernMonumentMainBlocks(clip,env);if err!=nil{return javastructure.Placement{},err}
	out:=javastructure.Placement{Blocks:blocks,Entities:make([]javastructure.Entity,0,3)}
	overlay:=make(map[[3]int32]string,len(blocks))
	for _,block:=range blocks{overlay[block.Pos]=block.State.Name}
	inside:=func(pos [3]int32)bool{return pos[0]>=clip.Min[0]&&pos[0]<=clip.Max[0]&&pos[1]>=clip.Min[1]&&pos[1]<=clip.Max[1]&&pos[2]>=clip.Min[2]&&pos[2]<=clip.Max[2]}
	getBuilding:=func(x,y,z int32)string{
		pos:=p.worldPos(x,y,z);if !inside(pos){return "minecraft:air"}
		if name,ok:=overlay[pos];ok{return name}
		block:=env.BaseBlock(pos[0],pos[1],pos[2]);if block==nil{return "minecraft:air"}
		name,_:=block.EncodeBlock();return normalizeJavaResource(name)
	}
	putBuilding:=func(x,y,z int32,name string){pos:=p.worldPos(x,y,z);if inside(pos){out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:world.BlockState{Name:name}});overlay[pos]=name}}
	spawnBuilding:=func(x,y,z int32){
		pos:=p.worldPos(x,y,z);if !inside(pos){return}
		out.Entities=append(out.Entities,javastructure.Entity{Pos:[3]float64{float64(pos[0])+0.5,float64(pos[1]),float64(pos[2])+0.5},BlockPos:pos,NBT:map[string]any{"id":"minecraft:elder_guardian","PersistenceRequired":true},Record:int32(len(out.Entities)),Seed:p.seed,SpawnReason:"structure"})
	}
	for _,child:=range p.children{
		if !modernMonumentChildIntersects(&p,child,clip){continue}
		get:=func(x,y,z int32)string{pos:=modernMonumentChildToBuilding(&p,child,x,y,z);return getBuilding(pos[0],pos[1],pos[2])}
		put:=func(x,y,z int32,name string){pos:=modernMonumentChildToBuilding(&p,child,x,y,z);putBuilding(pos[0],pos[1],pos[2],name)}
		spawn:=func(x,y,z int32){pos:=modernMonumentChildToBuilding(&p,child,x,y,z);spawnBuilding(pos[0],pos[1],pos[2])}
		emitModernMonumentChild(&p,child,env.Random,get,put,spawn)
	}
	return out,nil
}

func buildModernOceanMonument(input modernStructureBuildInput)(modernStructureStart,bool,error){
	if input.source==nil||input.random==nil{return modernStructureStart{},false,fmt.Errorf("ocean monument requires biome source and generation random")}
	allowed,err:=modernOceanMonumentBiomesAllowed(input);if err!=nil{return modernStructureStart{},false,err};if !allowed{return modernStructureStart{},false,nil}
	orientation:=int(input.random.NextInt(4))
	piece:=modernOceanMonumentPiece{west:input.chunkX*16-29,north:input.chunkZ*16-29,orientation:orientation,seed:input.seed}
	if err:=buildModernMonumentRoomGraph(&piece,input.random);err!=nil{return modernStructureStart{},false,err}
	piece.children=fitModernMonumentRooms(&piece,input.random)
	return modernStructureStart{name:input.name,pieces:[]modernStructurePiece{piece},box:piece.Bounds(),terrain:javastructure.TerrainNone},true,nil
}
func modernOceanMonumentBiomesAllowed(input modernStructureBuildInput)(bool,error){
	centerX,centerZ:=input.chunkX*16+9,input.chunkZ*16+9
	for zIndex:=range 15{for xIndex:=range 15{qx,qz:=(centerX>>2)+int32(xIndex-7),(centerZ>>2)+int32(zIndex-7);for qy:=int32((63-29)>>2);qy<=int32((63+29)>>2);qy++{biome:=normalizeJavaResource(input.source.JavaNoiseBiomeAt(qx,qy,qz));if !strings.Contains(biome,":"){biome="minecraft:"+biome};allowed,err:=javadata.BiomeTagContains("#minecraft:required_ocean_monument_surrounding",biome);if err!=nil{return false,err};if !allowed{return false,nil}}}}
	return true,nil
}

func buildModernMonumentRoomGraph(piece *modernOceanMonumentPiece,random *java26.Random)error{
	for i:=range piece.rooms{piece.rooms[i]=newModernMonumentRoom(i);piece.rooms[i].index=-1}
	roomIndex:=func(x,y,z int)int{return y*25+z*5+x}
	for x:=range 5{for z:=range 4{for y:=range 2{index:=roomIndex(x,y,z);piece.rooms[index]=newModernMonumentRoom(index)}}}
	for x:=1;x<4;x++{for z:=range 2{index:=roomIndex(x,2,z);piece.rooms[index]=newModernMonumentRoom(index)}}
	piece.rooms[roomIndex(2,0,0)].source=true
	dirs:=[6][3]int{{0,-1,0},{0,1,0},{0,0,-1},{0,0,1},{-1,0,0},{1,0,0}}
	for x:=range 5{for z:=range 5{for y:=range 3{index:=roomIndex(x,y,z);if piece.rooms[index].index<0{continue};for d,delta:=range dirs{nx,ny,nz:=x+delta[0],y+delta[1],z+delta[2];if nx<0||nx>=5||ny<0||ny>=3||nz<0||nz>=5{continue};other:=roomIndex(nx,ny,nz);if piece.rooms[other].index<0{continue};direction:=d;if nz!=z{direction=oppositeMonumentDirection(d)};setModernMonumentConnection(piece,direction,index,other)}}}}
	piece.rooms[1003]=newModernMonumentRoom(1003);piece.rooms[1001]=newModernMonumentRoom(1001);piece.rooms[1002]=newModernMonumentRoom(1002)
	setModernMonumentConnection(piece,monumentUp,roomIndex(2,2,0),1003)
	setModernMonumentConnection(piece,monumentSouth,roomIndex(0,1,0),1001)
	setModernMonumentConnection(piece,monumentSouth,roomIndex(4,1,0),1002)
	piece.rooms[1003].claimed=true;piece.rooms[1001].claimed=true;piece.rooms[1002].claimed=true
	piece.core=roomIndex(int(random.NextInt(4)),0,2);piece.rooms[piece.core].claimed=true
	east:=piece.rooms[piece.core].connections[monumentEast];north:=piece.rooms[piece.core].connections[monumentNorth];up:=piece.rooms[piece.core].connections[monumentUp]
	for _,index:=range []int{east,north,up}{if index>=0{piece.rooms[index].claimed=true}}
	if east>=0{for _,index:=range []int{piece.rooms[east].connections[monumentNorth],piece.rooms[east].connections[monumentUp]}{if index>=0{piece.rooms[index].claimed=true}}}
	if north>=0{index:=piece.rooms[north].connections[monumentUp];if index>=0{piece.rooms[index].claimed=true}}
	if east>=0&&piece.rooms[east].connections[monumentNorth]>=0{index:=piece.rooms[piece.rooms[east].connections[monumentNorth]].connections[monumentUp];if index>=0{piece.rooms[index].claimed=true}}
	order:=make([]int,0,46)
	for i:=range 75{if piece.rooms[i].index>=0{order=append(order,i);for direction:=range 6{piece.rooms[i].hasOpening[direction]=piece.rooms[i].connections[direction]>=0}}}
	piece.rooms[1003].hasOpening[monumentDown]=true
	for n:=range len(order)-1{i:=len(order)-1-n;j:=int(random.NextInt(int32(i+1)));order[i],order[j]=order[j],order[i]}
	scan:=1
	for _,index:=range order{room:=&piece.rooms[index];closed,attempts:=0,0;for closed<2&&attempts<5{attempts++;direction:=int(random.NextInt(6));other:=room.connections[direction];if other<0||!room.hasOpening[direction]{continue};opposite:=oppositeMonumentDirection(direction);room.hasOpening[direction]=false;piece.rooms[other].hasOpening[opposite]=false;firstScan:=scan;scan++;if modernMonumentFindSource(piece,index,firstScan){secondScan:=scan;scan++;if modernMonumentFindSource(piece,other,secondScan){closed++;continue}};room.hasOpening[direction]=true;piece.rooms[other].hasOpening[opposite]=true}}
	piece.roomOrder=order
	piece.rooms[roomIndex(2,0,0)].claimed=true
	return nil
}
func newModernMonumentRoom(index int)modernMonumentRoom{room:=modernMonumentRoom{index:index};for direction:=range 6{room.connections[direction]=-1};return room}
func setModernMonumentConnection(piece *modernOceanMonumentPiece,direction,a,b int){opposite:=oppositeMonumentDirection(direction);piece.rooms[a].connections[direction]=b;piece.rooms[b].connections[opposite]=a}
func oppositeMonumentDirection(direction int)int{switch direction{case monumentDown:return monumentUp;case monumentUp:return monumentDown;case monumentNorth:return monumentSouth;case monumentSouth:return monumentNorth;case monumentWest:return monumentEast;default:return monumentWest}}
func modernMonumentFindSource(piece *modernOceanMonumentPiece,index,scan int)bool{room:=&piece.rooms[index];if room.source{return true};room.scanIndex=scan;for direction:=range 6{next:=room.connections[direction];if next>=0&&room.hasOpening[direction]&&piece.rooms[next].scanIndex!=scan&&modernMonumentFindSource(piece,next,scan){return true}};return false}
