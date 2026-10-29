#!/usr/bin/env python3
"""Capture TNT circuit priming from direct and strongly conducted short pulses."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=CASES,vectors=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const body=e=>({actor_id:e.id,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_tnt_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,70,0])||!get([255,70,191])){if(system.currentTick-loaded>480)throw new Error("TNT chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%12*20,70,8+Math.floor(i/12)*20],v=vectors[scenario.face],support=base.map((x,j)=>x+v[j]),source=base.map((x,j)=>x+v[j]*(scenario.conducted?2:1));
   cmd(`fill ${base[0]-4} 64 ${base[2]-4} ${base[0]+4} 76 ${base[2]+4} air`);
   cmd(`fill ${base[0]-4} 63 ${base[2]-4} ${base[0]+4} 63 ${base[2]+4} bedrock`);
   cmd(`fill ${base[0]-4} 69 ${base[2]-4} ${base[0]+4} 69 ${base[2]+4} stone`);
   cmd(`setblock ${base.join(" ")} tnt`);
   if(scenario.conducted)cmd(`setblock ${support.join(" ")} stone`);
   cells.push({scenario,base,support,source,setup_tick:system.currentTick,changes:[],samples:[]});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,block:state(c.base),source:state(c.source),
  tnt:d.getEntities({type:"minecraft:tnt",location:{x:c.base[0]+.5,y:c.base[1]+.5,z:c.base[2]+.5},maxDistance:12}).map(body)});
  if(t===40+c.scenario.phase||t===40+c.scenario.phase+c.scenario.width){const powered=t===40+c.scenario.phase;
   if(c.scenario.conducted){cmd(`setblock ${c.source.join(" ")} air`);cmd(`setblock ${c.source.join(" ")} stone_button ["facing_direction"=${c.scenario.face},"button_pressed_bit"=${powered}]`);}
   else cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);
   c.changes.push({tick:system.currentTick,powered});
  }
 }
 if(t===72){for(const c of cells){if(c.changes.length!==2||c.samples.some(s=>s.tnt.length>1))throw new Error("invalid native TNT pulse controls");
  if(c.scenario.width===8&&!c.samples.some(s=>s.tnt.length))throw new Error("native TNT positive control did not prime "+JSON.stringify(c.scenario));emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(face=face, conducted=conducted, phase=phase, width=width)
             for face in range(6) for conducted in (False, True)
             for phase in (0, 1) for width in (1, 2, 8)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
