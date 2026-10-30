#!/usr/bin/env python3
"""Measure light loss and source removal in a sealed native test corridor."""

import itertools
import json
from redstone_bds_timing_probe import arguments, capture

CASES = [{"filter": block, "phase": phase, "other": other}
         for block, phase, other in itertools.product(("air", "glass", "leaves", "stone"), (0, 1), (False, True))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,index=0,current=null,start=0;
const cmd=x=>d.runCommand(x);
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 32 100 16 audit true");load=system.currentTick;cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("weather clear 100000");cmd("time set 6000");return;}
 if(!current){
  if(!d.getBlock({x:0,y:65,z:0})){if(system.currentTick-load>200)throw new Error("load timeout");return;}
  const c=cases[index];if((system.currentTick&1)!==c.phase)return;
  cmd("fill -1 64 -1 9 66 1 stone");cmd("fill 0 65 0 8 65 0 air");
  if(c.filter!=="air")cmd(`setblock 1 65 0 ${c.filter} ${c.filter==="leaves"?'["persistent_bit"=true]':""}`);
  if(c.other)cmd("setblock 8 65 0 glowstone");
  start=system.currentTick;current={scenario:c,changes:[{tick:start,kind:"setup"}],samples:[]};return;
 }
 const values=Array.from({length:9},(_,x)=>({light:d.getLightLevel({x,y:65,z:0}),sky:d.getSkyLightLevel({x,y:65,z:0})}));
 current.samples.push({tick:system.currentTick,source:d.getBlock({x:0,y:65,z:0}).typeId,other:d.getBlock({x:8,y:65,z:0}).typeId,values});
 const age=system.currentTick-start;
 if(age===8||age===32||age===48){const b=age===32?"air":"glowstone";cmd(`setblock 0 65 0 ${b}`);current.changes.push({tick:system.currentTick,kind:b==="air"?"off":"on"});}
 if(age===64){cmd("setblock 0 65 0 air");cmd("setblock 8 65 0 air");current.changes.push({tick:system.currentTick,kind:"all_off"});}
 if(age===72){
  if(!current.samples.some(s=>s.source==="minecraft:glowstone")||current.samples.some(s=>s.values.some(v=>v.sky!==0))||(current.scenario.filter==="air"&&!current.samples.some(s=>s.values[1].light===14)))throw new Error("sealed light control failed");
  emit(current);current=null;index++;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
