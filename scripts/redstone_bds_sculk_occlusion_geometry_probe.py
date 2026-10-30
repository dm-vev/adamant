#!/usr/bin/env python3
"""Read real BDS vibration obstruction at seams and float32 coordinate boundaries.

Native armour stands operate stone plates. A calibrated listener accepts only
frequency 10, excluding actor creation and scripted movement. Native actor poses
and every source/listener/wire frame are retained; no player client is needed.
"""

import itertools
import json

from redstone_bds_timing_probe import arguments, capture


ANCHORS = (0, 128, 256, -257, 1000000, -1000000)
CASES = [{"block": "stone_pressure_plate", "anchor": anchor, "dx": dx, "dz": dz,
          "mask": mask, "filter": 10, "phase": i & 1,
          "label": f"anchor{anchor}/x{dx}z{dz}/{mask}"}
         for i, (anchor, (dx, dz), mask) in enumerate(itertools.product(
             ANCHORS, ((1,1), (1,-1), (-1,1), (-1,-1)),
             ("none", "a", "b", "both", "centre", "stone", "carpet")))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,anchors=ANCHORS,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,batch=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates(),power:b.getRedstonePower()};};
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  for(const [i,a] of anchors.entries()){const lo=a<0?a-112:a-8,hi=a<0?a+8:a+112;cmd(`tickingarea add ${lo} 0 ${lo} ${hi} 100 ${hi} audit${i} true`);}
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");cmd("gamerule dodaylightcycle false");cmd("time set 6001");cmd("weather clear 100000");return;}
 if(!start){
  if(anchors.some(a=>!get([a,65,a])||!get([a+(a<0?-108:108),65,a+(a<0?-108:108)]))){if(system.currentTick-load>400)throw new Error("load timeout");return;}if(system.currentTick&1)return;
  emit({ready:true});const selected=cases.slice(batch,batch+16);
  for(let i=0;i<selected.length;i++){
   const scenario=selected[i],sign=scenario.anchor<0?-1:1,x=scenario.anchor+sign*(i%4)*34,z=scenario.anchor+sign*Math.floor(i/4)*34;
   cmd(`fill ${x-6} 64 ${z-6} ${x+6} 64 ${z+6} stone`);
   for(let y=65;y<=68;y++)cmd(`fill ${x-6} ${y} ${z-6} ${x+6} ${y} ${z+6} air`);
   const source=[x,65,z],sensor=[x+4*scenario.dx,65,z+4*scenario.dz],input=[sensor[0],65,sensor[2]+1],output=[sensor[0]+1,65,sensor[2]],wire=[x-1,65,z];
   const barriers=[];
   const barrier=(a,b,id)=>{const pos=[x+a*scenario.dx,65,z+b*scenario.dz];set(pos,id);barriers.push({pos,block:read(pos)});};
   if(["a","both"].includes(scenario.mask))barrier(2,1,"white_wool");
   if(["b","both"].includes(scenario.mask))barrier(1,2,"white_wool");
   if(scenario.mask==="centre")barrier(2,2,"white_wool");
   if(scenario.mask==="stone")barrier(1,2,"stone");
   if(scenario.mask==="carpet")barrier(1,2,"white_carpet");
   set(source,"stone_pressure_plate");set(wire,"redstone_wire");set(output,"redstone_wire");
   set(input,'daylight_detector ["redstone_signal"=10]');set(sensor,'calibrated_sculk_sensor ["minecraft:cardinal_direction"="north"]');
   cells.push({scenario,x,z,source,sensor,input,output,wire,barriers,entities:[],samples:[],changes:[]});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.entities=c.entities.filter(e=>e.isValid);
  const entities=c.entities.map(e=>({id:e.typeId,entity_id:e.id,position:e.location,aabb:e.getAABB()}));
  c.samples.push({tick:system.currentTick,source:read(c.source),sensor:read(c.sensor),input:read(c.input),output:read(c.output),wire:read(c.wire),entities});
  const edge=80+c.scenario.phase,position={x:c.source[0]+.5,y:65.02,z:c.source[2]+.5};
  if(t===edge&&read(c.sensor).states.sculk_sensor_phase!==0)throw new Error("listener not idle before contact");
  if(t===edge||t===edge+35){c.entities.push(d.spawnEntity("minecraft:armor_stand",position));c.changes.push({tick:system.currentTick,kind:"spawn",count:1});}
  if(t===edge+70){for(const e of c.entities)e.remove();c.entities=[];c.changes.push({tick:system.currentTick,kind:"remove"});}
  for(const e of c.entities){e.teleport(position);e.clearVelocity();}
 }
 if(t>=210){
  for(const c of cells){
   if(c.changes.length!==3||!c.samples.some(s=>s.entities.length===2&&s.source.states.redstone_signal>0)||c.samples.some(s=>s.input.power!==10))throw new Error("native source/filter control failed");
   const heard=c.samples.some(s=>s.sensor.states.sculk_sensor_phase===1);
   if(["none","stone","carpet"].includes(c.scenario.mask)&&!heard)throw new Error("unobstructed listener positive control failed "+JSON.stringify(c.scenario));
   if(c.scenario.mask==="centre"&&heard)throw new Error("central wool did not obstruct");
   for(const b of c.barriers)if(JSON.stringify(read(b.pos))!==JSON.stringify(b.block))throw new Error("barrier changed during contact");
   delete c.entities;emit(c);
  }
  batch+=cells.length;if(batch===cases.length){emit({complete:true,count:cases.length});system.clearRun(h);}else{cells=[];start=0;}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    script = SCRIPT.replace("CASES", json.dumps(CASES)).replace("ANCHORS", json.dumps(ANCHORS))
    capture(arguments(), script, len(CASES))
