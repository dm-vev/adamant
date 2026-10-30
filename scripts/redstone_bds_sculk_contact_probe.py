#!/usr/bin/env python3
"""Observe native contact-driven sources through frequency-filtered sensors.

Ordinary native entities run their own physics. Their actual positions/AABBs
are retained for replay; scripted teleports and spawn events have frequencies
excluded by the calibrated sensor's stable 9/10 input.
"""

import argparse
import itertools
import json
from pathlib import Path

from redstone_bds_timing_probe import capture
from redstone_bds_pressure_plate_probe import BLOCKS as PLATES


def cases(suite):
    if suite == "plates":
        return [{"block": name, "filter": frequency, "phase": phase}
                for name, frequency, phase in itertools.product(
                    PLATES, (9,10), (0,1))]
    return [{"block": "tripwire_hook", "entity": entity, "filter": frequency, "phase": phase}
            for entity, frequency, phase in itertools.product(
                ("minecraft:pig", "minecraft:armor_stand", "minecraft:item", "minecraft:arrow", "minecraft:xp_orb"),
                (9,10), (0,1))]


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,batch=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates(),power:b.getRedstonePower()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 143 100 175 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("time set 6001");cmd("weather clear 100000");return;}
 if(!start){
  if(!get([0,65,0])||!get([143,65,175])){if(system.currentTick-load>240)throw new Error("load timeout");return;}if(system.currentTick&1)return;
  emit({ready:true});
  const selected=cases.slice(batch,batch+16);
  for(let i=0;i<selected.length;i++){
   const c=selected[i],x=3+(i%4)*34,z=3+Math.floor(i/4)*34,tripwire=c.block==="tripwire_hook";
   cmd(`fill ${x-2} 64 ${z-2} ${x+9} 64 ${z+3} stone`);
   for(let y=65;y<=68;y++)cmd(`fill ${x-2} ${y} ${z-2} ${x+9} ${y} ${z+3} air`);
   const sensor=[x+(tripwire?5:4),65,z],input=[sensor[0],65,z+1],output=[sensor[0]+1,65,z],source=[x+(tripwire?1:0),65,z],wire=[x-2,65,z];
   if(tripwire){
    cmd(`setblock ${x-1} 65 ${z} stone`);cmd(`setblock ${x+3} 65 ${z} stone`);
    cmd(`setblock ${x} 65 ${z} tripwire_hook ["direction"=3]`);cmd(`setblock ${x+2} 65 ${z} tripwire_hook ["direction"=1]`);
    cmd(`setblock ${source.join(" ")} trip_wire`);
   }else cmd(`setblock ${source.join(" ")} ${c.block}`);
   const sourceWire=tripwire?wire:[x-1,65,z];
   cmd(`setblock ${sourceWire.join(" ")} redstone_wire`);cmd(`setblock ${output.join(" ")} redstone_wire`);
   cmd(`setblock ${input.join(" ")} daylight_detector ["redstone_signal"=${c.filter}]`);
   cmd(`setblock ${sensor.join(" ")} calibrated_sculk_sensor ["minecraft:cardinal_direction"="north"]`);
   cells.push({scenario:c,x,z,source,sensor,input,output,wire:sourceWire,entities:[],changes:[],samples:[]});
  }
  start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  // Native XP/item/arrow actors can merge or be removed by their own physics.
  // Their absence is part of the captured population, not a synthetic removal.
  c.entities=c.entities.filter(e=>e.isValid);
  const bodies=c.entities.map(e=>{if(!e.isValid)throw new Error("native source entity disappeared");return {id:e.typeId,entity_id:e.id,position:e.location,aabb:e.getAABB()};});
  const tripwire=c.scenario.block==="tripwire_hook";
  c.samples.push({tick:system.currentTick,source:read(c.source),wire:read(c.wire),sensor:read(c.sensor),input:read(c.input),output:read(c.output),left:tripwire?read([c.x,65,c.z]):null,right:tripwire?read([c.x+2,65,c.z]):null,entities:bodies});
  const edge=80+c.scenario.phase,position={x:c.source[0]+.5,y:65.02,z:c.z+.5};
  if(t===edge&&read(c.sensor).states.sculk_sensor_phase!==0)throw new Error("native sensor not idle before contact");
  if(t===edge||t===edge+35){
   const count=t===edge?1:(c.scenario.block==="heavy_weighted_pressure_plate"?10:1),id=c.scenario.entity??"minecraft:armor_stand";
   for(let n=0;n<count;n++)c.entities.push(id==="minecraft:item"?d.spawnItem(new ItemStack("minecraft:dirt",1),position):d.spawnEntity(id,position));
   c.changes.push({tick:system.currentTick,kind:"spawn",count});
  }
  if(t===edge+70){for(const e of c.entities)e.remove();c.entities=[];c.changes.push({tick:system.currentTick,kind:"remove"});}
  for(const e of c.entities){e.teleport(position);e.clearVelocity();}
 }
 if(t>=210){
  for(const c of cells){
   const tripwire=c.scenario.block==="tripwire_hook";
   if(c.changes.length!==3||!c.samples.some(s=>s.entities.length>=(tripwire?1:2)))throw new Error("native source population control failed");
   if(c.samples.some(s=>s.input.power!==c.scenario.filter))throw new Error("native filter not stable");
   if(c.scenario.entity!=="minecraft:xp_orb"&&!c.samples.some(s=>s.sensor.states.sculk_sensor_phase===1))throw new Error("native vibration positive control failed");
   delete c.entities;emit(c);
  }
  batch+=cells.length;
  if(batch===cases.length){emit({complete:true,count:cases.length});system.clearRun(h);}
  else {cells=[];start=0;}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary","assets","output"):
        parser.add_argument("--"+name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30877)
    parser.add_argument("--suite", choices=("plates","tripwire"), required=True)
    args = parser.parse_args()
    scenarios = cases(args.suite)
    capture(args, SCRIPT.replace("CASES",json.dumps(scenarios)), len(scenarios))
