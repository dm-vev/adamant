package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("woodland_mansion", buildModernWoodlandMansion) }

type modernMansionPiece struct {
	template *javastructure.Template
	origin [3]int32
	rotation javastructure.Rotation
	mirror javastructure.Mirror
	seed int64
	box javastructure.Box
}
func (p modernMansionPiece) Bounds() javastructure.Box { return p.box }

func (p modernMansionPiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement, error) {
	if env.Random==nil{return javastructure.Placement{},fmt.Errorf("woodland mansion placement requires Java structure random")}
	placed,err:=p.template.PlaceBlocks(0,p.origin,p.rotation,p.mirror,clip,p.seed)
	if err!=nil{return javastructure.Placement{},err}
	for i := range placed.Blocks {
		block := &placed.Blocks[i]
		javastructure.SeedRandomizableContainerLoot(block, env.Random)
		if block.State.Name != "minecraft:structure_block" {
			continue
		}
		marker, _ := block.NBT["metadata"].(string)
		switch marker {
		case "ChestWest", "ChestEast", "ChestSouth", "ChestNorth":
			facing := "north"
			switch marker {
			case "ChestWest":
				facing = "west"
			case "ChestEast":
				facing = "east"
			case "ChestSouth":
				facing = "south"
			}
			state := javastructure.TransformState(world.BlockState{
				Name: "minecraft:chest", Properties: map[string]any{"facing": facing},
			}, javastructure.NoMirror, p.rotation)
			placed.PostShapeBlocks = append(placed.PostShapeBlocks, javastructure.Block{
				Pos: block.Pos, State: state,
				NBT: map[string]any{"id": "minecraft:chest", "LootTable": "minecraft:chests/woodland_mansion", "LootTableSeed": env.Random.NextLong()},
			})
		case "Mage", "Warrior", "Group of Allays":
			id, count := "minecraft:evoker", 1
			if marker == "Warrior" {
				id = "minecraft:vindicator"
			} else if marker == "Group of Allays" {
				id, count = "minecraft:allay", int(env.Random.NextInt(3))+1
			}
			for index := 0; index < count; index++ {
				placed.PostShapeEntities = append(placed.PostShapeEntities, javastructure.Entity{
					Pos: [3]float64{float64(block.Pos[0]) + 0.5, float64(block.Pos[1]), float64(block.Pos[2]) + 0.5},
					BlockPos: block.Pos, Record: int32(index),
					NBT: map[string]any{"id": id, "PersistenceRequired": byte(1)},
				})
			}
			placed.PostShapeBlocks = append(placed.PostShapeBlocks, javastructure.Block{
				Pos: block.Pos, State: world.BlockState{Name: "minecraft:air"},
			})
		}
	}
	return placed, nil
}

const (
 mansionClear=0; mansionCorridor=1; mansionRoom=2; mansionStartRoom=3; mansionBlocked=5
 mansionRoom1x1=65536; mansionRoom1x2=131072; mansionRoom2x2=262144
 mansionOriginFlag=1048576; mansionDoorFlag=2097152; mansionStairsFlag=4194304; mansionCorridorFlag=8388608
 mansionRoomTypeMask=983040; mansionRoomIDMask=65535
)
type mansionGrid struct { w,h int; outside int; v [11][11]int }
func newMansionGrid(w,h,out int) *mansionGrid { return &mansionGrid{w:w,h:h,outside:out} }
func (g *mansionGrid) get(x,y int) int { if x<0||x>=g.w||y<0||y>=g.h{return g.outside};return g.v[x][y] }
func (g *mansionGrid) set(x,y,v int) { if x>=0&&x<g.w&&y>=0&&y<g.h{g.v[x][y]=v} }
func (g *mansionGrid) rect(x0,y0,x1,y1,v int) { for y:=y0;y<=y1;y++{for x:=x0;x<=x1;x++{g.set(x,y,v)}} }
func (g *mansionGrid) setif(x,y,ifv,v int) { if g.get(x,y)==ifv{g.set(x,y,v)} }
func (g *mansionGrid) edgesTo(x,y,v int) bool { return g.get(x-1,y)==v||g.get(x+1,y)==v||g.get(x,y+1)==v||g.get(x,y-1)==v }
func mansionHouse(g *mansionGrid,x,y int) bool { v:=g.get(x,y);return v==1||v==2||v==3||v==4 }
type mansionDir int
const ( mansionNorth mansionDir=iota; mansionEast; mansionSouth; mansionWest; mansionUp; mansionNoDoor )
func (d mansionDir) delta()(int,int){switch d{case mansionNorth:return 0,-1;case mansionEast:return 1,0;case mansionSouth:return 0,1;case mansionWest:return -1,0};return 0,0}
func (d mansionDir) cw() mansionDir{return (d+1)%4};func(d mansionDir) ccw()mansionDir{return(d+3)%4};func(d mansionDir) opposite()mansionDir{return(d+2)%4}
func mansionShuffle(a [][2]int,r *java26.Random) { for i:=len(a)-1;i>0;i--{j:=int(r.NextInt(int32(i+1)));a[i],a[j]=a[j],a[i]} }
type mansionPlan struct { base,third *mansionGrid; rooms [3]*mansionGrid; sx,sy int; random *java26.Random }
func newMansionPlan(r *java26.Random)*mansionPlan{
	m:=&mansionPlan{base:newMansionGrid(11,11,5),sx:8,sy:5,random:r}
	m.base.rect(7,4,7,5,mansionStartRoom);m.base.rect(6,4,6,5,mansionRoom)
	m.base.rect(9,2,10,7,mansionBlocked)
	m.base.set(8,2,mansionCorridor);m.base.set(8,3,mansionCorridor);m.base.set(8,6,mansionCorridor);m.base.set(8,7,mansionCorridor);m.base.set(6,3,mansionCorridor);m.base.set(6,6,mansionCorridor)
	m.base.rect(0,0,10,1,mansionBlocked);m.base.rect(0,9,10,10,mansionBlocked)
	m.corridor(m.base,7,2,mansionWest,6);m.corridor(m.base,7,7,mansionWest,6);m.corridor(m.base,5,3,mansionWest,3);m.corridor(m.base,5,6,mansionWest,3)
	for m.clean(m.base){}
	for i:=range m.rooms{m.rooms[i]=newMansionGrid(11,11,5)}
	m.identify(m.base,m.rooms[0]);m.identify(m.base,m.rooms[1])
	m.rooms[0].set(8,4,mansionCorridorFlag);m.rooms[0].set(8,5,mansionCorridorFlag);m.rooms[1].set(8,4,mansionCorridorFlag);m.rooms[1].set(8,5,mansionCorridorFlag)
	m.third=newMansionGrid(11,11,5);m.setupThird();m.identify(m.third,m.rooms[2]);return m
}
func(m *mansionPlan)corridor(g *mansionGrid,x,y int,d mansionDir,depth int){if depth<=0{return};g.set(x,y,mansionCorridor);dx,dy:=d.delta();g.setif(x+dx,y+dy,mansionClear,mansionCorridor);for i:=0;i<8;i++{n:=[]mansionDir{mansionSouth,mansionWest,mansionNorth,mansionEast}[m.random.NextInt(4)];if n==d.opposite()||(n==mansionEast&&m.random.NextBoolean()){continue};nx,ny:=x+dx,y+dy;ex,ey:=n.delta();if g.get(nx+ex,ny+ey)==0&&g.get(nx+2*ex,ny+2*ey)==0{m.corridor(g,x+dx+ex,y+dy+ey,n,depth-1);break}}
	cwx,cwy:=d.cw().delta();ccwx,ccwy:=d.ccw().delta();g.setif(x+cwx,y+cwy,0,mansionRoom);g.setif(x+ccwx,y+ccwy,0,mansionRoom);g.setif(x+dx+cwx,y+dy+cwy,0,mansionRoom);g.setif(x+dx+ccwx,y+dy+ccwy,0,mansionRoom);g.setif(x+2*dx,y+2*dy,0,mansionRoom);g.setif(x+2*cwx,y+2*cwy,0,mansionRoom);g.setif(x+2*ccwx,y+2*ccwy,0,mansionRoom)
}
func(m *mansionPlan)clean(g *mansionGrid)bool{t:=false;for y:=0;y<g.h;y++{for x:=0;x<g.w;x++{if g.get(x,y)!=0{continue};n:=0;for _,p:=range [][2]int{{x+1,y},{x-1,y},{x,y+1},{x,y-1}}{if mansionHouse(g,p[0],p[1]){n++}};if n>=3{g.set(x,y,2);t=true}else if n==2{d:=0;for _,p:=range [][2]int{{x+1,y+1},{x-1,y+1},{x+1,y-1},{x-1,y-1}}{if mansionHouse(g,p[0],p[1]){d++}};if d<=1{g.set(x,y,2);t=true}}}};return t}
func(m *mansionPlan)identify(src,dst *mansionGrid){positions:=make([][2]int,0);for y:=0;y<src.h;y++{for x:=0;x<src.w;x++{if src.get(x,y)==2{positions=append(positions,[2]int{x,y})}}};mansionShuffle(positions,m.random);id:=10
 for _,p:=range positions{x,y:=p[0],p[1];if dst.get(x,y)!=0{continue};x0,x1,y0,y1,typ:=x,x,y,y,mansionRoom1x1
 switch {case dst.get(x+1,y)==0&&dst.get(x,y+1)==0&&dst.get(x+1,y+1)==0&&src.get(x+1,y)==2&&src.get(x,y+1)==2&&src.get(x+1,y+1)==2:x1++;y1++;typ=mansionRoom2x2
 case dst.get(x-1,y)==0&&dst.get(x,y+1)==0&&dst.get(x-1,y+1)==0&&src.get(x-1,y)==2&&src.get(x,y+1)==2&&src.get(x-1,y+1)==2:x0--;y1++;typ=mansionRoom2x2
 case dst.get(x-1,y)==0&&dst.get(x,y-1)==0&&dst.get(x-1,y-1)==0&&src.get(x-1,y)==2&&src.get(x,y-1)==2&&src.get(x-1,y-1)==2:x0--;y0--;typ=mansionRoom2x2
 case dst.get(x+1,y)==0&&src.get(x+1,y)==2:x1++;typ=mansionRoom1x2
 case dst.get(x,y+1)==0&&src.get(x,y+1)==2:y1++;typ=mansionRoom1x2
 case dst.get(x-1,y)==0&&src.get(x-1,y)==2:x0--;typ=mansionRoom1x2
 case dst.get(x,y-1)==0&&src.get(x,y-1)==2:y0--;typ=mansionRoom1x2 }
 dx:=x0;if m.random.NextBoolean(){dx=x1};dy:=y0;if m.random.NextBoolean(){dy=y1};flag:=mansionDoorFlag;if !src.edgesTo(dx,dy,1){if dx==x0{dx=x1}else{dx=x0};if dy==y0{dy=y1}else{dy=y0};if !src.edgesTo(dx,dy,1){if dy==y0{dy=y1}else{dy=y0};if !src.edgesTo(dx,dy,1){if dx==x0{dx=x1}else{dx=x0};if dy==y0{dy=y1}else{dy=y0};if !src.edgesTo(dx,dy,1){flag=0;dx=x0;dy=y0}}}}
 for yy:=y0;yy<=y1;yy++{for xx:=x0;xx<=x1;xx++{v:=typ|id;if xx==dx&&yy==dy{v|=mansionOriginFlag|flag};dst.set(xx,yy,v)}};id++ }
}
func(m *mansionPlan)roomDirection(g *mansionGrid,x,y,floor,id int)mansionDir{for d:=mansionNorth;d<=mansionWest;d++{dx,dy:=d.delta();if m.rooms[floor].get(x+dx,y+dy)&mansionRoomIDMask==id{return d}};return mansionNorth}
func(m *mansionPlan)setupThird(){potential:=make([][2]int,0);f:=m.rooms[1];for y:=0;y<m.third.h;y++{for x:=0;x<m.third.w;x++{v:=f.get(x,y);if v&mansionRoomTypeMask==mansionRoom1x2&&v&mansionDoorFlag!=0{potential=append(potential,[2]int{x,y})}}};if len(potential)==0{m.third.rect(0,0,10,10,5);return};p:=potential[m.random.NextInt(int32(len(potential)))];x,y:=p[0],p[1];v:=f.get(x,y);f.set(x,y,v|mansionStairsFlag);d:=m.roomDirection(m.base,x,y,1,v&mansionRoomIDMask);dx,dy:=d.delta();ex,ey:=x+dx,y+dy
 for yy:=0;yy<11;yy++{for xx:=0;xx<11;xx++{if !mansionHouse(m.base,xx,yy){m.third.set(xx,yy,5)}else if xx==x&&yy==y{m.third.set(xx,yy,3)}else if xx==ex&&yy==ey{m.third.set(xx,yy,3);m.rooms[2].set(xx,yy,mansionCorridorFlag)}}}
 dirs:=make([]mansionDir,0,4);for d:=mansionNorth;d<=mansionWest;d++{ax,ay:=d.delta();if m.third.get(ex+ax,ey+ay)==0{dirs=append(dirs,d)}};if len(dirs)==0{m.third.rect(0,0,10,10,5);f.set(x,y,v);return};d=dirs[m.random.NextInt(int32(len(dirs)))];ax,ay:=d.delta();m.corridor(m.third,ex+ax,ey+ay,d,4);for m.clean(m.third){} }

func buildModernWoodlandMansion(in modernStructureBuildInput)(modernStructureStart,bool,error){
	if in.random==nil||in.source==nil{return modernStructureStart{},false,fmt.Errorf("woodland mansion requires structure random and world source")}
	rotation:=javastructure.Rotation(in.random.NextInt(4));x,z:=in.chunkX*16+7,in.chunkZ*16+7
	dx,dz:=int32(5),int32(5);switch rotation{case javastructure.Clockwise90:dx=-5;case javastructure.Clockwise180:dx,dz=-5,-5;case javastructure.CounterClockwise90:dz=-5}
	low:=int32(1<<30);for _,p:=range [][2]int32{{x+dx,z+dz},{x+dx+5,z+dz},{x+dx,z+dz+5},{x+dx+5,z+dz+5}}{if y:=in.source.BaseHeight(p[0],p[1],false);y<low{low=y}}
	if low<60{return modernStructureStart{},false,nil}
	origin:=[3]int32{x,low,z};plan:=newMansionPlan(in.random);placer:=newMansionPlacer(origin,rotation,in.seed,in.random)
	if err:=placer.create(plan);err!=nil{return modernStructureStart{},false,err}
	box:=emptyStructureBox();for _,piece:=range placer.pieces{box=unionStructureBox(box,piece.Bounds())}
	return modernStructureStart{name:in.name,pieces:placer.pieces,box:box,terrain:terrainAdjustment(stringValue(in.config["terrain_adaptation"]))},true,nil
}
