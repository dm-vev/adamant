#!/usr/bin/env python3
"""Capture native hopper-cart transfers with independent source/destination controls."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,offset=0,load=0,cells=[],ready=false;
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded cart transfer cell "+p);return b;};
const container=p=>{const inv=block(p).getComponent("minecraft:inventory")?.container;if(!inv)throw new Error("missing native container "+p);return inv;};
const count=inv=>{let n=0;for(let i=0;i<inv.size;i++)n+=inv.getItem(i)?.amount??0;return n;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
function snapshot(c){return {tick:system.currentTick,cart:c.entity?count(c.entity.getComponent("minecraft:inventory").container):null,source:c.scenario.source?count(container(c.above)):null,destination:c.scenario.destination?count(container(c.below)):null,rail:read(c.base)};}
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<8&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[64+i%2*128,65,64+Math.floor(i/2)*128],above=[base[0],66,base[2]],below=[base[0],64,base[2]];
  cmd(`fill ${base[0]-3} 64 ${base[2]-4} ${base[0]+3} 70 ${base[2]+4} air`);
  cmd(`fill ${base[0]-3} 64 ${base[2]-4} ${base[0]+3} 64 ${base[2]+4} bedrock`);
  if(scenario.destination)set(below,"barrel");
  if(scenario.source){set(above,"barrel");container(above).setItem(0,new ItemStack("minecraft:diamond",64));}
  if(scenario.mode==="activator_on")set([base[0]+1,65,base[2]],"redstone_block");
  for(let z=-3;z<=3;z++)set([base[0],65,base[2]+z],(scenario.mode==="rail"?"rail":"activator_rail")+' ["rail_direction"=0]');
  cells.push({scenario,base,above,below,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],entity:undefined});
 }start=system.currentTick;
}
function spawn(c){
 const r=read(c.base);if(r.states.rail_direction!==0||(c.scenario.mode!=="rail"&&r.states.rail_data_bit!==(c.scenario.mode==="activator_on")))throw new Error("native hopper track not settled");
 if(c.scenario.source&&count(container(c.above))!==64)throw new Error("native source changed before cart");
 if(c.scenario.destination&&count(container(c.below))!==0)throw new Error("native destination changed before cart");
 c.entity=d.spawnEntity("minecraft:hopper_minecart",{x:c.base[0]+.5,y:65.0625,z:c.base[2]+.5});c.entity.clearVelocity();
 if(c.scenario.initial) c.entity.getComponent("minecraft:inventory").container.setItem(0,new ItemStack("minecraft:diamond",c.scenario.initial));
 c.spawn=snapshot(c);c.spawn.position=c.entity.location;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  for(let z=0;z<512;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${Math.min(z+95,511)} audit_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){
  for(let x=0;x<256;x+=16)for(let z=0;z<512;z+=16)if(!d.getBlock({x,y:65,z})){
   if(system.currentTick-load>480)throw new Error("hopper transfer simulation load timeout");return;
  }
  if(system.currentTick&1)return;ready=true;setup();return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push(snapshot(c));if(t===40)spawn(c);}
 if(t===200){
  for(const c of cells){const {entity,...trace}=c;emit(trace);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(mode=mode, source=source, destination=destination, initial=initial, phase=0)
             for mode in ('rail', 'activator_off', 'activator_on')
             for source in (False, True) for destination in (False, True) for initial in (0, 32)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
