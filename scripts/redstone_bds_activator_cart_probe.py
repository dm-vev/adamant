#!/usr/bin/env python3
"""Observe native activator effects, TNT lifetime and hopper transfers."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,offset=0,load=0,cells=[],ready=false,explosions=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded activator cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
world.beforeEvents.explosion.subscribe(e=>{explosions.push({tick:system.currentTick,source:e.source?.typeId,position:e.source?.location});});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];explosions=[];
 for(let i=0;i<8&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[64+i%2*128,65,64+Math.floor(i/2)*128];
  cmd(`fill ${base[0]-3} 64 ${base[2]-5} ${base[0]+3} 70 ${base[2]+5} air`);
  cmd(`fill ${base[0]-3} 64 ${base[2]-5} ${base[0]+3} 64 ${base[2]+5} bedrock`);
  if(scenario.mode==="steady_on")set([base[0],64,base[2]],"redstone_block");
  for(let n=-4;n<=4;n++)set([base[0],65,base[2]+n],'activator_rail ["rail_direction"=0]');
  if(scenario.entity==="minecraft:hopper_minecart"){
   set([base[0],66,base[2]],"chest");const inv=block([base[0],66,base[2]]).getComponent("minecraft:inventory")?.container;
   if(!inv)throw new Error("missing native source chest");inv.setItem(0,new ItemStack("minecraft:diamond",64));
  }
  cells.push({scenario,base,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],entity:undefined,rider:undefined});
 }start=system.currentTick;
}
function snapshot(c){
 const e=c.entity;if(!e||!e.isValid)return null;
 const inv=e.getComponent("minecraft:inventory")?.container,ride=e.getComponent("minecraft:rideable");
 let count=0;if(inv)for(let i=0;i<inv.size;i++)count+=inv.getItem(i)?.amount??0;
 return {id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround,items:count,riders:ride?ride.getRiders().length:0,components:e.getComponents().map(x=>x.typeId).sort()};
}
function spawn(c){
 const r=read(c.base);if(r.id!=="minecraft:activator_rail"||r.states.rail_direction!==0||r.states.rail_data_bit!==(c.scenario.mode==="steady_on"))throw new Error("native activator not settled");
 c.entity=d.spawnEntity(c.scenario.entity,{x:c.base[0]+.5,y:65.0625,z:c.base[2]+.5});c.entity.clearVelocity();
 if(c.scenario.rider){c.rider=d.spawnEntity("minecraft:cow",{x:c.base[0]+1.5,y:65,z:c.base[2]+.5});if(!c.entity.getComponent("minecraft:rideable")?.addRider(c.rider))throw new Error("native cart rider rejected");}
 c.spawn={tick:system.currentTick,body:snapshot(c)};
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  for(let z=0;z<512;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${Math.min(z+95,511)} audit_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){
  for(let x=0;x<256;x+=16)for(let z=0;z<512;z+=16)if(!d.getBlock({x,y:65,z})){
   if(system.currentTick-load>480)throw new Error("activator simulation load timeout");return;
  }
  if(system.currentTick&1)return;ready=true;setup();return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const b=snapshot(c);let source_items=null;
  if(c.scenario.entity==="minecraft:hopper_minecart"){const inv=block([c.base[0],66,c.base[2]]).getComponent("minecraft:inventory")?.container;source_items=0;for(let i=0;i<inv.size;i++)source_items+=inv.getItem(i)?.amount??0;}
  c.samples.push({tick:system.currentTick,body:b,rail:read(c.base),source_items});
  if(t===40)spawn(c);
  if(c.scenario.mode==="toggle"){
   const on=60+c.scenario.input_phase;
   if([on,on+10,on+20,on+30,on+40].includes(t)){const powered=((t-on)/10)%2===0;set([c.base[0],64,c.base[2]],powered?"redstone_block":"bedrock");c.changes.push({tick:system.currentTick,kind:powered?"on":"off"});}
  }
 }
 if(t===200){
  for(const c of cells){const {entity,rider,...trace}=c;trace.explosions=explosions.filter(e=>e.position&&Math.abs(e.position.x-c.base[0])<8&&Math.abs(e.position.z-c.base[2])<8);emit(trace);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity, mode=mode, input_phase=phase, rider=rider, phase=0)
             for entity, rider in (('minecraft:minecart', False), ('minecraft:minecart', True),
                                   ('minecraft:tnt_minecart', False), ('minecraft:hopper_minecart', False))
             for mode in ('steady_off', 'steady_on', 'toggle') for phase in (0, 1)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
