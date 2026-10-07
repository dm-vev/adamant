package overworld

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/world"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world/generator/advanced/internal/javastructure"
)

func init() { registerModernStructureBuilder("stronghold", buildModernStronghold) }

type strongholdPiece struct { box javastructure.Box; kind,dir,door string; depth int; leftLow,leftHigh,rightLow,rightHigh bool; seed int64; variant int }
func(p strongholdPiece)Bounds()javastructure.Box{return p.box}
func(p strongholdPiece)Place(clip javastructure.Bounds,env javastructure.ProcessorEnvironment)(javastructure.Placement,error){
	if env.Random==nil{return javastructure.Placement{},fmt.Errorf("stronghold placement requires shared Java random")}
	out:=javastructure.Placement{}
	put:=func(x,y,z int32,n string,props map[string]any){if x<clip.Min[0]||x>clip.Max[0]||y<clip.Min[1]||y>clip.Max[1]||z<clip.Min[2]||z>clip.Max[2]{return};out.Blocks=append(out.Blocks,javastructure.Block{Pos:[3]int32{x,y,z},State:world.BlockState{Name:"minecraft:"+n,Properties:props}})}
	b:=p.box;stateRandom:=env.Random;height:=b.Max[1]-b.Min[1]+1;width:=b.Max[0]-b.Min[0]+1;length:=b.Max[2]-b.Min[2]+1;if p.dir=="east"||p.dir=="west"{width,length=length,width}
	if p.kind!="filler"{
	for y:=int32(0);y<height;y++{for x:=int32(0);x<width;x++{for z:=int32(0);z<length;z++{pos:=strongholdLocal(b,p.dir,x,y,z);wx,wy,wz:=pos[0],pos[1],pos[2];if p.kind!="portal"&&mineAir(env.BaseBlock(wx,wy,wz)){continue};edge:=x==0||x==width-1||y==0||y==height-1||z==0||z==length-1;if edge{roll:=stateRandom.NextFloat();block:="stone_bricks";if roll<0.2{block="cracked_stone_bricks"}else if roll<0.5{block="mossy_stone_bricks"}else if roll<0.55{block="infested_stone_bricks"};put(wx,wy,wz,block,nil)}else{put(wx,wy,wz,"cave_air",nil)}}}}
	}
	if p.kind=="portal"{portalDoor:=p;portalDoor.door="grates";strongholdDoorPlacement(portalDoor,clip,&out)}
	// Exact functional furnishing is associated with each room class rather than a shared box-only template.
	switch p.kind {
	case "filler":
		for z:=int32(0);z<int32(p.variant);z++{for x:=int32(0);x<=4;x++{strongholdPutLocal(p,clip,&out,x,0,z,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,x,4,z,"stone_bricks",nil)};for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,0,y,z,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,4,y,z,"stone_bricks",nil);for x:=int32(1);x<=3;x++{strongholdPutLocal(p,clip,&out,x,y,z,"cave_air",nil)}}}
	case "start","stairs":
		strongholdPutLocal(p,clip,&out,2,6,1,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,5,1,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,6,1,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,1,5,2,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,4,3,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,5,3,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,2,4,3,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,3,3,3,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,3,4,3,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,3,3,2,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,3,2,1,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,3,3,1,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,2,2,1,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,1,1,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,2,1,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,1,1,2,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,1,1,3,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false})
	case "straight_stairs":
		for i:=int32(0);i<6;i++{for x:=int32(1);x<=3;x++{strongholdPutLocal(p,clip,&out,x,6-i,1+i,"cobblestone_stairs",map[string]any{"facing":strongholdDirection(p.dir,"south"),"half":"bottom","shape":"straight","waterlogged":false});if i<5{strongholdPutLocal(p,clip,&out,x,5-i,1+i,"stone_bricks",nil)}}}
	case "straight":
		for _,q:=range []struct{x,y,z int32;facing string}{{1,2,1,"east"},{3,2,1,"west"},{1,2,5,"east"},{3,2,5,"west"}}{if env.Random.NextFloat()<0.1{strongholdPutLocal(p,clip,&out,q.x,q.y,q.z,"wall_torch",map[string]any{"facing":strongholdDirection(p.dir,q.facing),"lit":true})}}
		if p.leftLow{for y:=int32(1);y<=3;y++{for z:=int32(2);z<=4;z++{strongholdPutLocal(p,clip,&out,0,y,z,"cave_air",nil)}}}
		if p.rightLow{for y:=int32(1);y<=3;y++{for z:=int32(2);z<=4;z++{strongholdPutLocal(p,clip,&out,4,y,z,"cave_air",nil)}}}
	case "turn_left","turn_right":
		cutX:=int32(0);if (p.kind=="turn_left"&&p.dir!="north"&&p.dir!="east")||(p.kind=="turn_right"&&(p.dir=="north"||p.dir=="east")){cutX=4};for y:=int32(1);y<=3;y++{for z:=int32(1);z<=3;z++{strongholdPutLocal(p,clip,&out,cutX,y,z,"cave_air",nil)}}
	case "prison":
		for _,z:=range []int32{1,3,7,9}{strongholdSelectedBox(p,clip,&out,env.Random,4,1,z,4,3,z)}
		for y:=int32(1);y<=3;y++{strongholdPutBars(p,clip,&out,4,y,4,true,true,false,false);strongholdPutBars(p,clip,&out,4,y,5,true,true,true,false);strongholdPutBars(p,clip,&out,4,y,6,true,true,false,false);for _,x:=range []int32{5,6,7}{strongholdPutBars(p,clip,&out,x,y,5,false,false,true,true)}}
		strongholdPutBars(p,clip,&out,4,3,2,true,true,false,false);strongholdPutBars(p,clip,&out,4,3,8,true,true,false,false)
		for _,z:=range []int32{2,8}{strongholdPutLocal(p,clip,&out,4,1,z,"iron_door",map[string]any{"facing":strongholdDirection(p.dir,"west"),"half":"lower","hinge":"left","open":false,"powered":false});strongholdPutLocal(p,clip,&out,4,2,z,"iron_door",map[string]any{"facing":strongholdDirection(p.dir,"west"),"half":"upper","hinge":"left","open":false,"powered":false})}
	case "library":
		height:=int32(11);if b.Max[1]-b.Min[1]+1<=6{height=6}
		for y:=int32(1);y<=4;y++{for x:=int32(2);x<=11;x++{for z:=int32(1);z<=13;z++{if env.Random.NextFloat()<0.07{strongholdPutLocal(p,clip,&out,x,y,z,"cobweb",nil)}}}}
		for d:=int32(1);d<=13;d++{
			name:="bookshelf";if (d-1)%4==0{name="oak_planks"}
			for y:=int32(1);y<=4;y++{strongholdPutLocal(p,clip,&out,1,y,d,name,nil);strongholdPutLocal(p,clip,&out,12,y,d,name,nil)}
			if name=="oak_planks"{strongholdPutLocal(p,clip,&out,2,3,d,"wall_torch",map[string]any{"facing":strongholdDirection(p.dir,"east"),"lit":true});strongholdPutLocal(p,clip,&out,11,3,d,"wall_torch",map[string]any{"facing":strongholdDirection(p.dir,"west"),"lit":true})}
			if height==11{for y:=int32(6);y<=9;y++{strongholdPutLocal(p,clip,&out,1,y,d,name,nil);strongholdPutLocal(p,clip,&out,12,y,d,name,nil)}}
		}
		for d:=int32(3);d<12;d+=2{for _,span:=range [][2]int32{{3,4},{6,7},{9,10}}{for x:=span[0];x<=span[1];x++{for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,x,y,d,"bookshelf",nil)}}}}
		if height==11{
			for _,span:=range [][2]int32{{1,3},{10,12}}{for x:=span[0];x<=span[1];x++{for z:=int32(1);z<=13;z++{strongholdPutLocal(p,clip,&out,x,5,z,"oak_planks",nil)}}}
			for x:=int32(4);x<=9;x++{for z:=int32(1);z<=2;z++{strongholdPutLocal(p,clip,&out,x,5,z,"oak_planks",nil)};for z:=int32(12);z<=13;z++{strongholdPutLocal(p,clip,&out,x,5,z,"oak_planks",nil)}}
			for _,q:=range [][3]int32{{9,5,11},{8,5,11},{9,5,10}}{strongholdPutLocal(p,clip,&out,q[0],q[1],q[2],"oak_planks",nil)}
			for y:=int32(1);y<=7;y++{strongholdPutLocal(p,clip,&out,10,y,13,"ladder",map[string]any{"facing":strongholdDirection(p.dir,"south"),"waterlogged":false})}
			for z:=int32(3);z<=11;z++{strongholdPutFenceLocal(p,clip,&out,3,6,z,true,true,false,false)}
			for z:=int32(3);z<=9;z++{strongholdPutFenceLocal(p,clip,&out,10,6,z,true,true,false,false)}
			for x:=int32(4);x<=9;x++{strongholdPutFenceLocal(p,clip,&out,x,6,2,false,false,true,true)}
			for x:=int32(4);x<=7;x++{strongholdPutFenceLocal(p,clip,&out,x,6,12,false,false,true,true)}
			strongholdPutFenceLocal(p,clip,&out,3,6,2,true,false,true,false);strongholdPutFenceLocal(p,clip,&out,3,6,12,false,true,true,false);strongholdPutFenceLocal(p,clip,&out,10,6,2,true,false,false,true)
			for i:=int32(0);i<=2;i++{strongholdPutFenceLocal(p,clip,&out,8+i,6,12-i,false,true,false,true);if i!=2{strongholdPutFenceLocal(p,clip,&out,8+i,6,11-i,true,false,true,false)}}
			strongholdPutFenceLocal(p,clip,&out,6,9,7,false,false,true,false);strongholdPutFenceLocal(p,clip,&out,7,9,7,false,false,false,true);strongholdPutFenceLocal(p,clip,&out,6,8,7,false,false,true,false);strongholdPutFenceLocal(p,clip,&out,7,8,7,false,false,false,true)
			strongholdPutFenceLocal(p,clip,&out,6,7,7,true,true,true,true);strongholdPutFenceLocal(p,clip,&out,7,7,7,true,true,true,true);strongholdPutFenceLocal(p,clip,&out,5,7,7,false,false,true,false);strongholdPutFenceLocal(p,clip,&out,8,7,7,false,false,false,true)
			strongholdPutFenceLocal(p,clip,&out,6,7,6,true,false,true,false);strongholdPutFenceLocal(p,clip,&out,6,7,8,false,true,true,false);strongholdPutFenceLocal(p,clip,&out,7,7,6,true,false,false,true);strongholdPutFenceLocal(p,clip,&out,7,7,8,false,true,false,true)
			strongholdPutLocal(p,clip,&out,12,9,1,"cave_air",nil)
		}
		strongholdPutLootLocal(p,clip,env,&out,3,3,5,"minecraft:chests/stronghold_library",env.Random)
		if height==11{strongholdPutLootLocal(p,clip,env,&out,12,8,1,"minecraft:chests/stronghold_library",env.Random)}
	case "portal":
		strongholdSelectedBox(p,clip,&out,env.Random,1,6,1,1,6,14);strongholdSelectedBox(p,clip,&out,env.Random,9,6,1,9,6,14);strongholdSelectedBox(p,clip,&out,env.Random,2,6,1,8,6,2);strongholdSelectedBox(p,clip,&out,env.Random,2,6,14,8,6,14)
		strongholdSelectedBox(p,clip,&out,env.Random,1,1,1,2,1,4);strongholdSelectedBox(p,clip,&out,env.Random,8,1,1,9,1,4);strongholdSelectedBox(p,clip,&out,env.Random,3,1,8,7,1,12);strongholdSelectedBox(p,clip,&out,env.Random,4,1,9,6,1,11)
		for z:=int32(1);z<=3;z++{strongholdPutLocal(p,clip,&out,1,1,z,"lava",nil);strongholdPutLocal(p,clip,&out,9,1,z,"lava",nil)}
		for z:=int32(9);z<=11;z++{for x:=int32(4);x<=6;x++{strongholdPutLocal(p,clip,&out,x,1,z,"lava",nil)}}
		for z:=int32(3);z<14;z+=2{strongholdPutBars(p,clip,&out,0,3,z,true,true,false,false);strongholdPutBars(p,clip,&out,0,4,z,true,true,false,false);strongholdPutBars(p,clip,&out,10,3,z,true,true,false,false);strongholdPutBars(p,clip,&out,10,4,z,true,true,false,false)}
		for x:=int32(2);x<9;x+=2{strongholdPutBars(p,clip,&out,x,3,15,false,false,true,true);strongholdPutBars(p,clip,&out,x,4,15,false,false,true,true)}
		strongholdSelectedBox(p,clip,&out,env.Random,4,1,5,6,1,7);strongholdSelectedBox(p,clip,&out,env.Random,4,2,6,6,2,7);strongholdSelectedBox(p,clip,&out,env.Random,4,3,7,6,3,7)
		for x:=int32(4);x<=6;x++{for _,q:=range [][3]int32{{x,1,4},{x,2,5},{x,3,6}}{strongholdPutLocal(p,clip,&out,q[0],q[1],q[2],"stone_brick_stairs",map[string]any{"facing":strongholdDirection(p.dir,"north"),"half":"bottom","shape":"straight","waterlogged":false})}}
		for y:=int32(1);y<=2;y++{strongholdPutLocal(p,clip,&out,5,y,0,"cave_air",nil)}
		strongholdPutBars(p,clip,&out,4,1,0,false,false,false,true);strongholdPutBars(p,clip,&out,4,2,0,false,false,false,true);strongholdPutBars(p,clip,&out,4,3,0,false,false,true,true);strongholdPutBars(p,clip,&out,5,3,0,false,false,true,true);strongholdPutBars(p,clip,&out,6,3,0,false,false,true,true);strongholdPutBars(p,clip,&out,6,1,0,false,false,true,false);strongholdPutBars(p,clip,&out,6,2,0,false,false,true,false)
		allEyes:=true
		coords:=[12][3]int32{{4,3,8},{5,3,8},{6,3,8},{4,3,12},{5,3,12},{6,3,12},{3,3,9},{3,3,10},{3,3,11},{7,3,9},{7,3,10},{7,3,11}}
		facings:=[12]string{"north","north","north","south","south","south","east","east","east","west","west","west"}
		eyes:=[12]bool{}
		for i:=range 12{eyes[i]=env.Random.NextFloat()>0.9;allEyes=allEyes&&eyes[i]}
		for i,coord:=range coords{strongholdPutLocal(p,clip,&out,coord[0],coord[1],coord[2],"end_portal_frame",map[string]any{"facing":strongholdDirection(p.dir,facings[i]),"eye":eyes[i]})}
		if allEyes{for x:=int32(4);x<=6;x++{for z:=int32(9);z<=11;z++{strongholdPutLocal(p,clip,&out,x,3,z,"end_portal",nil)}}}
		spawner:=strongholdLocal(b,p.dir,5,3,6);put(spawner[0],spawner[1],spawner[2],"spawner",nil)
		for i:=range out.Blocks{if out.Blocks[i].Pos==spawner{out.Blocks[i].NBT=map[string]any{"id":"minecraft:mob_spawner","SpawnData":map[string]any{"entity":map[string]any{"id":"minecraft:silverfish"}}}}}
	case "chest":
		for x:=int32(1);x<=3;x++{strongholdPutLocal(p,clip,&out,x,1,6,"cave_air",nil)}
		for z:=int32(2);z<=4;z++{strongholdPutLocal(p,clip,&out,3,1,z,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,2,1,z,"stone_brick_slab",map[string]any{"type":"bottom","waterlogged":false})}
		strongholdPutLocal(p,clip,&out,3,1,1,"stone_brick_slab",map[string]any{"type":"bottom","waterlogged":false})
		strongholdPutLocal(p,clip,&out,3,2,2,"stone_brick_slab",map[string]any{"type":"bottom","waterlogged":false})
		strongholdPutLocal(p,clip,&out,3,2,4,"stone_brick_slab",map[string]any{"type":"bottom","waterlogged":false})
		strongholdPutLootLocal(p,clip,env,&out,3,2,3,"minecraft:chests/stronghold_corridor",env.Random)
	case "crossing":
		for y:=int32(1);y<=3;y++{for x:=int32(4);x<=6;x++{strongholdPutLocal(p,clip,&out,x,y,10,"cave_air",nil)};for z:=int32(4);z<=6;z++{strongholdPutLocal(p,clip,&out,0,y,z,"cave_air",nil);strongholdPutLocal(p,clip,&out,10,y,z,"cave_air",nil)}}
		switch p.variant{
		case 0:
			for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,5,y,5,"stone_bricks",nil)}
			for _,q:=range []struct{x,y,z int32;facing string}{{4,3,5,"west"},{6,3,5,"east"},{5,3,4,"south"},{5,3,6,"north"}}{strongholdPutLocal(p,clip,&out,q.x,q.y,q.z,"wall_torch",map[string]any{"facing":strongholdDirection(p.dir,q.facing),"lit":true})}
			for _,q:=range [][3]int32{{4,1,4},{4,1,5},{4,1,6},{6,1,4},{6,1,5},{6,1,6},{5,1,4},{5,1,6}}{strongholdPutLocal(p,clip,&out,q[0],q[1],q[2],"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false})}
		case 1:
			for z:=int32(3);z<=7;z++{strongholdPutLocal(p,clip,&out,3,1,z,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,7,1,z,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,z,1,3,"stone_bricks",nil);strongholdPutLocal(p,clip,&out,z,1,7,"stone_bricks",nil)}
			for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,5,y,5,"stone_bricks",nil)};strongholdPutLocal(p,clip,&out,5,4,5,"water",nil)
		case 2:
			for z:=int32(1);z<=9;z++{strongholdPutLocal(p,clip,&out,1,3,z,"cobblestone",nil);strongholdPutLocal(p,clip,&out,9,3,z,"cobblestone",nil)}
			for x:=int32(1);x<=9;x++{strongholdPutLocal(p,clip,&out,x,3,1,"cobblestone",nil);strongholdPutLocal(p,clip,&out,x,3,9,"cobblestone",nil)}
			for _,q:=range [][3]int32{{5,1,4},{5,1,6},{5,3,4},{5,3,6},{4,1,5},{6,1,5},{4,3,5},{6,3,5}}{strongholdPutLocal(p,clip,&out,q[0],q[1],q[2],"cobblestone",nil)}
			for y:=int32(1);y<=3;y++{for _,x:=range []int32{4,6}{for _,z:=range []int32{4,6}{strongholdPutLocal(p,clip,&out,x,y,z,"cobblestone",nil)}}}
			strongholdPutLocal(p,clip,&out,5,3,5,"wall_torch",map[string]any{"facing":strongholdDirection(p.dir,"north"),"lit":true})
			for z:=int32(2);z<=8;z++{for _,x:=range []int32{2,3,7,8}{strongholdPutLocal(p,clip,&out,x,3,z,"oak_planks",nil)};if z<=3||z>=7{for x:=int32(4);x<=6;x++{strongholdPutLocal(p,clip,&out,x,3,z,"oak_planks",nil)}}}
			for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,9,y,3,"ladder",map[string]any{"facing":strongholdDirection(p.dir,"west"),"waterlogged":false})}
			strongholdPutLootLocal(p,clip,env,&out,3,4,8,"minecraft:chests/stronghold_crossing",env.Random)
		}
	case "five":
		if p.leftLow{for y:=int32(3);y<=5;y++{for z:=int32(1);z<=3;z++{strongholdPutLocal(p,clip,&out,0,y,z,"cave_air",nil)}}}
		if p.rightLow{for y:=int32(3);y<=5;y++{for z:=int32(1);z<=3;z++{strongholdPutLocal(p,clip,&out,9,y,z,"cave_air",nil)}}}
		if p.leftHigh{for y:=int32(5);y<=7;y++{for z:=int32(7);z<=9;z++{strongholdPutLocal(p,clip,&out,0,y,z,"cave_air",nil)}}}
		if p.rightHigh{for y:=int32(5);y<=7;y++{for z:=int32(7);z<=9;z++{strongholdPutLocal(p,clip,&out,9,y,z,"cave_air",nil)}}}
		strongholdSelectedBox(p,clip,&out,env.Random,1,2,1,8,2,6);strongholdSelectedBox(p,clip,&out,env.Random,4,1,5,4,4,9);strongholdSelectedBox(p,clip,&out,env.Random,8,1,5,8,4,9);strongholdSelectedBox(p,clip,&out,env.Random,1,4,7,3,4,9);strongholdSelectedBox(p,clip,&out,env.Random,1,3,5,3,3,6);strongholdSelectedBox(p,clip,&out,env.Random,5,1,7,7,1,8)
		for x:=int32(1);x<=3;x++{strongholdPutLocal(p,clip,&out,x,3,4,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,x,4,6,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false})}
		for x:=int32(5);x<=7;x++{strongholdPutLocal(p,clip,&out,x,1,9,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false});strongholdPutLocal(p,clip,&out,x,2,7,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false})}
		for _,x:=range []int32{4,8}{for z:=int32(7);z<=9;z++{strongholdPutLocal(p,clip,&out,x,5,z,"smooth_stone_slab",map[string]any{"type":"bottom","waterlogged":false})}}
		for z:=int32(7);z<=9;z++{for x:=int32(5);x<=7;x++{strongholdPutLocal(p,clip,&out,x,5,z,"smooth_stone_slab",map[string]any{"type":"double","waterlogged":false})}}
		strongholdPutLocal(p,clip,&out,6,5,6,"wall_torch",map[string]any{"facing":strongholdDirection(p.dir,"south"),"lit":true})
	}
	if p.kind!="portal"&&p.kind!="filler"{strongholdDoorPlacement(p,clip,&out)}
	exitZ:=int32(-1)
	switch p.kind{case "start","stairs":exitZ=4;case "straight_stairs":exitZ=7;case "straight","chest":exitZ=6;case "prison":exitZ=10}
	if p.kind=="five"{for x:=int32(5);x<=7;x++{for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,x,y,10,"cave_air",nil)}}}else if exitZ>=0{for x:=int32(1);x<=3;x++{for y:=int32(1);y<=3;y++{strongholdPutLocal(p,clip,&out,x,y,exitZ,"cave_air",nil)}}}
	return out,nil
}

func strongholdLocal(b javastructure.Box,dir string,x,y,z int32)[3]int32{
	switch dir{case "north":return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Max[2]-z};case "south":return [3]int32{b.Min[0]+x,b.Min[1]+y,b.Min[2]+z};case "west":return [3]int32{b.Max[0]-z,b.Min[1]+y,b.Min[2]+x};default:return [3]int32{b.Min[0]+z,b.Min[1]+y,b.Min[2]+x}}
}

func strongholdDirection(pieceFacing,local string)string{
	switch pieceFacing{
	case "south":return local
	case "north":switch local{case "north":return "south";case "south":return "north";case "east":return "west";default:return "east"}
	case "east":switch local{case "north":return "west";case "south":return "east";case "east":return "north";default:return "south"}
	default:switch local{case "north":return "east";case "south":return "west";case "east":return "south";default:return "north"}
	}
}
func strongholdPutLocal(p strongholdPiece,clip javastructure.Bounds,out *javastructure.Placement,x,y,z int32,name string,props map[string]any){
	pos:=strongholdLocal(p.box,p.dir,x,y,z)
	if pos[0]<clip.Min[0]||pos[0]>clip.Max[0]||pos[1]<clip.Min[1]||pos[1]>clip.Max[1]||pos[2]<clip.Min[2]||pos[2]>clip.Max[2]{return}
	out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:world.BlockState{Name:"minecraft:"+name,Properties:props}})
}
func strongholdPutBars(p strongholdPiece,clip javastructure.Bounds,out *javastructure.Placement,x,y,z int32,north,south,east,west bool){
	local:=map[string]any{"north":north,"south":south,"east":east,"west":west,"waterlogged":false};worldProps:=map[string]any{"waterlogged":false}
	for _,dir:=range []string{"north","south","east","west"}{worldProps[strongholdDirection(p.dir,dir)]=local[dir]}
	strongholdPutLocal(p,clip,out,x,y,z,"iron_bars",worldProps)
}
func strongholdPutFenceLocal(p strongholdPiece,clip javastructure.Bounds,out *javastructure.Placement,x,y,z int32,north,south,east,west bool){
	local:=map[string]any{"north":north,"south":south,"east":east,"west":west,"waterlogged":false}
	worldProps:=map[string]any{"waterlogged":false}
	for _,dir:=range []string{"north","south","east","west"}{worldProps[strongholdDirection(p.dir,dir)]=local[dir]}
	strongholdPutLocal(p,clip,out,x,y,z,"oak_fence",worldProps)
}
func strongholdSelectedBox(p strongholdPiece,clip javastructure.Bounds,out *javastructure.Placement,random *java26.Random,x0,y0,z0,x1,y1,z1 int32){
	for y:=y0;y<=y1;y++{for x:=x0;x<=x1;x++{for z:=z0;z<=z1;z++{edge:=x==x0||x==x1||y==y0||y==y1||z==z0||z==z1;if !edge{strongholdPutLocal(p,clip,out,x,y,z,"cave_air",nil);continue};roll:=random.NextFloat();name:="stone_bricks";if roll<0.2{name="cracked_stone_bricks"}else if roll<0.5{name="mossy_stone_bricks"}else if roll<0.55{name="infested_stone_bricks"};strongholdPutLocal(p,clip,out,x,y,z,name,nil)}}}
}
func strongholdPutLootLocal(p strongholdPiece,clip javastructure.Bounds,env javastructure.ProcessorEnvironment,out *javastructure.Placement,x,y,z int32,loot string,random *java26.Random){
	pos:=strongholdLocal(p.box,p.dir,x,y,z)
	if pos[0]<clip.Min[0]||pos[0]>clip.Max[0]||pos[1]<clip.Min[1]||pos[1]>clip.Max[1]||pos[2]<clip.Min[2]||pos[2]>clip.Max[2]{return}
	if name,_:=env.BaseBlock(pos[0],pos[1],pos[2]).EncodeBlock();name=="minecraft:chest"{return}
	facing:="north"
	directions:=[4]struct{dx,dz int32;name,opposite string}{{0,-1,"north","south"},{1,0,"east","west"},{0,1,"south","north"},{-1,0,"west","east"}}
	solidDirection:=""
	for _,direction:=range directions{neighbor:=[3]int32{pos[0]+direction.dx,pos[1],pos[2]+direction.dz};name:=""
		for i:=range out.Blocks{if out.Blocks[i].Pos==neighbor{name=out.Blocks[i].State.Name;break}}
		if name==""{name,_=env.BaseBlock(neighbor[0],neighbor[1],neighbor[2]).EncodeBlock()}
		if name=="minecraft:chest"{solidDirection="";break}
		if name!="minecraft:air"&&name!="minecraft:cave_air"&&name!="minecraft:void_air"&&name!="minecraft:water"&&name!="minecraft:lava"{if solidDirection!=""{solidDirection="multiple";break};solidDirection=direction.opposite}
	}
	if solidDirection!=""&&solidDirection!="multiple"{facing=solidDirection}
	out.Blocks=append(out.Blocks,javastructure.Block{Pos:pos,State:world.BlockState{Name:"minecraft:chest",Properties:map[string]any{"facing":facing,"type":"single","waterlogged":false}},NBT:map[string]any{"id":"minecraft:chest","LootTable":loot,"LootTableSeed":random.NextLong()}})
}
func strongholdDoorPlacement(p strongholdPiece,clip javastructure.Bounds,out *javastructure.Placement){
	footX:=int32(1);footY:=int32(1);if p.kind=="crossing"||p.kind=="library"||p.kind=="five"||p.kind=="portal"{footX=4};if p.kind=="five"{footY=3};if p.kind=="start"||p.kind=="stairs"||p.kind=="straight_stairs"{footY=7}
	put:=func(x,y,z int32,name string,props map[string]any){if name=="iron_bars"{rotated:=map[string]any{"waterlogged":false};for _,dir:=range []string{"north","south","east","west"}{rotated[strongholdDirection(p.dir,dir)]=props[dir]};props=rotated};strongholdPutLocal(p,clip,out,x+footX-1,y+footY-1,z,name,props)}
	if p.door=="opening"||p.door==""{for x:=int32(1);x<=3;x++{for y:=int32(1);y<=3;y++{put(x,y,0,"cave_air",nil)}};return}
	for _,x:=range []int32{1,3}{for y:=int32(1);y<=3;y++{put(x,y,0,"stone_bricks",nil)}}
	for x:=int32(1);x<=3;x++{put(x,3,0,"stone_bricks",nil)}
	switch p.door{
	case "wood":
		put(2,1,0,"oak_door",map[string]any{"facing":strongholdDirection(p.dir,"north"),"half":"lower","hinge":"left","open":false,"powered":false})
		put(2,2,0,"oak_door",map[string]any{"facing":strongholdDirection(p.dir,"north"),"half":"upper","hinge":"left","open":false,"powered":false})
	case "grates":
		put(2,1,0,"cave_air",nil);put(2,2,0,"cave_air",nil)
		put(1,1,0,"iron_bars",map[string]any{"east":false,"north":false,"south":false,"west":true,"waterlogged":false})
		put(1,2,0,"iron_bars",map[string]any{"east":false,"north":false,"south":false,"west":true,"waterlogged":false})
		put(1,3,0,"iron_bars",map[string]any{"east":true,"north":false,"south":false,"west":true,"waterlogged":false})
		put(2,3,0,"iron_bars",map[string]any{"east":true,"north":false,"south":false,"west":true,"waterlogged":false})
		put(3,3,0,"iron_bars",map[string]any{"east":true,"north":false,"south":false,"west":true,"waterlogged":false})
		put(3,1,0,"iron_bars",map[string]any{"east":true,"north":false,"south":false,"west":false,"waterlogged":false})
		put(3,2,0,"iron_bars",map[string]any{"east":true,"north":false,"south":false,"west":false,"waterlogged":false})
	case "iron":
		put(2,1,0,"iron_door",map[string]any{"facing":strongholdDirection(p.dir,"north"),"half":"lower","hinge":"left","open":false,"powered":false})
		put(2,2,0,"iron_door",map[string]any{"facing":strongholdDirection(p.dir,"north"),"half":"upper","hinge":"left","open":false,"powered":false})
		put(3,2,1,"stone_button",map[string]any{"face":"wall","facing":strongholdDirection(p.dir,"north"),"powered":false})
		put(3,2,-1,"stone_button",map[string]any{"face":"wall","facing":strongholdDirection(p.dir,"south"),"powered":false})
	}
}

type strongholdDoor struct{x,y,z int32;dir string}
type strongholdGraph struct{r *java26.Random; pieces []strongholdPiece; boxes []javastructure.Box; pending []strongholdPiece; weights [11]int; removed [11]bool; previous int; portal bool; imposed bool; origin [3]int32}
var strongholdWeight=[11]int{40,5,20,20,10,5,5,5,5,10,20}
var strongholdMaximum=[11]int{0,5,0,0,6,5,5,4,4,2,1}
func(g *strongholdGraph)add(p strongholdPiece)bool{for _,b:=range g.boxes{if boxesOverlap(b,p.box){return false}};g.pieces=append(g.pieces,p);g.boxes=append(g.boxes,p.box);return true}
func(g *strongholdGraph)make(kind,dir string,x,y,z int32,depth int)(strongholdPiece,bool){
	w,h,l,offsetX,offsetY:=int32(5),int32(5),int32(5),int32(-1),int32(-1)
	switch kind {
	case "start","stairs":w,h,l,offsetY=5,11,5,-7
	case "straight","chest":l=7
	case "prison":w,h,l=9,5,11
	case "turn_left","turn_right":w,h,l=5,5,5
	case "crossing":w,h,l,offsetX=11,7,11,-4
	case "five":w,h,l,offsetX,offsetY=10,9,11,-4,-3
	case "straight_stairs":l, h, offsetY=8,11,-7
	case "library":w,h,l,offsetX=14,11,15,-4
	case "portal":w,h,l,offsetX=11,8,16,-4
	case "filler":w,h,l=5,5,4
	}
	boxFor:=func(height int32)javastructure.Box{switch dir{case "north":return mbox(x+offsetX,y+offsetY,z-l+1,x+offsetX+w-1,y+offsetY+height-1,z);case "south":return mbox(x+offsetX,y+offsetY,z,x+offsetX+w-1,y+offsetY+height-1,z+l-1);case "west":return mbox(x-l+1,y+offsetY,z+offsetX,x,y+offsetY+height-1,z+offsetX+w-1);default:return mbox(x,y+offsetY,z+offsetX,x+l-1,y+offsetY+height-1,z+offsetX+w-1)}}
	b:=boxFor(h);fillerSteps:=0
	if kind=="library"&&g.collides(b){h=6;b=boxFor(h)}
	if kind=="filler"{
		collision,found:=javastructure.Box{},false
		for _,other:=range g.boxes{if boxesOverlap(other,b){collision,found=other,true;break}}
		if !found||collision.Min[1]!=b.Min[1]{return strongholdPiece{},false}
		for length:=int32(2);length>=1;length--{l=length;candidate:=boxFor(h);if !boxesOverlap(collision,candidate){l=length+1;b=boxFor(h);fillerSteps=int(l);break}}
		if fillerSteps==0{return strongholdPiece{},false}
	}
	if (kind!="filler"&&g.collides(b))||b.Min[1]<=1{return strongholdPiece{},false}
	p:=strongholdPiece{box:b,kind:kind,dir:dir,depth:depth,seed:int64(uint64(x)*0x9e3779b97f4a7c15^uint64(y)*0xbf58476d1ce4e5b9^uint64(z)),variant:fillerSteps}
	if kind!="portal"&&kind!="filler"&&kind!="five"{door:=g.r.NextInt(5);p.door=[]string{"opening","opening","wood","grates","iron"}[door]}
	if kind=="straight"{p.leftLow=g.r.NextInt(2)==0;p.rightLow=g.r.NextInt(2)==0}
	if kind=="crossing"{p.variant=int(g.r.NextInt(5))}
	if kind=="five"{p.door=[]string{"opening","opening","wood","grates","iron"}[g.r.NextInt(5)];p.leftLow=g.r.NextBoolean();p.leftHigh=g.r.NextBoolean();p.rightLow=g.r.NextBoolean();p.rightHigh=g.r.NextInt(3)>0}
	return p,true
}
func(g *strongholdGraph)collides(box javastructure.Box)bool{for _,other:=range g.boxes{if boxesOverlap(other,box){return true}};return false}
func(g *strongholdGraph)door(p strongholdPiece,kind string)strongholdDoor{b:=p.box;switch p.dir{case "north":return strongholdDoor{(b.Min[0]+b.Max[0])/2,b.Min[1]+1,b.Min[2]-1,"north"};case "south":return strongholdDoor{(b.Min[0]+b.Max[0])/2,b.Min[1]+1,b.Max[2]+1,"south"};case "west":return strongholdDoor{b.Min[0]-1,b.Min[1]+1,(b.Min[2]+b.Max[2])/2,"west"};default:return strongholdDoor{b.Max[0]+1,b.Min[1]+1,(b.Min[2]+b.Max[2])/2,"east"}}
}
func(g *strongholdGraph)grow(p strongholdPiece){
	if p.depth>50{return}
	b:=p.box
	forward:=strongholdDoor{b.Min[0]+1,b.Min[1]+1,b.Max[2]+1,"south"}
	switch p.dir{case "north":forward.z=b.Min[2]-1;case "west":forward.x,forward.z=b.Min[0]-1,b.Min[2]+1;case "east":forward.x,forward.z=b.Max[0]+1,b.Min[2]+1}
	if p.kind=="start"||p.kind=="stairs"||p.kind=="straight_stairs"||p.kind=="straight"||p.kind=="prison"||p.kind=="chest"{g.attach(forward,p.depth)}
	if p.kind=="crossing"{forward.x=b.Min[0]+4;g.attach(forward,p.depth);g.side(p,p.depth,"left",1,4);g.side(p,p.depth,"right",1,4)}
	if p.kind=="straight"{if p.leftLow{g.side(p,p.depth,"left",2,1)};if p.rightLow{g.side(p,p.depth,"right",2,1)}}
	if p.kind=="turn_left"{side:="left";if p.dir=="north"||p.dir=="east"{side="right"};g.side(p,p.depth,side,1,1)}
	if p.kind=="turn_right"{side:="right";if p.dir=="north"||p.dir=="east"{side="left"};g.side(p,p.depth,side,1,1)}
	if p.kind=="five"{forward.x=b.Min[0]+1;g.attach(forward,p.depth);offA,offB:=int32(3),int32(5);if p.dir=="west"||p.dir=="north"{offA,offB=8-offA,8-offB};if p.leftLow{g.side(p,p.depth,"left",offA,1)};if p.leftHigh{g.side(p,p.depth,"left",offB,7)};if p.rightLow{g.side(p,p.depth,"right",offA,1)};if p.rightHigh{g.side(p,p.depth,"right",offB,7)}}
}
func(g *strongholdGraph)side(p strongholdPiece,depth int,side string,zoff,yoff int32){
	b:=p.box;d:=strongholdDoor{b.Min[0]-1,b.Min[1]+yoff,b.Min[2]+zoff,"west"}
	switch p.dir{
	case "south","north":
		if side=="right"{d.x,d.dir=b.Max[0]+1,"east"}
	case "west","east":
		d.x,d.z,d.dir=b.Min[0]+zoff,b.Min[2]-1,"north"
		if side=="right"{d.z,d.dir=b.Max[2]+1,"south"}
	}
	g.attach(d,depth)
}
func(g *strongholdGraph)attach(d strongholdDoor,parentDepth int){
	depth:=parentDepth+1
	if depth>50||abs32(d.x-g.origin[0])>112||abs32(d.z-g.origin[2])>112{return}
	if g.imposed{g.imposed=false;p,ok:=g.make("five",d.dir,d.x,d.y,d.z,depth);if ok&&g.add(p){g.pending=append(g.pending,p)};return}
	total,hasAny:=0,false
	for i,w:=range strongholdWeight{if g.removed[i]{continue};total+=w;if strongholdMaximum[i]>0&&g.weights[i]<strongholdMaximum[i]{hasAny=true}}
	if !hasAny{return}
	names:=[]string{"straight","prison","turn_left","turn_right","crossing","straight_stairs","stairs","five","chest","library","portal"}
	for range 5{
		sel:=g.r.NextInt(int32(total));idx:=0
		for i,w:=range strongholdWeight{if g.removed[i]{continue};sel-=int32(w);if sel<0{idx=i;break}}
		if idx==g.previous||idx==9&&depth<=4||idx==10&&depth<=5{continue}
		if strongholdMaximum[idx]>0&&g.weights[idx]>=strongholdMaximum[idx]{continue}
		p,ok:=g.make(names[idx],d.dir,d.x,d.y,d.z,depth);if !ok{continue}
		g.weights[idx]++;g.previous=idx;if idx==10{g.portal=true}
		if strongholdMaximum[idx]>0&&g.weights[idx]>=strongholdMaximum[idx]{g.removed[idx]=true}
		if g.add(p){g.pending=append(g.pending,p);return}
	}
	p,ok:=g.make("filler",d.dir,d.x,d.y,d.z,depth);if ok&&p.box.Min[1]>1&&g.add(p){g.pending=append(g.pending,p)}
}
func buildModernStronghold(in modernStructureBuildInput)(modernStructureStart,bool,error){
	if in.random==nil{return modernStructureStart{},false,fmt.Errorf("stronghold requires structure random")}
	x,z:=in.chunkX*16+2,in.chunkZ*16+2
	for attempt:=int64(0);;attempt++{
		r:=largeFeatureRandom(in.seed+attempt,in.chunkX,in.chunkZ)
		g:=strongholdGraph{r:&r,origin:[3]int32{x,64,z},previous:-1,imposed:true}
		dir:=[]string{"south","west","north","east"}[r.NextInt(4)]
		start,ok:=g.make("start",dir,x,64,z,0)
		if !ok{return modernStructureStart{},false,nil}
		g.add(start);g.grow(start)
		for len(g.pending)>0{at:=int(r.NextInt(int32(len(g.pending))));p:=g.pending[at];g.pending=append(g.pending[:at],g.pending[at+1:]...);g.grow(p)}
		if !g.portal{continue}
		box:=emptyStructureBox();for _,p:=range g.pieces{box=unionStructureBox(box,p.box)}
		span:=box.Max[1]-box.Min[1]+1;target:=span-63
		if target<53{target+=r.NextInt(53-target)}
		shift:=target-box.Max[1]
		pieces:=make([]modernStructurePiece,len(g.pieces));rigid:=make([]javastructure.RigidPiece,len(g.pieces))
		box=emptyStructureBox()
		for i,p:=range g.pieces{p.box.Min[1]+=shift;p.box.Max[1]+=shift;box=unionStructureBox(box,p.box);pieces[i]=p;rigid[i]=javastructure.RigidPiece{Bounds:p.box,Adjustment:javastructure.TerrainNone}}
		return modernStructureStart{name:in.name,pieces:pieces,rigid:rigid,box:box},true,nil
	}
}
