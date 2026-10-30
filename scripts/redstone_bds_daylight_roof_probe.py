#!/usr/bin/env python3
"""Observe live roof insertion/removal, detector sampling and dust publication."""

import itertools
import json
from redstone_bds_timing_probe import arguments, capture

CASES = [{"x": x, "cover": cover, "phase": phase}
         for x, cover, phase in itertools.product((4, 15, 16, -1), ("stone", "glass", "leaves"), (0, 1))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,index=0,current=null,start=0;
const cmd=x=>d.runCommand(x),get=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=get(x,65,z),w=get(x+1,65,z);return {source:{id:b.typeId,states:b.permutation.getAllStates()},wire:{id:w.typeId,states:w.permutation.getAllStates()},sky:d.getSkyLightLevel({x,y:65,z})};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -32 0 -16 48 100 32 audit true");load=system.currentTick;cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("weather clear 100000");cmd("time set 6000");return;}
 if(!current){
  if(!get(-32,65,-8)||!get(47,65,16)){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  const c=cases[index];if((system.currentTick&1)!==c.phase)return;
  cmd("fill -32 65 -8 47 72 16 air");cmd(`fill ${c.x-5} 64 -5 ${c.x+5} 64 13 stone`);
  for(const z of [0,8]){cmd(`setblock ${c.x} 65 ${z} ${z===0?"daylight_detector":"daylight_detector_inverted"}`);cmd(`setblock ${c.x+1} 65 ${z} redstone_wire`);}
  start=system.currentTick;current={scenario:c,changes:[{tick:start,kind:"setup"}],samples:[]};return;
 }
 const c=current.scenario;
 current.samples.push({tick:system.currentTick,time:world.getTimeOfDay(),values:[read(c.x,0),read(c.x,8)]});
 if(system.currentTick-start===12||system.currentTick-start===40){
  const added=system.currentTick-start===12,material=added?c.cover:"air";
  for(const z of [0,8])cmd(`fill ${c.x-2} 68 ${z-2} ${c.x+2} 68 ${z+2} ${material} ${material==="leaves"?'["persistent_bit"=true]':""}`);
  current.changes.push({tick:system.currentTick,kind:added?"cover":"uncover"});
 }
 if(system.currentTick-start===80){
  if(current.samples[7].values[0].source.states.redstone_signal!==15||current.samples.at(-1).values[0].source.states.redstone_signal!==15)throw new Error("daylight positive control failed");
  emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
