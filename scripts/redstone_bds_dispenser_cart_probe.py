#!/usr/bin/env python3
"""Capture native dispenser cart origins and first motion, including spawn events."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,offset=0,load=0,cells=[],ready=false;
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded cart dispenser cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const body=e=>({id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround});
world.afterEvents.entitySpawn.subscribe(e=>{for(const c of cells){const p=e.entity.location;if(Math.abs(p.x-c.base[0])<12&&Math.abs(p.z-c.base[2])<12)c.entity_spawns.push({tick:system.currentTick,body:body(e.entity)});}});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<8&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[64+i%2*128,65,64+Math.floor(i/2)*128],dx=scenario.face===5?1:scenario.face===4?-1:0,dz=scenario.face===3?1:scenario.face===2?-1:0;
  const floor_y=scenario.track==="below"?63:64;
  cmd(`fill ${base[0]-12} 63 ${base[2]-12} ${base[0]+12} 70 ${base[2]+12} air`);
  cmd(`fill ${base[0]-12} ${floor_y} ${base[2]-12} ${base[0]+12} ${floor_y} ${base[2]+12} bedrock`);
  const rail_positions=[];
  for(let u=1;u<=9;u++){
   if(scenario.track==="none"||scenario.track==="blocked")break;
   const y=scenario.track==="below"?64:scenario.slope&&u>=2?66:65,p=[base[0]+u*dx,y,base[2]+u*dz];rail_positions.push(p);
   if(y===66)set([p[0],65,p[2]],"stone");
  }
  set(base,'dispenser ["facing_direction"='+scenario.face+']');
  if(scenario.track==="blocked")set([base[0]+dx,65,base[2]+dz],"stone");
  for(let i=rail_positions.length-1;i>=0;i--){
   let dir=dx?1:0;if(scenario.slope&&i===0)dir=dx===1?2:dx===-1?3:dz===1?5:4;
   set(rail_positions[i],'rail ["rail_direction"='+dir+']');
  }
  cells.push({scenario,base,floor_y,rail_positions,source:[base[0]-dx,65,base[2]-dz],setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],entity_spawns:[]});
 }start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  for(let z=0;z<512;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${Math.min(z+95,511)} audit_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){
  for(let x=0;x<256;x+=16)for(let z=0;z<512;z+=16)if(!d.getBlock({x,y:65,z})){
   if(system.currentTick-load>480)throw new Error("dispenser cart simulation load timeout");return;
  }
  if(system.currentTick&1)return;ready=true;setup();return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const inv=block(c.base).getComponent("minecraft:inventory")?.container;if(!inv)throw new Error("missing native dispenser inventory");
  const carts=d.getEntities({type:c.scenario.item,location:{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5},maxDistance:12});
  const fallback_items=d.getEntities({type:"minecraft:item",location:{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5},maxDistance:12}).map(e=>{const s=e.getComponent("minecraft:item").itemStack;return {id:s.typeId,count:s.amount};});
  c.samples.push({tick:system.currentTick,dispenser:read(c.base),items:inv.getItem(0)?.amount??0,bodies:carts.map(body),fallback_items});
  if(t===40){
   c.setup_control=[];
   for(let i=0;i<c.rail_positions.length;i++){
    const r=read(c.rail_positions[i]);let direction=c.scenario.face<=3?0:1;
    if(c.scenario.slope&&i===0)direction=c.scenario.face===5?2:c.scenario.face===4?3:c.scenario.face===3?5:4;
    if(r.id!=="minecraft:rail"||r.states.rail_direction!==direction)throw new Error("native dispenser track changed "+JSON.stringify({scenario:c.scenario,position:c.rail_positions[i],actual:r}));
    c.setup_control.push({position:c.rail_positions[i],block:r});
   }
   inv.setItem(0,new ItemStack(c.scenario.item,1));c.changes.push({tick:system.currentTick,kind:"load"});
  }
  if(t===60){set(c.source,"redstone_block");c.changes.push({tick:system.currentTick,kind:"power"});}
 }
 if(t===140){
  for(const c of cells){const last=c.samples[c.samples.length-1],total=last.items+last.bodies.length+last.fallback_items.reduce((n,x)=>n+x.count,0);if(total!==1||c.entity_spawns.length>1||(!c.scenario.track&&(last.items!==0||last.bodies.length!==1||c.entity_spawns.length!==1)))throw new Error("native dispenser outcome control failed "+JSON.stringify(c.scenario));emit(c);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(item=kind,face=face,slope=slope,phase=0)
             for kind in ('minecraft:minecart','minecraft:chest_minecart','minecraft:hopper_minecart',
                          'minecraft:tnt_minecart','minecraft:command_block_minecart')
             for face in (2,3,4,5) for slope in (False,True)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
