#!/usr/bin/env python3
"""Capture real lightning hits, retriggers and rod output in unmodified BDS."""

import itertools
import json

from redstone_bds_timing_probe import arguments, capture

MATERIALS = [prefix + age + "lightning_rod" for prefix, age in
             itertools.product(("", "waxed_"), ("", "exposed_", "weathered_", "oxidized_"))]
CASES = [{"material": "lightning_rod", "face": face, "phase": phase, "repeat": repeat}
         for face, phase, repeat in itertools.product(range(6), range(2), (0, 3))]
CASES += [{"material": material, "face": 1, "phase": phase, "repeat": 0}
          for material, phase in itertools.product(MATERIALS[1:], range(2))]
CASES += [{"material": material, "face": 1, "phase": phase, "repeat": 0, "on": on}
          for material, phase, on in itertools.product(MATERIALS, range(2), (0, 1))]
CASES += [{"material": "lightning_rod", "face": face, "phase": phase, "repeat": 0, "height": height}
          for face, phase, height in itertools.product(range(6), range(2), (0, 1))]
CASES += [{"material": "lightning_rod", "face": 1, "phase": phase, "repeat": repeat}
          for phase, repeat in itertools.product(range(2), (7, 8, 9))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const offsets=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,load,index=0,current=null,start=0;
const cmd=x=>d.runCommand(x),get=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,y,z)=>{const b=get(x,y,z);return {id:b.typeId,states:b.permutation.getAllStates()};};
const strike=()=>{const c=current.scenario,bolt=d.spawnEntity("minecraft:lightning_bolt",{x:.5,y:65+(c.height??.5),z:.5});current.changes.push({tick:system.currentTick,kind:"strike",id:bolt.id,location:bolt.location});};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 audit true");load=system.currentTick;cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dofiretick false");cmd("difficulty peaceful");cmd("weather clear 100000");return;}
 if(!current){
  if(!get(-4,65,-4)||!get(4,68,4)){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  const c=cases[index];if((system.currentTick&1)!==c.phase)return;
  for(const e of d.getEntities({type:"minecraft:lightning_bolt"}))e.remove();
  cmd("fill -4 64 -4 4 68 4 air");cmd("fill -4 64 -4 4 64 4 stone");
  const o=offsets[c.face^1];cmd(`setblock ${o[0]} ${65+o[1]} ${o[2]} stone`);
  cmd(`setblock 0 65 0 ${c.material} ["facing_direction"=${c.face}]`);
  const w=c.face===2||c.face===3?[1,65,0]:[0,65,1];cmd(`setblock ${w.join(" ")} redstone_wire`);
  const strong=[o[0]+(c.face<2||c.face===2||c.face===3?-1:0),65+o[1],o[2]+(c.face===4||c.face===5?-1:0)];
  cmd(`setblock ${strong[0]} ${strong[1]-1} ${strong[2]} stone`);cmd(`setblock ${strong.join(" ")} redstone_wire`);
  start=system.currentTick;current={scenario:c,changes:[{tick:start,kind:"setup"}],samples:[],wire:w,strong_wire:strong};if(c.on===0)strike();return;
 }
 const c=current.scenario,t=system.currentTick-start;
 current.samples.push({tick:system.currentTick,source:read(0,65,0),wire:read(...current.wire),strong_wire:read(...current.strong_wire),bolts:d.getEntities({type:"minecraft:lightning_bolt"}).map(e=>({id:e.id,location:e.location}))});
 if(t===(c.on??8)||(c.repeat&&t===8+c.repeat))strike();
 if(t===36){
  const requiredHit=c.height===undefined;
  if(!current.samples.some(s=>s.bolts.length))throw new Error("missing real bolt");
  if(requiredHit&&!current.samples.some(s=>s.source.states.powered_bit))throw new Error("missing direct rod hit");
  // A bolt spawned in the setup frame can hit before producer registration.
  // Keep that trace, including zero circuit output, rather than rejecting it.
  if(requiredHit&&(c.on??8)>0&&!current.samples.some(s=>s.wire.states.redstone_signal>0))throw new Error("rod did not power dust");
  emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
