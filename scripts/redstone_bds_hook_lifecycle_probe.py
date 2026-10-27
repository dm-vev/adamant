#!/usr/bin/env python3
"""Capture hook/string/support replacement, repair and listener behavior on BDS.

Each isolated circuit is read every game tick. Tile drops are disabled; optional
native armour stands remain on the string throughout the controlled edits.
Their actual bodies are retained rather than replacing native entity physics.
"""

import itertools
import json

from redstone_bds_timing_probe import arguments,capture


CASES=[{"mode":mode,"powered":powered,"filter":frequency,"phase":phase}
       for mode,powered,frequency,phase in itertools.product(
           ("remove_left","remove_right","left_support","right_support","remove_string",
            "rotate_left","replace_left","complete_string","add_right"),
           (False,True),(9,10),(0,1))]

SCRIPT=r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,batch=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates(),power:b.getRedstonePower()};};
const set=(p,id,direction,attached,powered)=>{
 const states=[];
 for(const [key,value] of [["direction",direction],["attached_bit",attached],["powered_bit",powered]])if(value!==undefined)states.push(JSON.stringify(key)+"="+JSON.stringify(value));
 cmd(`setblock ${p.join(" ")} ${id} ${states.length?'['+states.join(',')+']':''}`);
};
const apply=(c,actions)=>{for(const a of actions)set(a.pos,a.block,a.direction,a.attached,a.powered);c.changes.push({tick:system.currentTick,kind:"edit",actions});};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 143 100 175 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");cmd("gamerule dodaylightcycle false");cmd("time set 6001");cmd("weather clear 100000");return;}
 if(!start){
  if(!get([0,65,0])||!get([143,65,175])){if(system.currentTick-load>240)throw new Error("load timeout");return;}if(system.currentTick&1)return;
  emit({ready:true});const selected=cases.slice(batch,batch+16);
  for(let i=0;i<selected.length;i++){
   const scenario=selected[i],x=3+(i%4)*34,z=3+Math.floor(i/4)*34;
   cmd(`fill ${x-3} 64 ${z-2} ${x+9} 64 ${z+3} stone`);
   for(let y=65;y<=68;y++)cmd(`fill ${x-3} ${y} ${z-2} ${x+9} ${y} ${z+3} air`);
   const left=[x,65,z],right=[x+2,65,z],source=[x+1,65,z],left_support=[x-1,65,z],right_support=[x+3,65,z],sensor=[x+5,65,z],input=[x+5,65,z+1],output=[x+6,65,z],wire=[x-2,65,z],right_wire=[x+4,65,z];
   const direct_wire=scenario.permutations?[x,65,z-1]:undefined;
   if(direct_wire){set([x,65,z+1],"stone");set(direct_wire,"redstone_wire");}
   set(left_support,"stone");set(right_support,"stone");set(left,"tripwire_hook",3);
   if(scenario.mode!=="add_right")set(right,"tripwire_hook",1);
   if(scenario.mode!=="complete_string")set(source,"trip_wire");
   set(wire,"redstone_wire");set(right_wire,"redstone_wire");set(output,"redstone_wire");
   cmd(`setblock ${input.join(" ")} daylight_detector ["redstone_signal"=${scenario.filter}]`);
   cmd(`setblock ${sensor.join(" ")} calibrated_sculk_sensor ["minecraft:cardinal_direction"="north"]`);
   cells.push({scenario,x,z,left,right,source,left_support,right_support,sensor,input,output,wire,right_wire,direct_wire,entities:[],samples:[],changes:[]});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const q=c.scenario;
  c.entities=c.entities.filter(e=>e.isValid);
  const entities=c.entities.map(e=>({id:e.typeId,entity_id:e.id,position:e.location,aabb:e.getAABB()}));
  c.samples.push({tick:system.currentTick,left:read(c.left),right:read(c.right),source:read(c.source),left_support:read(c.left_support),right_support:read(c.right_support),sensor:read(c.sensor),input:read(c.input),output:read(c.output),wire:read(c.wire),right_wire:read(c.right_wire),direct_wire:c.direct_wire?read(c.direct_wire):undefined,entities});
  if(t===40&&q.powered)c.entities.push(d.spawnEntity("minecraft:armor_stand",{x:c.source[0]+.5,y:65.02,z:c.z+.5}));
  if(t===70&&q.mode!=="complete_string"&&q.mode!=="add_right"&&q.powered&&(!read(c.left).states.powered_bit||!read(c.right).states.powered_bit))throw new Error("powered lifecycle positive control failed");
  const edge=80+q.phase;
  if(t===edge){
   let actions;
   switch(q.mode){
    case "remove_left":actions=[{pos:c.left,block:"air"}];break;
    case "remove_right":actions=[{pos:c.right,block:"air"}];break;
    case "left_support":actions=[{pos:c.left_support,block:"air"}];break;
    case "right_support":actions=[{pos:c.right_support,block:"air"}];break;
    case "remove_string":actions=[{pos:c.source,block:"air"}];break;
    case "rotate_left":actions=[{pos:c.left,block:"tripwire_hook",direction:2}];break;
    case "replace_left":actions=[{pos:c.left,block:"stone"}];break;
    case "complete_string":actions=[{pos:c.source,block:"trip_wire"}];break;
    case "add_right":actions=[{pos:c.right,block:"tripwire_hook",direction:1}];break;
    case "replace_on":actions=[{pos:c.left,block:"air"},{pos:c.left,block:"tripwire_hook",direction:3,attached:true,powered:true}];break;
    case "replace_off":actions=[{pos:c.left,block:"air"},{pos:c.left,block:"tripwire_hook",direction:3,attached:false,powered:false}];break;
    case "rotate_supported":actions=[{pos:c.left,block:"tripwire_hook",direction:2}];break;
    case "rotate_on":actions=[{pos:c.left,block:"tripwire_hook",direction:2,attached:true,powered:true}];break;
    case "edit_on":actions=[{pos:c.left,block:"tripwire_hook",direction:3,attached:true,powered:true}];break;
    case "edit_off":actions=[{pos:c.left,block:"tripwire_hook",direction:3,attached:true,powered:false}];break;
    case "edit_detached":actions=[{pos:c.left,block:"tripwire_hook",direction:3,attached:false,powered:true}];break;
    case "edit_idle":actions=[{pos:c.left,block:"tripwire_hook",direction:3,attached:false,powered:false}];break;
   }apply(c,actions);
  }
  if(t===edge+35){
   const actions=[{pos:c.left_support,block:"stone"},{pos:c.right_support,block:"stone"},{pos:c.left,block:"tripwire_hook",direction:3},{pos:c.right,block:"tripwire_hook",direction:1}];
   if(q.mode==="remove_string")actions.push({pos:c.source,block:"trip_wire"});
   apply(c,actions);
  }
  if(t===edge+70)apply(c,[{pos:c.source,block:"air"}]);
  for(const e of c.entities){e.teleport({x:c.source[0]+.5,y:65.02,z:c.z+.5});e.clearVelocity();}
 }
 if(t>=240){
  if(d.getEntities({type:"minecraft:item"}).length!==0)throw new Error("tile drops disabled but native item actors appeared");
  for(const c of cells){
   if(c.changes.length!==3||c.samples.some(s=>s.input.power!==c.scenario.filter))throw new Error("lifecycle control failed");
   if(c.scenario.powered&&!c.samples.some(s=>s.entities.length===1))throw new Error("native stand missing");
   for(const e of c.entities)e.remove();delete c.entities;emit(c);
  }
  batch+=cells.length;
  if(batch===cases.length){emit({complete:true,count:cases.length});system.clearRun(h);}else{cells=[];start=0;}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__=="__main__":
    capture(arguments(),SCRIPT.replace("CASES",json.dumps(CASES)),len(CASES))
