#!/usr/bin/env python3
"""Capture native block-hopper pickup, partial capacity, locking and subsequent push."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded hopper item cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const inv=p=>{const c=block(p).getComponent("minecraft:inventory")?.container;if(!c)throw new Error("missing hopper inventory");return c;};
const slots=p=>{const c=inv(p),out=[];for(let i=0;i<c.size;i++){const s=c.getItem(i);out.push(s?{id:s.typeId,count:s.amount}:null);}return out;};
const body=e=>{const s=e.getComponent("minecraft:item").itemStack;return {actor_id:e.id,id:s.typeId,count:s.amount,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround};};
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<12&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[32+i%4*64,65,32+Math.floor(i/4)*64],destination=scenario.destination?[base[0],65,base[2]-1]:null;
  cmd(`fill ${base[0]-8} 63 ${base[2]-8} ${base[0]+8} 70 ${base[2]+8} air`);
  cmd(`fill ${base[0]-8} 64 ${base[2]-8} ${base[0]+8} 64 ${base[2]+8} bedrock`);
  set(base,'hopper ["facing_direction"=2]');
  if(destination)set(destination,"barrel");
  const source=[base[0]+1,65,base[2]];if(scenario.locked)set(source,"redstone_block");
  cells.push({scenario,base,destination,source,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],spawns:[]});
 }start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_hopper_items_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){for(let x=0;x<256;x+=16)for(let z=0;z<192;z+=16)if(!d.getBlock({x,y:65,z})){
  if(system.currentTick-loaded>480)throw new Error("hopper item chunk load timeout");return;
 }if(system.currentTick&1)return;ready=true;setup();return;}
 const t=system.currentTick-start;
 for(const c of cells){
  const drops=d.getEntities({type:"minecraft:item",location:{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5},maxDistance:12});
  c.samples.push({tick:system.currentTick,block:read(c.base),items:slots(c.base),destination:c.destination?read(c.destination):null,destination_items:c.destination?slots(c.destination):null,drops:drops.map(body)});
  if(t===40){
   if(read(c.base).states.toggle_bit!==c.scenario.locked)throw new Error("hopper lock control failed");
   const h=inv(c.base);let n=c.scenario.initial;
   for(let i=0;i<h.size;i++){if(n){const take=Math.min(n,64);h.setItem(i,new ItemStack("minecraft:diamond",take));n-=take;}}
   if(n)throw new Error("invalid hopper initial count");c.load_control=slots(c.base);
  }
  if(t===40||(c.scenario.second&&t===44)){
   const id=t===40?"minecraft:diamond":"minecraft:dirt",n=t===40?c.scenario.count:3;
   const e=d.spawnItem(new ItemStack(id,n),{x:c.base[0]+.5+c.scenario.dx,y:c.base[1]+c.scenario.dy,z:c.base[2]+.5});
   c.spawns.push({tick:system.currentTick,body:body(e)});c.changes.push({tick:system.currentTick,kind:"spawn",id,count:n});
  }
 }
 if(t===120){for(const c of cells){if(c.spawns.length!==(c.scenario.second?2:1))throw new Error("missing native item spawn");emit(c);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    profiles = [('inside',0,.7),('mouth',0,1.),('edge',.8,1.),('high',0,2.)]
    cases = [dict(initial=initial,count=count,profile=profile,dx=dx,dy=dy,
                  destination=destination,locked=locked,phase=0)
             for initial in (0,319,320) for count in (3,64) for profile,dx,dy in profiles
             for destination in (False,True) for locked in (False,True)]
    cases += [dict(initial=0,count=3,profile='mouth',dx=0,dy=1.,destination=destination,
                   locked=False,second=True,phase=0) for destination in (False,True)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
