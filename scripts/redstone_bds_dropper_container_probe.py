#!/usr/bin/env python3
"""Capture native dropper insertion/rejection for full and filtered containers."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const vectors=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,loaded=0,ready=false,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded container cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const inv=p=>{const c=block(p).getComponent("minecraft:inventory")?.container;if(!c)throw new Error("missing inventory at "+p);return c;};
const slots=p=>{const c=inv(p),out=[];for(let i=0;i<c.size;i++){const s=c.getItem(i);out.push(s?{id:s.typeId,count:s.amount}:null);}return out;};
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<12&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[32+i%4*64,65,32+Math.floor(i/4)*64],v=vectors[scenario.face];
  const target=base.map((x,j)=>x+v[j]),source=base.map((x,j)=>x-v[j]);
  cmd(`fill ${base[0]-8} 62 ${base[2]-8} ${base[0]+8} 70 ${base[2]+8} air`);
  cmd(`fill ${base[0]-8} 63 ${base[2]-8} ${base[0]+8} 63 ${base[2]+8} bedrock`);
  set(base,scenario.kind+' ["facing_direction"='+scenario.face+']');
  if(scenario.face!==1)set([target[0],target[1]-1,target[2]],"stone");
  set(target,scenario.target);
  let lock=null;
  if(scenario.target==="hopper"){
   const p=vectors[scenario.face>=4?2:5];lock=target.map((x,j)=>x+p[j]);set(lock,"redstone_block");
  }
  cells.push({scenario,base,target,source,lock,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[]});
 }start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_dropper_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){for(let x=0;x<256;x+=16)for(let z=0;z<192;z+=16)if(!d.getBlock({x,y:65,z})){
  if(system.currentTick-loaded>480)throw new Error("container chunk load timeout");return;
 }if(system.currentTick&1)return;ready=true;setup();return;}
 const t=system.currentTick-start;
 for(const c of cells){
  if(read(c.target).id!=="minecraft:"+c.scenario.target)throw new Error("container lost support");
  c.samples.push({tick:system.currentTick,block:read(c.base),items:slots(c.base),target:read(c.target),target_items:slots(c.target),
   drops:d.getEntities({type:"minecraft:item",location:{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5},maxDistance:12}).map(e=>{const s=e.getComponent("minecraft:item").itemStack;return {id:s.typeId,count:s.amount};})});
  if(t===40){
   if(read(c.base).states.triggered_bit)throw new Error("hopper lock powers source");
   inv(c.base).setItem(0,new ItemStack("minecraft:diamond",4));const target=inv(c.target);
   if(c.scenario.fill!=="empty")for(let i=0;i<target.size;i++)target.setItem(i,new ItemStack("minecraft:diamond",64-(c.scenario.fill==="partial"&&i===target.size-1?1:0)));
   c.changes.push({tick:system.currentTick,kind:"load"});
  }
  if(t===60||t===76){set(c.source,t===60?"redstone_block":"air");c.changes.push({tick:system.currentTick,kind:t===60?"power":"off"});}
 }
 if(t===128){for(const c of cells){if(c.samples[40].items[0]?.count!==4)throw new Error("source load control failed");emit(c);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(kind='dropper', face=face, target=target, fill=fill, phase=0)
             for target in ('barrel', 'chest', 'hopper', 'furnace', 'brewing_stand')
             for face in range(6) for fill in ('empty', 'full', 'partial')
             if target != 'brewing_stand' or fill == 'empty']
    cases += [dict(kind='dispenser', face=face, target='barrel', fill=fill, phase=0)
              for face in range(6) for fill in ('empty', 'full', 'partial')]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
