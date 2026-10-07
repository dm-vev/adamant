package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("mineshaft", buildModernMineshaft) }

type minePiece struct {
	box javastructure.Box
	kind, facing, wood string
	sections int
	rails, spider bool
	seed int64
	entrances []javastructure.Box
}
func (p minePiece) Bounds() javastructure.Box { return p.box }
func (p minePiece) Place(clip javastructure.Bounds, env javastructure.ProcessorEnvironment) (javastructure.Placement,error) {
	if mineInvalid(p,clip,env){return javastructure.Placement{},nil}
	out:=javastructure.Placement{}
	put:=func(x,y,z int32,name string,props map[string]any) { if x<clip.Min[0]||x>clip.Max[0]||y<clip.Min[1]||y>clip.Max[1]||z<clip.Min[2]||z>clip.Max[2] {return}; out.Blocks=append(out.Blocks,javastructure.Block{Pos:[3]int32{x,y,z},State:world.BlockState{Name:"minecraft:"+name,Properties:props}}) }
	plank,log,fence:="oak_planks","oak_log","oak_fence"; if p.wood=="mesa" {plank,log,fence="dark_oak_planks","dark_oak_log","dark_oak_fence"}
	min,max:=p.box.Min,p.box.Max
	corridorLength:=int32(p.sections*5-1);random:=env.Random
	if p.kind=="corridor" {
		for y:=int32(0);y<=1;y++{for x:=int32(0);x<=2;x++{for z:=int32(0);z<=corridorLength;z++{wx,wy,wz:=mineLocal(p,x,y,z);put(wx,wy,wz,"cave_air",nil)}}}
		for y:=int32(2);y<=2;y++{for x:=int32(0);x<=2;x++{for z:=int32(0);z<=corridorLength;z++{if random.NextFloat()<0.8{wx,wy,wz:=mineLocal(p,x,y,z);put(wx,wy,wz,"cave_air",nil)}}}}
		if p.spider{for y:=int32(0);y<=1;y++{for x:=int32(0);x<=2;x++{for z:=int32(0);z<=corridorLength;z++{if random.NextFloat()<0.6{wx,wy,wz:=mineLocal(p,x,y,z);put(wx,wy,wz,"cobweb",nil)}}}}}
	} else if p.kind=="room" {
		for x:=min[0];x<=max[0];x++{for z:=min[2];z<=max[2];z++{for y:=min[1]+1;y<=min32(min[1]+3,max[1]);y++{put(x,y,z,"cave_air",nil)}}}
		dx:=float32(max[0]-min[0]+1);dy:=float32(max[1]-(min[1]+4)+1);dz:=float32(max[2]-min[2]+1);cx:=float32(min[0])+dx/2;cz:=float32(min[2])+dz/2
		for y:=min[1]+4;y<=max[1];y++{ny:=float32(y-(min[1]+4))/dy;for x:=min[0];x<=max[0];x++{nx:=(float32(x)-cx)/(dx*0.5);for z:=min[2];z<=max[2];z++{nz:=(float32(z)-cz)/(dz*0.5);if nx*nx+ny*ny+nz*nz<=1.05{put(x,y,z,"cave_air",nil)}}}}
		for _,entrance:=range p.entrances{for y:=entrance.Max[1]-2;y<=entrance.Max[1];y++{for x:=entrance.Min[0];x<=entrance.Max[0];x++{for z:=entrance.Min[2];z<=entrance.Max[2];z++{put(x,y,z,"cave_air",nil)}}}}
	}
	if p.kind=="corridor"{
		
		spiderPlaced:=false
		for section:=range p.sections{
			z:=int32(2+section*5);supported:=true
			for x:=int32(0);x<=2;x++{wx,wy,wz:=mineLocal(p,x,3,z);if wx<clip.Min[0]||wx>clip.Max[0]||wy<clip.Min[1]||wy>clip.Max[1]||wz<clip.Min[2]||wz>clip.Max[2]||!mineSolid(env.BaseBlock(wx,wy,wz)){supported=false}}
			if supported{
				for y:=int32(0);y<2;y++{wx,wy,wz:=mineLocal(p,0,y,z);put(wx,wy,wz,fence,map[string]any{mineWorldDirection(p.facing,"west"):true,"waterlogged":false});wx,wy,wz=mineLocal(p,2,y,z);put(wx,wy,wz,fence,map[string]any{mineWorldDirection(p.facing,"east"):true,"waterlogged":false})}
				if random.NextInt(4)==0{for _,x:=range []int32{0,2}{wx,wy,wz:=mineLocal(p,x,2,z);put(wx,wy,wz,plank,nil)}}else{
					for x:=int32(0);x<=2;x++{wx,wy,wz:=mineLocal(p,x,2,z);put(wx,wy,wz,plank,nil)}
					for _,torchZ:=range []int32{z-1,z+1}{if random.NextFloat()<0.05{wx,wy,wz:=mineLocal(p,1,2,torchZ);facing:="north";if torchZ<z{facing="south"};put(wx,wy,wz,"wall_torch",map[string]any{"facing":mineWorldDirection(p.facing,facing),"lit":true})}}
				}
			}
			for _,candidate:=range [][3]int32{{0,2,z-1},{2,2,z-1},{0,2,z+1},{2,2,z+1}}{mineMaybeCobweb(p,clip,env,&out,random,0.1,candidate[0],candidate[1],candidate[2])}
			for _,candidate:=range [][3]int32{{0,2,z-2},{2,2,z-2},{0,2,z+2},{2,2,z+2}}{mineMaybeCobweb(p,clip,env,&out,random,0.05,candidate[0],candidate[1],candidate[2])}
			for _,spot:=range [][3]int32{{2,0,z-1},{0,0,z+1}}{if random.NextInt(100)==0{mineLootChest(p,clip,env,&out,random,spot[0],spot[1],spot[2],"minecraft:chests/abandoned_mineshaft")}}
			if p.spider&&!spiderPlaced{sz:=z-1+random.NextInt(3);wx,wy,wz:=mineLocal(p,1,0,sz);if wx>=clip.Min[0]&&wx<=clip.Max[0]&&wy>=clip.Min[1]&&wy<=clip.Max[1]&&wz>=clip.Min[2]&&wz<=clip.Max[2]&&mineInterior(p,clip,env,1,0,sz){spiderPlaced=true;put(wx,wy,wz,"spawner",nil);for i:=range out.Blocks{if out.Blocks[i].Pos==[3]int32{wx,wy,wz}{out.Blocks[i].NBT=map[string]any{"id":"minecraft:mob_spawner","SpawnData":map[string]any{"entity":map[string]any{"id":"minecraft:cave_spider"}}}}}}}
		}
		for z:=int32(0);z<=corridorLength;z++{for x:=int32(0);x<=2;x++{if !mineInterior(p,clip,env,x,-1,z){continue};block:=mineBlock(env,p,x,-1,z);if !mineSolid(block){wx,wy,wz:=mineLocal(p,x,-1,z);put(wx,wy,wz,plank,nil)}}}
		if p.sections>0{last:=int32(2);if p.sections>1{last=corridorLength-2};for _,z:=range []int32{2,last}{for _,x:=range []int32{0,2}{if mineInterior(p,clip,env,x,-1,z)&&!mineSolid(mineBlock(env,p,x,-1,z)){mineSupportColumn(p,clip,env,&out,x,-1,z,log,fence)}}}}
		if p.rails{for z:=int32(0);z<=corridorLength;z++{wx,wy,wz:=mineLocal(p,1,0,z);if wx<clip.Min[0]||wx>clip.Max[0]||wy<clip.Min[1]||wy>clip.Max[1]||wz<clip.Min[2]||wz>clip.Max[2]{continue};if mineSolid(mineBlock(env,p,1,-1,z)){chance:=float32(0.9);if mineInterior(p,clip,env,1,0,z){chance=0.7};if random.NextFloat()<chance{put(wx,wy,wz,"rail",map[string]any{"shape":mineRailShape(p.facing),"waterlogged":false})}}}}
	}
	if p.kind=="crossing"{
		two:=max[1]-min[1]>3;lo,hi:=min[1],max[1]
		for x:=min[0]+1;x<=max[0]-1;x++{for z:=min[2];z<=max[2];z++{for y:=lo;y<=hi;y++{if !two||y<=lo+2||y>=hi-2||y==lo+3{put(x,y,z,"cave_air",nil)}}}}
		for x:=min[0];x<=max[0];x++{for z:=min[2]+1;z<=max[2]-1;z++{for y:=lo;y<=hi;y++{if !two||y<=lo+2||y>=hi-2||y==lo+3{put(x,y,z,"cave_air",nil)}}}}
		for _,corner:=range [][2]int32{{min[0]+1,min[2]+1},{min[0]+1,max[2]-1},{max[0]-1,min[2]+1},{max[0]-1,max[2]-1}}{if mineSolid(env.BaseBlock(corner[0],hi+1,corner[1])){for y:=lo;y<=hi;y++{put(corner[0],y,corner[1],plank,nil)}}}
		for x:=min[0];x<=max[0];x++{for z:=min[2];z<=max[2];z++{if x<clip.Min[0]||x>clip.Max[0]||lo-1<clip.Min[1]||lo-1>clip.Max[1]||z<clip.Min[2]||z>clip.Max[2]||lo-1>=env.BaseHeight(x,z,true){continue};if !mineSolid(env.BaseBlock(x,lo-1,z)){put(x,lo-1,z,plank,nil)}}}
	}
	if p.kind=="stairs"{
		for x:=int32(0);x<=2;x++{for y:=int32(5);y<=7;y++{for z:=int32(0);z<=1;z++{wx,wy,wz:=mineLocal(p,x,y,z);put(wx,wy,wz,"cave_air",nil)}}}
		for x:=int32(0);x<=2;x++{for y:=int32(0);y<=2;y++{for z:=int32(7);z<=8;z++{wx,wy,wz:=mineLocal(p,x,y,z);put(wx,wy,wz,"cave_air",nil)}}}
		for i:=int32(0);i<5;i++{y0:=int32(5)-i;if i<4{y0--};y1:=int32(7)-i;for x:=int32(0);x<=2;x++{for y:=y0;y<=y1;y++{wx,wy,wz:=mineLocal(p,x,y,2+i);put(wx,wy,wz,"cave_air",nil)}}}
	}
	return out,nil
}

func mineLocal(p minePiece,x,y,z int32)(int32,int32,int32){
	b:=p.box
	switch p.facing{case "north":return b.Min[0]+x,b.Min[1]+y,b.Max[2]-z;case "south":return b.Min[0]+x,b.Min[1]+y,b.Min[2]+z;case "west":return b.Max[0]-z,b.Min[1]+y,b.Min[2]+x;default:return b.Min[0]+z,b.Min[1]+y,b.Min[2]+x}
}
func mineBlock(env javastructure.ProcessorEnvironment,p minePiece,x,y,z int32)world.Block{wx,wy,wz:=mineLocal(p,x,y,z);return env.BaseBlock(wx,wy,wz)}
func mineAir(b world.Block)bool{if b==nil{return false};name,_:=b.EncodeBlock();return name=="minecraft:air"||name=="minecraft:cave_air"||name=="minecraft:void_air"}
func mineSolid(b world.Block)bool{if b==nil{return false};name,_:=b.EncodeBlock();return !mineAir(b)&&name!="minecraft:water"&&name!="minecraft:lava"}
func mineRailShape(facing string)string{if facing=="east"||facing=="west"{return "east_west"};return "north_south"}
func mineWorldDirection(facing,local string)string{
	switch facing{case "south":return local;case "north":switch local{case "north":return "south";case "south":return "north";case "east":return "west";default:return "east"};case "east":switch local{case "north":return "west";case "south":return "east";case "east":return "north";default:return "south"};default:switch local{case "north":return "east";case "south":return "west";case "east":return "south";default:return "north"}}
}
func mineInvalid(p minePiece,clip javastructure.Bounds,env javastructure.ProcessorEnvironment)bool{
	b:=p.box;x0:=max32(b.Min[0]-1,clip.Min[0]);x1:=min32(b.Max[0]+1,clip.Max[0]);y0:=max32(b.Min[1]-1,clip.Min[1]);y1:=min32(b.Max[1]+1,clip.Max[1]);z0:=max32(b.Min[2]-1,clip.Min[2]);z1:=min32(b.Max[2]+1,clip.Max[2])
	if x0>x1||y0>y1||z0>z1{return false}
	if env.JavaBiomeAt!=nil&&env.JavaBiomeAt((x0+x1)/2,(y0+y1)/2,(z0+z1)/2)=="minecraft:deep_dark"{return true}
	if env.BaseBlock==nil{return false}
	liquid:=func(x,y,z int32)bool{n,_:=env.BaseBlock(x,y,z).EncodeBlock();return n=="minecraft:water"||n=="minecraft:lava"}
	for x:=x0;x<=x1;x++{for z:=z0;z<=z1;z++{if liquid(x,y0,z)||liquid(x,y1,z){return true}}}
	for x:=x0;x<=x1;x++{for y:=y0;y<=y1;y++{if liquid(x,y,z0)||liquid(x,y,z1){return true}}}
	for z:=z0;z<=z1;z++{for y:=y0;y<=y1;y++{if liquid(x0,y,z)||liquid(x1,y,z){return true}}}
	return false
}
func mineStructureReplaceable(b world.Block)bool{
	if b==nil{return true}
	name,_:=b.EncodeBlock()
	return mineAir(b)||name=="minecraft:water"||name=="minecraft:lava"||name=="minecraft:glow_lichen"||name=="minecraft:seagrass"||name=="minecraft:tall_seagrass"
}
func mineSupportColumn(p minePiece,clip javastructure.Bounds,env javastructure.ProcessorEnvironment,out *javastructure.Placement,x,y,z int32,log,fence string){
	for distance:=int32(1);distance<=20;distance++{below:=mineBlock(env,p,x,y-distance,z);name:="";if below!=nil{name,_=below.EncodeBlock()};empty:=mineStructureReplaceable(below)&&name!="minecraft:lava";if !empty{if mineSolid(below){for yy:=y-distance+1;yy<y;yy++{mineAppendLocal(p,clip,out,x,yy,z,log,map[string]any{"axis":"y"})};return};break}}
	for distance:=int32(1);distance<=50;distance++{above:=mineBlock(env,p,x,y+distance,z);name:="";if above!=nil{name,_=above.EncodeBlock()};if !mineStructureReplaceable(above)&&name!="minecraft:sand"&&name!="minecraft:gravel"&&name!="minecraft:anvil"{mineAppendLocal(p,clip,out,x,y+1,z,fence,map[string]any{"north":false,"south":false,"east":false,"west":false,"waterlogged":false});for yy:=y+2;yy<y+distance;yy++{mineAppendLocal(p,clip,out,x,yy,z,"iron_chain",map[string]any{"axis":"y","waterlogged":false})};return}}
}
func mineAppendLocal(p minePiece,clip javastructure.Bounds,out *javastructure.Placement,x,y,z int32,name string,props map[string]any){
	wx,wy,wz:=mineLocal(p,x,y,z);if wx<clip.Min[0]||wx>clip.Max[0]||wy<clip.Min[1]||wy>clip.Max[1]||wz<clip.Min[2]||wz>clip.Max[2]{return};out.Blocks=append(out.Blocks,javastructure.Block{Pos:[3]int32{wx,wy,wz},State:world.BlockState{Name:"minecraft:"+name,Properties:props}})
}
func mineInterior(p minePiece,clip javastructure.Bounds,env javastructure.ProcessorEnvironment,x,y,z int32)bool{
	wx,wy,wz:=mineLocal(p,x,y,z)
	return wx>=clip.Min[0]&&wx<=clip.Max[0]&&wy>=clip.Min[1]&&wy<=clip.Max[1]&&wz>=clip.Min[2]&&wz<=clip.Max[2]&&wy<env.BaseHeight(wx,wz,true)
}
func mineMaybeCobweb(p minePiece,clip javastructure.Bounds,env javastructure.ProcessorEnvironment,out *javastructure.Placement,random *java26.Random,probability float32,x,y,z int32){
	wx,wy,wz:=mineLocal(p,x,y,z)
	if wx<clip.Min[0]||wx>clip.Max[0]||wy<clip.Min[1]||wy>clip.Max[1]||wz<clip.Min[2]||wz>clip.Max[2]||wy>=env.BaseHeight(wx,wz,true){return}
	if random.NextFloat()>=probability{return}
	sturdy:=0
	for _,d:=range [][3]int32{{1,0,0},{-1,0,0},{0,1,0},{0,-1,0},{0,0,1},{0,0,-1}}{
		nx,ny,nz:=wx+d[0],wy+d[1],wz+d[2]
		if nx<clip.Min[0]||nx>clip.Max[0]||ny<clip.Min[1]||ny>clip.Max[1]||nz<clip.Min[2]||nz>clip.Max[2]{continue}
		if mineSolid(env.BaseBlock(nx,ny,nz)){sturdy++;if sturdy>=2{put:=javastructure.Block{Pos:[3]int32{wx,wy,wz},State:world.BlockState{Name:"minecraft:cobweb"}};out.Blocks=append(out.Blocks,put);return}}
	}
}
func mineLootChest(p minePiece,clip javastructure.Bounds,env javastructure.ProcessorEnvironment,out *javastructure.Placement,random *java26.Random,x,y,z int32,loot string){
	wx,wy,wz:=mineLocal(p,x,y,z)
	if wx<clip.Min[0]||wx>clip.Max[0]||wy<clip.Min[1]||wy>clip.Max[1]||wz<clip.Min[2]||wz>clip.Max[2]{return}
	if name,_:=env.BaseBlock(wx,wy,wz).EncodeBlock();name=="minecraft:chest"{return}
	out.Blocks=append(out.Blocks,javastructure.Block{Pos:[3]int32{wx,wy,wz},State:world.BlockState{Name:"minecraft:chest",Properties:map[string]any{"facing":mineChestFacing(env,wx,wy,wz),"type":"single","waterlogged":false}},NBT:map[string]any{"id":"minecraft:chest","LootTable":loot,"LootTableSeed":random.NextLong()}})
}
func mineChestFacing(env javastructure.ProcessorEnvironment,x,y,z int32)string{
	directions:=[][4]int32{{0,0,-1,0},{1,0,0,1},{0,0,1,2},{-1,0,0,3}}
	found:=-1
	for _,d:=range directions{if mineSolid(env.BaseBlock(x+d[0],y,z+d[2])){if found>=0{found=-1;break};found=int(d[3])}}
	if found>=0{return []string{"south","west","north","east"}[found]}
	return "north"
}
type mineGraph struct { pieces []minePiece; boxes []javastructure.Box; r *java26.Random; wood string; origin [3]int32 }
func (g *mineGraph) add(p minePiece) bool { for _,b:=range g.boxes {if boxesOverlap(b,p.box){return false}};g.pieces=append(g.pieces,p);g.boxes=append(g.boxes,p.box);return true }
func boxesOverlap(a,b javastructure.Box)bool {for i:=range 3 {if a.Max[i]<b.Min[i]||b.Max[i]<a.Min[i] {return false}};return true}
func (g *mineGraph) child(x, y, z int32, dir string, depth int) {
	if depth > 8 || abs32(x-g.origin[0]) > 80 || abs32(z-g.origin[2]) > 80 { return }
	selection := g.r.NextInt(100)
	p := minePiece{wood:g.wood, facing:dir}
	var box javastructure.Box
	if selection >= 80 {
		height:=int32(2);if g.r.NextInt(4)==0{height=6};p.kind="crossing"
		switch dir {case "north":box=mbox(x-1,y,z-4,x+3,y+height,z);case "south":box=mbox(x-1,y,z,x+3,y+height,z+4);case "west":box=mbox(x-4,y,z-1,x,y+height,z+3);default:box=mbox(x,y,z-1,x+4,y+height,z+3)}
		if !g.canAdd(box){return}
	} else if selection >= 70 {
		p.kind="stairs";switch dir{case "north":box=mbox(x,y-5,z-8,x+2,y+2,z);case "south":box=mbox(x,y-5,z,x+2,y+2,z+8);case "west":box=mbox(x-8,y-5,z,x,y+2,z+2);default:box=mbox(x,y-5,z,x+8,y+2,z+2)}
		if !g.canAdd(box){return}
	} else {
		p.kind="corridor";length:=g.r.NextInt(3)+2
		for ;length>0;length-- {
			n:=length*5
			switch dir{case "north":box=mbox(x,y,z-(n-1),x+2,y+2,z);case "south":box=mbox(x,y,z,x+2,y+2,z+n-1);case "west":box=mbox(x-(n-1),y,z,x,y+2,z+2);default:box=mbox(x,y,z,x+n-1,y+2,z+2)}
			if g.canAdd(box){break}
		}
		if length==0{return}
		p.sections=int(max32(box.Max[0]-box.Min[0]+1,box.Max[2]-box.Min[2]+1)/5)
		p.rails=g.r.NextInt(3)==0
		p.spider=!p.rails&&g.r.NextInt(23)==0
	}
	p.box=box;p.seed=int64(uint64(x)*0x9e3779b97f4a7c15 ^ uint64(y)*0xbf58476d1ce4e5b9 ^ uint64(z))
	if !g.add(p){return}
	g.branch(p,depth)
}
func(g *mineGraph)canAdd(box javastructure.Box)bool{for _,other:=range g.boxes{if boxesOverlap(other,box){return false}};return true}
func (g *mineGraph) branch(p minePiece, depth int) {
	b := p.box
	if p.kind == "room" {
		heightSpace:=b.Max[1]-b.Min[1]-3
		if heightSpace<=0{heightSpace=1}
		for side:=range 4 {
			span:=b.Max[0]-b.Min[0]+1
			if side>=2{span=b.Max[2]-b.Min[2]+1}
			pos:=int32(0)
			for pos<span{
				pos+=g.r.NextInt(span)
				if pos+3>span{break}
				y:=b.Min[1]+g.r.NextInt(heightSpace)+1
				x,z:=b.Min[0]+pos,b.Min[2]+pos
				dir:="north"
				switch side{
				case 0:z=b.Min[2]-1
				case 1:z=b.Max[2]+1;dir="south"
				case 2:x,z=b.Min[0]-1,b.Min[2]+pos;dir="west"
				case 3:x,z=b.Max[0]+1,b.Min[2]+pos;dir="east"
				}
				childIndex:=len(g.pieces)
				g.child(x,y,z,dir,depth+1)
				if childIndex<len(g.pieces){
					cb:=g.pieces[childIndex].box
					entrance:=cb
					switch side{
					case 0:entrance.Min[2]=b.Min[2];entrance.Max[2]=b.Min[2]+1
					case 1:entrance.Min[2]=b.Max[2]-1;entrance.Max[2]=b.Max[2]
					case 2:entrance.Min[0]=b.Min[0];entrance.Max[0]=b.Min[0]+1
					case 3:entrance.Min[0]=b.Max[0]-1;entrance.Max[0]=b.Max[0]
					}
					for i:=range g.pieces{if g.pieces[i].kind=="room"&&g.pieces[i].box==b{g.pieces[i].entrances=append(g.pieces[i].entrances,entrance);break}}
				}
				pos+=4
			}
		}
		return
	}
	if p.kind == "corridor" {
		end := g.r.NextInt(4)
		y := func() int32 { return b.Min[1]-1+g.r.NextInt(3) }
		switch p.facing {
		case "north":
			if end <= 1 { g.child(b.Min[0], y(), b.Min[2]-1, "north", depth+1) } else if end == 2 { g.child(b.Min[0]-1,y(),b.Min[2],"west",depth+1) } else { g.child(b.Max[0]+1,y(),b.Min[2],"east",depth+1) }
		case "south":
			if end <= 1 { g.child(b.Min[0], y(), b.Max[2]+1, "south", depth+1) } else if end == 2 { g.child(b.Min[0]-1,y(),b.Max[2]-3,"west",depth+1) } else { g.child(b.Max[0]+1,y(),b.Max[2]-3,"east",depth+1) }
		case "west":
			if end <= 1 { g.child(b.Min[0]-1,y(),b.Min[2],"west",depth+1) } else if end == 2 { g.child(b.Min[0],y(),b.Min[2]-1,"north",depth+1) } else { g.child(b.Min[0],y(),b.Max[2]+1,"south",depth+1) }
		case "east":
			if end <= 1 { g.child(b.Max[0]+1,y(),b.Min[2],"east",depth+1) } else if end == 2 { g.child(b.Max[0]-3,y(),b.Min[2]-1,"north",depth+1) } else { g.child(b.Max[0]-3,y(),b.Max[2]+1,"south",depth+1) }
		}
		if depth < 8 {
			if p.facing == "north" || p.facing == "south" {
				for x:=b.Min[0]+3; x+3<=b.Max[0]; x+=5 { switch g.r.NextInt(5) { case 0:g.child(x,b.Min[1],b.Min[2]-1,"north",depth+1);case 1:g.child(x,b.Min[1],b.Max[2]+1,"south",depth+1) } }
			} else {
				for z:=b.Min[2]+3; z+3<=b.Max[2]; z+=5 { switch g.r.NextInt(5) { case 0:g.child(b.Min[0]-1,b.Min[1],z,"west",depth+1);case 1:g.child(b.Max[0]+1,b.Min[1],z,"east",depth+1) } }
			}
		}
		return
	}
	if p.kind == "crossing" {
		switch p.facing {
		case "north":
			g.child(b.Min[0]+1,b.Min[1],b.Min[2]-1,"north",depth+1);g.child(b.Min[0]-1,b.Min[1],b.Min[2]+1,"west",depth+1);g.child(b.Max[0]+1,b.Min[1],b.Min[2]+1,"east",depth+1)
		case "south":
			g.child(b.Min[0]+1,b.Min[1],b.Max[2]+1,"south",depth+1);g.child(b.Min[0]-1,b.Min[1],b.Min[2]+1,"west",depth+1);g.child(b.Max[0]+1,b.Min[1],b.Min[2]+1,"east",depth+1)
		case "west":
			g.child(b.Min[0]+1,b.Min[1],b.Min[2]-1,"north",depth+1);g.child(b.Min[0]+1,b.Min[1],b.Max[2]+1,"south",depth+1);g.child(b.Min[0]-1,b.Min[1],b.Min[2]+1,"west",depth+1)
		case "east":
			g.child(b.Min[0]+1,b.Min[1],b.Min[2]-1,"north",depth+1);g.child(b.Min[0]+1,b.Min[1],b.Max[2]+1,"south",depth+1);g.child(b.Max[0]+1,b.Min[1],b.Min[2]+1,"east",depth+1)
		}
		if b.Max[1]-b.Min[1]>3 {
			if g.r.NextBoolean(){g.child(b.Min[0]+1,b.Min[1]+4,b.Min[2]-1,"north",depth+1)}
			if g.r.NextBoolean(){g.child(b.Min[0]-1,b.Min[1]+4,b.Min[2]+1,"west",depth+1)}
			if g.r.NextBoolean(){g.child(b.Max[0]+1,b.Min[1]+4,b.Min[2]+1,"east",depth+1)}
			if g.r.NextBoolean(){g.child(b.Min[0]+1,b.Min[1]+4,b.Max[2]+1,"south",depth+1)}
		}
		return
	}
	if p.kind == "stairs" {
		x,y,z:=b.Min[0],b.Min[1],b.Min[2]
		switch p.facing {case "north":z--;case "south":z=b.Max[2]+1;case "west":x--;case "east":x=b.Max[0]+1}
		g.child(x,y,z,p.facing,depth+1)
	}
}
func mbox(x0,y0,z0,x1,y1,z1 int32)javastructure.Box{return javastructure.Box{Min:[3]int32{x0,y0,z0},Max:[3]int32{x1,y1,z1}}}
func abs32(v int32)int32{if v<0{return -v};return v};func max32(a,b int32)int32{if a>b{return a};return b}
func min32(a,b int32)int32{if a<b{return a};return b}

func buildModernMineshaft(in modernStructureBuildInput)(modernStructureStart,bool,error){
	if in.random==nil{return modernStructureStart{},false,fmt.Errorf("mineshaft requires structure random")}
	in.random.NextDouble() // Java MineshaftStructure consumes this before room generation.
	wood,_:=in.config["mineshaft_type"].(string);if wood!="mesa"{wood="normal"}
	x,z:=in.chunkX*16+2,in.chunkZ*16+2
	g:=mineGraph{r:in.random,wood:wood,origin:[3]int32{x,50,z}}
	room:=minePiece{kind:"room",wood:wood,seed:int64(uint64(x)*0x9e3779b97f4a7c15^uint64(z)),box:mbox(x,50,z,x+7+int32(in.random.NextInt(6)),54+int32(in.random.NextInt(6)),z+7+int32(in.random.NextInt(6)))}
	g.add(room);g.branch(room,0)
	// Java moves normal shafts below sea level; mesa shafts float between sea level and raw surface.
	center:=emptyStructureBox();for _,p:=range g.pieces{center=unionStructureBox(center,p.box)}
	shift:=int32(0);if wood=="mesa" {cy:=(center.Min[1]+center.Max[1])/2;surface:=in.source.BaseHeight((center.Min[0]+center.Max[0])/2,(center.Min[2]+center.Max[2])/2,false);target:=int32(63);if surface>63{target=63+in.random.NextInt(surface-63+1)};shift=target-cy} else {span:=center.Max[1]-center.Min[1]+1;target:=span-63;if target<53{target+=in.random.NextInt(53-target)};shift=target-center.Max[1]}
	for i:=range g.pieces {g.pieces[i].box.Min[1]+=shift;g.pieces[i].box.Max[1]+=shift};center=emptyStructureBox();pieces:=make([]modernStructurePiece,len(g.pieces));rigid:=make([]javastructure.RigidPiece,len(g.pieces));for i,p:=range g.pieces {center=unionStructureBox(center,p.box);pieces[i]=p;rigid[i]=javastructure.RigidPiece{Bounds:p.box,Adjustment:javastructure.TerrainNone}}
	return modernStructureStart{name:in.name,pieces:pieces,rigid:rigid,box:center,terrain:javastructure.TerrainNone},true,nil
}
