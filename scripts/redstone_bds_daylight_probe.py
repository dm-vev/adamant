#!/usr/bin/env python3
"""Capture native daylight sampling over time, cover and dimension matrices."""

import json
from redstone_bds_timing_probe import arguments, capture


CASES = [{"time": time, "advance": advance, "phase": i & 1}
         for i, (time, advance) in enumerate((t, a) for t in (0, 1, 6000, 6001, 12000, 12500, 18000, 23000) for a in (False, True))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),dimensions=["overworld","nether","the_end"];
let ds=[],load=0,index=0,start=0,current=null,cells=[];
const cmd=(d,x)=>d.runCommand(x),get=(d,x,y,z)=>d.getBlock({x,y,z});
const h=system.runInterval(()=>{try{
 if(!ds.length){
  ds=dimensions.map(id=>world.getDimension(id));
  for(const d of ds)cmd(d,"tickingarea add -16 0 -16 32 100 32 audit true");
  cmd(ds[0],"gamerule randomtickspeed 0");cmd(ds[0],"gamerule domobspawning false");cmd(ds[0],"weather clear 100000");load=system.currentTick;return;
 }
 if(!current){
  if(!ds.every(d=>get(d,0,65,0)&&get(d,31,65,31))){if(system.currentTick-load>240)throw new Error("dimension load timeout");return;}
  const c=cases[index];if((system.currentTick&1)!==c.phase)return;
  cmd(ds[0],`gamerule dodaylightcycle ${c.advance}`);cmd(ds[0],`time set ${c.time}`);
  cells=[];
  for(let di=0;di<ds.length;di++){
   const d=ds[di];cmd(d,"fill 0 64 0 31 64 31 stone");cmd(d,"fill 0 65 0 31 80 31 air");
   let n=0;
   for(const inverted of [false,true])for(const cover of ["air","stone","glass","leaves"])for(const initial of [0,15]){
    const x=n%4*8+4,z=Math.floor(n/4)*8+4;n++;
    if(cover!=="air")cmd(d,`fill ${x-2} 68 ${z-2} ${x+2} 68 ${z+2} ${cover} ${cover==="leaves"?'["persistent_bit"=true]':""}`);
    cmd(d,`setblock ${x} 65 ${z} ${inverted?"daylight_detector_inverted":"daylight_detector"} ["redstone_signal"=${initial}]`);
    cmd(d,`setblock ${x+1} 65 ${z} redstone_wire`);
    cells.push({dimension:dimensions[di],x,z,inverted,cover,initial});
   }
  }
  start=system.currentTick;current={scenario:c,changes:[{tick:start,kind:"setup"}],cells,samples:[]};return;
 }
 const values=cells.map(c=>{
  const d=ds[dimensions.indexOf(c.dimension)],b=get(d,c.x,65,c.z),wire=get(d,c.x+1,65,c.z);
  if(!b||!wire)throw new Error("unloaded daylight sample");
  if(typeof d.getSkyLightLevel!=="function")throw new Error("stable API cannot expose raw sky light");
  return {source:{id:b.typeId,states:b.permutation.getAllStates()},wire:{id:wire.typeId,states:wire.permutation.getAllStates()},sky:d.getSkyLightLevel({x:c.x,y:65,z:c.z})};
 });
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),time:world.getTimeOfDay(),values});
 if(system.currentTick-start>=60){
  if(!current.samples.some(s=>s.values.some((v,i)=>cells[i].dimension==="overworld"&&!cells[i].inverted&&cells[i].cover==="air"&&v.source.states.redstone_signal>0))&&[0,1,6000,6001].includes(current.scenario.time))throw new Error("daylight positive control failed");
  emit(current);current=null;index++;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
