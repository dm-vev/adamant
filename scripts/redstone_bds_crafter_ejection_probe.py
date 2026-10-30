#!/usr/bin/env python3
"""Capture native crafter world-ejection actor birth and subsequent motion."""
import json

from redstone_audit_structure import byte,string,listing,one_block
from redstone_bds_timing_probe import arguments,capture


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const vectors=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const body=e=>{const s=e.getComponent("minecraft:item").itemStack;return {actor_id:e.id,id:s.typeId,count:s.amount,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround};};
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();
 for(let i=0;i<cases.length;i++){
  const scenario=cases[i],base=[32+i%4*64,70,32+Math.floor(i/4)*64],v=vectors[scenario.face===4?5:4];
  const source=base.map((x,j)=>x+v[j]);
  cmd(`fill ${base[0]-8} 62 ${base[2]-8} ${base[0]+8} 78 ${base[2]+8} air`);
  cmd(`fill ${base[0]-8} 63 ${base[2]-8} ${base[0]+8} 63 ${base[2]+8} bedrock`);
  if(!cmd(`structure load audit:crafter_eject_${scenario.face} ${base.join(" ")}`).successCount)throw new Error("ejection grid load failed");
  cells.push({scenario,base,source,setup_tick:system.currentTick,changes:[],samples:[]});
 }start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_eject_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){for(let x=0;x<256;x+=16)for(let z=0;z<192;z+=16)if(!get([x,70,z])){
  if(system.currentTick-loaded>480)throw new Error("ejection chunk load timeout");return;
 }if(system.currentTick%20!==2)return;ready=true;setup();return;}
 const t=system.currentTick-start;
 for(const c of cells){const b=get(c.base);
  c.samples.push({tick:system.currentTick,block:{id:b.typeId,states:b.permutation.getAllStates()},
   drops:d.getEntities({type:"minecraft:item",location:{x:c.base[0]+.5,y:c.base[1]+.5,z:c.base[2]+.5},maxDistance:12}).map(body)});
  if(t===40+c.scenario.phase||t===56+c.scenario.phase){const powered=t===40+c.scenario.phase;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===100){for(const c of cells){const first=c.samples.find(s=>s.drops.length);
  if(!first||first.drops.length!==1||first.drops[0].id!=="minecraft:iron_block"||first.drops[0].count!==1)throw new Error("native ejection birth control differs");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def ejection_cases():
    orientations=('down_north','up_north','north_up','south_up','west_up','east_up')
    return [dict(face=face,phase=phase,orientation=orientation) for face,orientation in enumerate(orientations) for phase in (0,1)]


def ejection_structures():
    orientations=('down_north','up_north','north_up','south_up','west_up','east_up')
    stacks=[{'Slot':byte(i),'Name':string('minecraft:iron_ingot'),'Count':byte(1),'Damage':(2,0)} for i in range(9)]
    return {f'audit/crafter_eject_{face}':one_block('minecraft:crafter',
        {'orientation':string(orientation),'crafting':byte(0),'triggered_bit':byte(0)},
        {'id':string('Crafter'),'disabled_slots':(2,0),'Items':listing(10,stacks)}) for face,orientation in enumerate(orientations)}


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    cases=ejection_cases()
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases),structures=ejection_structures())
