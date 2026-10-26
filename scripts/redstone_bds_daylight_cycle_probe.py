#!/usr/bin/env python3
"""Capture every eligible daylight sample through a full day in three weathers."""

import json
from redstone_bds_timing_probe import arguments, capture

CASES = [{"weather": weather, "start": start, "phase": i & 1}
         for i, (weather, start) in enumerate((w, s) for w in ("clear", "rain", "thunder") for s in range(0, 24000, 4000))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,index=0,current=null,cells=[],prepared=false,weather="",wait=0;
const cmd=x=>d.runCommand(x),get=(x,y,z)=>d.getBlock({x,y,z});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 32 100 32 audit true");load=system.currentTick;return;}
 if(!prepared){
  if(!get(0,65,0)||!get(31,65,31)){if(system.currentTick-load>200)throw new Error("load timeout");return;}
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("gamerule doweathercycle false");
  cmd("fill 0 64 0 31 64 31 stone");cmd("fill 0 65 0 31 80 31 air");
  for(const inverted of [false,true])for(const cover of ["air","stone","glass","leaves"]){
   const i=cells.length,x=i%4*8+4,z=Math.floor(i/4)*8+4;
   if(cover!=="air")cmd(`fill ${x-2} 68 ${z-2} ${x+2} 68 ${z+2} ${cover} ${cover==="leaves"?'["persistent_bit"=true]':""}`);
   cmd(`setblock ${x} 65 ${z} ${inverted?"daylight_detector_inverted":"daylight_detector"}`);
   cells.push({x,z,inverted,cover});
  }
  prepared=true;
 }
 if(!current){
  const c=cases[index];
  if(c.weather!==weather){cmd(`weather ${c.weather} 100000`);weather=c.weather;wait=system.currentTick+400;return;}
  if(system.currentTick<wait||(system.currentTick&1)!==c.phase)return;
  cmd(`time set ${c.start}`);current={scenario:c,changes:[{tick:system.currentTick,kind:"setup"}],cells,samples:[]};return;
 }
 const expected=current.scenario.start+current.samples.length*20;
 if(world.getTimeOfDay()!==expected)throw new Error("day clock drift");
 const values=cells.map(c=>{const b=get(c.x,65,c.z);return {source:{id:b.typeId,states:b.permutation.getAllStates()},sky:d.getSkyLightLevel({x:c.x,y:65,z:c.z})};});
 current.samples.push({tick:system.currentTick,time:world.getTimeOfDay(),values});
 if(current.samples.length===200){
  emit(current);current=null;index++;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }else cmd(`time set ${expected+20}`);
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
