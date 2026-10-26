#!/usr/bin/env python3
"""Capture plate entity filters, contact/release timing and native body bounds.

Entities use native physics between snapshots and are returned to the requested
position after each snapshot. Readback retains their actual AABBs, not assumed
kernel dimensions. No custom entity definitions replace native entities.
"""

import json

from redstone_bds_timing_probe import arguments, capture


BLOCKS = ["wooden_pressure_plate", "spruce_pressure_plate", "birch_pressure_plate", "jungle_pressure_plate",
          "acacia_pressure_plate", "dark_oak_pressure_plate", "crimson_pressure_plate", "warped_pressure_plate",
          "mangrove_pressure_plate", "cherry_pressure_plate", "pale_oak_pressure_plate", "bamboo_pressure_plate",
          "poplar_pressure_plate", "stone_pressure_plate", "polished_blackstone_pressure_plate",
          "light_weighted_pressure_plate", "heavy_weighted_pressure_plate"]
CASES = [{"block": block, "entity": entity, "phase": phase}
         for block in BLOCKS
         for entity in ("minecraft:pig", "minecraft:armor_stand", "minecraft:item", "minecraft:arrow", "minecraft:xp_orb")
         for phase in (0, 1)]
CASES += [{"block": block, "entity": "minecraft:armor_stand", "count": count, "phase": phase}
          for block in BLOCKS[-2:]
          for count in (2, 10, 11, 14, 15, 16, 30, 31, 70, 100, 149, 150, 151)
          for phase in (0, 1)]
CASES += [{"block": block, "entity": "minecraft:armor_stand", "x_offset": x, "y_offset": y, "phase": phase}
          for block in (BLOCKS[0], BLOCKS[13], *BLOCKS[-2:])
          for x in (-0.2, -0.18, 0.5, 1.18, 1.2) for y in (0.02, 0.5) for phase in (0, 1)]


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),cases=CASES;
let d,loaded=0,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=get(x,65,z);if(!b)throw new Error("sampled unloaded plate");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 159 100 79 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");loaded=system.currentTick;return;}
if(!start){
 if(!get(0,65,0)||!get(158,65,78)){if(system.currentTick-loaded>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
 emit({ready:true});cmd("fill 0 64 0 159 64 79 stone");for(let y=65;y<=67;y++)cmd(`fill 0 ${y} 0 159 ${y} 79 air`);
 for(let i=0;i<cases.length;i++){
  const c=cases[i],x=i%32*4+3,z=Math.floor(i/32)*4+3;
  cmd(`setblock ${x} 65 ${z} ${c.block}`);cmd(`setblock ${x+1} 65 ${z} redstone_wire`);
  cells.push({scenario:c,x,z,entities:[],samples:[],changes:[]});
 }
 start=system.currentTick;return;
}
const t=system.currentTick-start;
for(const c of cells){
 const bodies=c.entities.map(e=>{if(!e.isValid)throw new Error("native plate entity vanished");return {id:e.typeId,position:e.location,aabb:e.getAABB()};});
 c.samples.push({tick:system.currentTick,source:read(c.x,c.z),wire:read(c.x+1,c.z),entities:bodies});
 const edge=12+c.scenario.phase,position={x:c.x+(c.scenario.x_offset??0.5),y:65+(c.scenario.y_offset??0.02),z:c.z+0.5};
 if(t===edge){
  for(let n=0;n<(c.scenario.count??1);n++){
   const e=c.scenario.entity==="minecraft:item"?d.spawnItem(new ItemStack("minecraft:dirt",1),position):d.spawnEntity(c.scenario.entity,position);
   c.entities.push(e);
  }c.changes.push({tick:system.currentTick,kind:"spawn"});
 }
 if(t===edge+30){for(const e of c.entities)e.remove();c.entities=[];c.changes.push({tick:system.currentTick,kind:"remove"});}
 for(const e of c.entities){e.teleport(position);e.clearVelocity();}
}
if(t>=80){for(const c of cells){
 const maximum=Math.max(...c.samples.map(s=>s.entities.length));
 if(maximum!==(c.scenario.count??1))throw new Error("native entity count positive control failed");
 if(c.scenario.x_offset===undefined&&c.scenario.y_offset===undefined&&["minecraft:pig","minecraft:armor_stand"].includes(c.scenario.entity)&&!c.samples.some(s=>s.source.states.redstone_signal>0))throw new Error("native plate activation positive control failed");
 delete c.entities;emit(c);
}emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
