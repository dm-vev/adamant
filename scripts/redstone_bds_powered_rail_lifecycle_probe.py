#!/usr/bin/env python3
"""Capture rail flag/axis edits, replacement, removal and repowering."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,load=0,cells=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const read=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded rail lifecycle cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const place=(c,p,powered,dir)=>set(p,`${c.scenario.block} ["rail_direction"=${dir},"rail_data_bit"=${powered}]`);
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 127 100 63 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){if(!d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:127,y:65,z:63})){if(system.currentTick-load>240)throw new Error("rail lifecycle load timeout");return;}if(system.currentTick&1)return;start=system.currentTick;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],x=3+i%8*16,z=3+Math.floor(i/8)*16;
   cmd(`fill ${x-3} 63 ${z-3} ${x+6} 68 ${z+3} air`);cmd(`fill ${x-3} 64 ${z-3} ${x+6} 64 ${z+3} stone`);
   const c={scenario,setup_tick:start,source:[x-1,65,z],positions:[[x,65,z],[x+1,65,z],[x+2,65,z]],samples:[],changes:[]};
   for(const p of c.positions)place(c,p,scenario.powered,1);cells.push(c);
  }return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const phase=c.scenario.phase,target=c.positions[1];c.samples.push({tick:system.currentTick,source:read(c.source),rails:c.positions.map(read)});
  if([24,56,88,104].some(age=>t===age+phase)){const powered=t===24+phase||t===88+phase;set(c.source,powered?"redstone_block":"air");c.changes.push({tick:system.currentTick,kind:"power",powered});}
  if(t===40+phase){
   switch(c.scenario.mode){
    case "flag":place(c,target,false,1);break;
    case "axis":place(c,target,true,0);break;
    case "replace":set(target,"air");place(c,target,false,0);break;
    case "remove":set(target,"air");break;
   }c.changes.push({tick:system.currentTick,kind:"edit"});
  }
  if(t===72+phase){set(target,"air");place(c,target,false,1);c.changes.push({tick:system.currentTick,kind:"reset"});}
 }
 if(t===132){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block=block,mode=mode,powered=power,phase=phase)
             for block in ('golden_rail','activator_rail') for mode in ('flag','axis','replace','remove')
             for power in (False,True) for phase in (0,1)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
