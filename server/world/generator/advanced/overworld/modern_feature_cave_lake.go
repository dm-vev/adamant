package overworld

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	java26 "github.com/df-mc/dragonfly/server/internal/java26"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

func placeModernLake(region *featureRegion, config map[string]any, origin featurePos, random *java26.Random) bool {
	if origin.y <= region.MinY()+4 { return false }
	origin=featurePos{origin.x-8,origin.y-4,origin.z-8}
	var grid [16*16*8]bool
	spots:=random.NextInt(4)+4
	for range spots {
		xRadius:=random.NextDouble()*6+3;yRadius:=random.NextDouble()*4+2;zRadius:=random.NextDouble()*6+3
		xCenter:=random.NextDouble()*(16-xRadius-2)+1+xRadius/2
		yCenter:=random.NextDouble()*(8-yRadius-4)+2+yRadius/2
		zCenter:=random.NextDouble()*(16-zRadius-2)+1+zRadius/2
		for x:=1;x<15;x++{for z:=1;z<15;z++{for y:=1;y<7;y++{
			dx:=(float64(x)-xCenter)/(xRadius/2);dy:=(float64(y)-yCenter)/(yRadius/2);dz:=(float64(z)-zCenter)/(zRadius/2)
			if dx*dx+dy*dy+dz*dz<1{grid[(x*16+z)*8+y]=true}
		}}}
	}
	fluidState,fluidActor,ok:=region.SampleNativeState(config["fluid"],origin,random);if !ok{panic("overworld: unresolved Java lake fluid provider")}
	airState:=featureRequireState(region,"minecraft:air")
	caveAirState:=featureRequireState(region,"minecraft:cave_air")
	fluidRID:=fluidState.Liquid;if fluidRID==airState.Block{fluidRID=fluidState.Block}
	fluidName,_,found:=region.c.BlockRegistry().RuntimeIDToState(fluidRID);if !found{panic("overworld: invalid Java lake fluid runtime ID")}
	fluidIsWater:=normalizeJavaResource(fluidName)=="water"
	canPlace,ok:=config["can_place_feature"].(map[string]any);if !ok{panic("overworld: malformed Java lake placement predicate")}
	canAirFluid,ok:=config["can_replace_with_air_or_fluid"].(map[string]any);if !ok{panic("overworld: malformed Java lake air/fluid predicate")}
	canBarrier,ok:=config["can_replace_with_barrier"].(map[string]any);if !ok{panic("overworld: malformed Java lake barrier predicate")}
	for x:=range 16{for z:=range 16{for y:=range 8{
		if grid[(x*16+z)*8+y]||!lakeGridEdge(&grid,x,y,z){continue}
		p:=featurePos{origin.x+int32(x),origin.y+int32(y),origin.z+int32(z)}
		stateNative:=region.nativeState(p)
		if y>=4&&featureStateHasFluid(region.c,stateNative){return false}
		if y<4&&!featureRegionSolid(region,p)&&stateNative!=fluidState{return false}
		if !featureRegionPredicate(region,p,canPlace){return false}
	}}}
	for x:=range 16{for z:=range 16{for y:=range 8{
		if !grid[(x*16+z)*8+y]{continue}
		p:=featurePos{origin.x+int32(x),origin.y+int32(y),origin.z+int32(z)}
		if !featureRegionPredicate(region,p,canAirFluid){continue}
		if y>=4{
			region.Set(p,caveAirState)
			if featureInsideTarget(region.target,p){region.result.ScheduledBlocks=append(region.result.ScheduledBlocks,chunk.ScheduledBlockUpdate{Pos:cube.Pos{int(p.x),int(p.y),int(p.z)},Block:caveAirState.Block,Tick:0})}
		}else{region.SetNativeBlock(p,fluidState,fluidActor)}
	}}}
	barrierProvider,ok:=config["barrier"].(map[string]any);if !ok{panic("overworld: malformed Java lake barrier provider")}
	barrierState,barrierActor,ok:=region.SampleNativeState(barrierProvider,origin,random);if !ok{panic("overworld: unresolved Java lake barrier provider")}
	barrierJava,err:=world.JavaState(region.c.BlockRegistry(), barrierState);if err!=nil{panic(err)}
	if normalizeJavaResource(barrierJava.Name)!="air"&&normalizeJavaResource(barrierJava.Name)!="cave_air"&&normalizeJavaResource(barrierJava.Name)!="void_air"{
		for x:=range 16{for z:=range 16{for y:=range 8{
			if grid[(x*16+z)*8+y]||!lakeGridEdge(&grid,x,y,z){continue}
			if y>=4&&random.NextInt(2)==0{continue}
			p:=featurePos{origin.x+int32(x),origin.y+int32(y),origin.z+int32(z)}
			if featureRegionSolid(region,p)&&featureRegionPredicate(region,p,canBarrier){region.SetNativeBlock(p,barrierState,barrierActor)}
		}}}
	}
	if fluidIsWater{
		for x:=range 16{for z:=range 16{p:=featurePos{origin.x+int32(x),origin.y+4,origin.z+int32(z)};biome:=normalizeJavaResource(region.f.source.JavaBiomeAt(p.x,p.y,p.z));if region.f.temperatures[biome]<0.15&&featureRegionPredicate(region,p,canAirFluid){ice:=featureRequireState(region,"minecraft:ice");region.Set(p,ice)}}}
	}
	return true
}

func lakeGridEdge(grid *[16*16*8]bool,x,y,z int)bool{
	return x<15&&grid[((x+1)*16+z)*8+y]||x>0&&grid[((x-1)*16+z)*8+y]||z<15&&grid[(x*16+z+1)*8+y]||z>0&&grid[(x*16+z-1)*8+y]||y<7&&grid[(x*16+z)*8+y+1]||y>0&&grid[(x*16+z)*8+y-1]
}

type featureRegionBlockSource struct{region *featureRegion}
func(s featureRegionBlockSource)Block(pos cube.Pos)world.Block{return s.region.Block(featurePos{int32(pos[0]),int32(pos[1]),int32(pos[2])})}
func featureRegionSolid(region *featureRegion,p featurePos)bool{
	block:=region.Block(p);pos:=cube.Pos{int(p.x),int(p.y),int(p.z)};source:=featureRegionBlockSource{region}
	for _,face:=range cube.Faces(){if !block.Model().FaceSolid(pos,face,source){return false}}
	return true
}
