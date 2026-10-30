#!/usr/bin/env python3
"""Capture dispenser/dropper pulse cadence and all six direct/conducted faces."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const vectors=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,loaded=0,ready=false,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded pulse cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<12&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[32+i%4*64,65,32+Math.floor(i/4)*64],v=vectors[scenario.face],distance=scenario.conducted?2:1;
  cmd(`fill ${base[0]-8} 62 ${base[2]-8} ${base[0]+8} 70 ${base[2]+8} air`);
  cmd(`fill ${base[0]-8} 63 ${base[2]-8} ${base[0]+8} 63 ${base[2]+8} bedrock`);
  set(base,scenario.kind+' ["facing_direction"=2]');
  const source=base.map((x,j)=>x+v[j]*distance),support=base.map((x,j)=>x+v[j]);
  if(scenario.conducted)set(support,"stone");
  const on=system.currentTick+60+((scenario.phase-((system.currentTick+60)&1)+2)%2);
  cells.push({scenario,base,source,support,setup_tick:system.currentTick,on,changes:[],samples:[]});
 }start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<192;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${z+95} audit_pulses_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){for(let x=0;x<256;x+=16)for(let z=0;z<192;z+=16)if(!d.getBlock({x,y:65,z})){
  if(system.currentTick-loaded>480)throw new Error("pulse chunk load timeout");return;
 }ready=true;setup();return;}
 const t=system.currentTick-start;
 for(const c of cells){
  const inv=block(c.base).getComponent("minecraft:inventory")?.container;if(!inv)throw new Error("missing dispenser/dropper inventory");
  c.samples.push({tick:system.currentTick,block:read(c.base),items:inv.getItem(0)?.amount??0});
  if(t===40){
   if(c.scenario.conducted&&read(c.support).id!=="minecraft:stone")throw new Error("missing conductor");
   inv.setItem(0,new ItemStack("minecraft:diamond",64));c.load_tick=system.currentTick;
  }
  const edge=c.scenario.edges.indexOf(system.currentTick-c.on);
  if(edge>=0){const powered=(edge&1)===0;set(c.source,powered?"redstone_block":"air");c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===128){for(const c of cells){if(c.changes.length!==c.scenario.edges.length)throw new Error("missing pulse transition");emit(c);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(kind=kind, face=face, conducted=conducted, phase=phase, edges=edges)
             for kind in ('dispenser', 'dropper') for face in range(6)
             for conducted in (False, True) for phase in (0, 1)
             for edges in ([0, 1], [0, 2], [0, 4], [0, 4, 6, 10], [0, 4, 8, 12], [0, 4, 16, 20])]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
