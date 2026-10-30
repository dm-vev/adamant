#!/usr/bin/env python3
"""Observe tripwire contact, hook publication, disarming and release on BDS."""

import json

from redstone_bds_timing_probe import arguments, capture


CASES = [{"mode": mode, "suspended": suspended, "entity": entity, "phase": phase}
         for mode in ("connected", "disarmed", "unhooked")
         for suspended in (False, True)
         for entity in ("minecraft:pig", "minecraft:armor_stand", "minecraft:item", "minecraft:arrow", "minecraft:xp_orb")
         for phase in (0, 1)]
CASES += [{"mode": mode, "suspended": suspended, "entity": "minecraft:armor_stand",
           "y_offset": y, "phase": phase}
          for mode in ("connected", "unhooked") for suspended in (False, True)
          for y in (0.02, 0.1, 0.16, 0.3, 0.49, 0.51) for phase in (0, 1)]
CASES += [{"mode": mode, "suspended": suspended, "entity": "minecraft:armor_stand",
           "x_offset": x, "phase": phase}
          for mode in ("connected", "unhooked") for suspended in (False, True)
          for x in (-0.26, -0.24, 1.24, 1.26) for phase in (0, 1)]

SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),cases=CASES;
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=get(x,65,z);if(!b)throw new Error("unloaded tripwire cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 159 100 63 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
 if(!start){
  if(!get(0,65,0)||!get(158,65,62)){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
  emit({ready:true});cmd("fill 0 64 0 159 64 63 stone");for(let y=65;y<=67;y++)cmd(`fill 0 ${y} 0 159 ${y} 63 air`);
  for(let i=0;i<cases.length;i++){
   const c=cases[i],x=i%20*8+3,z=Math.floor(i/20)*8+3;
   if(c.mode!=="unhooked"){
    cmd(`setblock ${x-1} 65 ${z} stone`);cmd(`setblock ${x+3} 65 ${z} stone`);
    cmd(`setblock ${x} 65 ${z} tripwire_hook ["direction"=3]`);
    cmd(`setblock ${x+2} 65 ${z} tripwire_hook ["direction"=1]`);
   }
   cmd(`setblock ${x+1} 65 ${z} trip_wire ["suspended_bit"=${c.suspended},"disarmed_bit"=${c.mode==="disarmed"}]`);
   cmd(`setblock ${x-2} 65 ${z} redstone_wire`);
   cells.push({scenario:c,x,z,entities:[],samples:[],changes:[]});
  }
  start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const bodies=c.entities.map(e=>{if(!e.isValid)throw new Error("native tripwire entity disappeared");return {id:e.typeId,position:e.location,aabb:e.getAABB()};});
  c.samples.push({tick:system.currentTick,source:read(c.x+1,c.z),left:read(c.x,c.z),right:read(c.x+2,c.z),wire:read(c.x-2,c.z),entities:bodies});
  if(t===8){
   const b=get(c.x+1,65,c.z);b.setPermutation(b.permutation.withState("suspended_bit",c.scenario.suspended).withState("disarmed_bit",c.scenario.mode==="disarmed"));
   c.configuration={tick:system.currentTick,source:read(c.x+1,c.z)};
   if(c.configuration.source.states.suspended_bit!==c.scenario.suspended||c.configuration.source.states.disarmed_bit!==(c.scenario.mode==="disarmed"))throw new Error("native configuration edit was not applied");
  }
  const edge=12+c.scenario.phase,position={x:c.x+1+(c.scenario.x_offset??0.5),y:65+(c.scenario.y_offset??0.02),z:c.z+0.5};
  if(t===edge){c.entities.push(c.scenario.entity==="minecraft:item"?d.spawnItem(new ItemStack("minecraft:dirt",1),position):d.spawnEntity(c.scenario.entity,position));c.changes.push({tick:system.currentTick,kind:"spawn"});}
  if(t===edge+30){for(const e of c.entities)e.remove();c.entities=[];c.changes.push({tick:system.currentTick,kind:"remove"});}
  for(const e of c.entities){e.teleport(position);e.clearVelocity();}
 }
 if(t>=80){
  for(const c of cells){
   if(!c.samples.some(s=>s.entities.length===1))throw new Error("native entity spawn control failed");
   if(c.scenario.mode==="connected"&&c.scenario.entity==="minecraft:pig"&&!c.samples.some(s=>s.left.states.powered_bit&&s.right.states.powered_bit))throw new Error("native hook positive control failed");
   delete c.entities;emit(c);
  }emit({complete:true,count:cells.length});system.clearRun(h);
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
