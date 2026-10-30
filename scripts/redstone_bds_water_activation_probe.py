#!/usr/bin/env python3
"""Separate static command water, dynamic placement and real neighbour activation."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;cmd("tickingarea add 0 0 0 127 100 63 audit true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,63])){if(system.currentTick-loaded>480)throw new Error("water chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%4*32,65,8+Math.floor(i/4)*16],source=[base[0]-1,65,base[2]],trigger=[base[0],67,base[2]];
   cmd(`fill ${base[0]-5} 62 ${base[2]-5} ${base[0]+5} 70 ${base[2]+5} air`);cmd(`setblock ${base[0]} 63 ${base[2]} stone`);
   cmd(`setblock ${base.join(" ")} noteblock`);if(scenario.update==="remove")cmd(`setblock ${trigger.join(" ")} stone`);
   let accepted=false,error=null;try{accepted=cmd(`setblock ${base[0]} 66 ${base[2]} ${scenario.kind}`).successCount>0;}catch(e){error=String(e);}
   cells.push({scenario,base,source,trigger,accepted,placement_error:error,setup_tick:system.currentTick,changes:[],samples:[]});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const positions=[[0,1,0],[0,2,0],[-1,1,0],[1,1,0],[0,1,-1],[0,1,1],[-1,0,0],[1,0,0],[0,0,-1],[0,0,1],[0,0,0]];
  c.samples.push({tick:system.currentTick,cells:positions.map(v=>{const p=c.base.map((x,j)=>x+v[j]);return {position:p,block:state(p)};})});
  if(t===20+c.scenario.phase){if(c.scenario.update!=="none")cmd(`setblock ${c.trigger.join(" ")} ${c.scenario.update==="place"?"stone":"air"}`);
   c.changes.push({tick:system.currentTick,kind:"neighbour"});}
  if(t===80+c.scenario.phase||t===96+c.scenario.phase){const powered=t===80+c.scenario.phase;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,kind:"power",powered});}
 }
 if(t===128){for(const c of cells){if(c.scenario.kind==="water"&&!c.accepted)throw new Error("static water control rejected");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    cases=[dict(kind=kind,update=update,phase=phase) for kind in ('water','flowing_water')
           for update in ('none','place','remove') for phase in (0,1)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
