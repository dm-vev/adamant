#!/usr/bin/env python3
"""Observe original-BDS recovery when a moving piston is removed or replaced."""

import argparse
import itertools
import json
from pathlib import Path

from redstone_bds_timing_probe import capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),cases=CASES,target=TARGET;
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
let d,load=0,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const side=(p,f,n=1)=>p.map((v,i)=>v+delta[f][i]*n);
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const read=p=>{
 const b=get(p);if(!b)throw new Error("unloaded interruption cell");
 const r={id:b.typeId,states:b.permutation.getAllStates()},inv=b.getComponent("minecraft:inventory")?.container;
 if(inv){r.inventory=[];for(let slot=0;slot<inv.size;slot++){const item=inv.getItem(slot);if(item)r.inventory.push({slot,id:item.typeId,count:item.amount});}}
 return r;
};
const actor=p=>{const a=get(p).getComponent("minecraft:piston");return a?{state:a.state,moving:a.isMoving,attached:a.getAttachedBlocksLocations()}:null;};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 127 100 127 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){
  if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  if(system.currentTick&1)return;
  cells=[];
  for(let i=0;i<64&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],base=[4+(i%8)*16,65,4+Math.floor(i/8)*16],source=side(base,scenario.facing^1),front=[1,2,3,4].map(n=>side(base,scenario.facing,n));
   cmd(`fill ${base[0]-4} 60 ${base[2]-4} ${base[0]+4} 70 ${base[2]+4} air`);
   set(base,(scenario.sticky?"sticky_piston":"piston")+` ["facing_direction"=${nativeFace[scenario.facing]}]`);
   const count=scenario.payload==="chest"?1:3;
   for(let j=0;j<count;j++)set(front[j],scenario.payload);
   if(scenario.payload==="chest")get(front[0]).getComponent("minecraft:inventory").container.setItem(0,new ItemStack("minecraft:diamond",7));
   cells.push({scenario,base,source,front,setup_tick:system.currentTick,changes:[],samples:[],motion_seen:0,interruption:null});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const a=actor(c.base);
  c.samples.push({tick:system.currentTick,base:read(c.base),actor:a,source:read(c.source),front:c.front.map(read)});
  const edge=40+c.scenario.phase;
  if(t===edge||t===edge+16){const powered=t===edge;set(c.source,powered?"redstone_block":"air");c.changes.push({tick:system.currentTick,powered});}
  if(!c.interruption&&a?.moving&&a.state.toLowerCase()===c.scenario.motion){
   if(c.motion_seen++===c.scenario.offset){const position=target==="base"?c.base:c.front[0],before=read(position);set(position,c.scenario.replacement);c.interruption={tick:system.currentTick,position,before,previous_actor:a,after:{base:read(c.base),actor:actor(c.base),front:c.front.map(read)}};}
  }
 }
 if(t===100){
  for(const c of cells){
   if(!c.interruption||c.changes.length!==2||!c.samples.some(s=>s.actor?.moving))throw new Error("missing interruption control "+JSON.stringify(c.scenario));
   const after=c.samples.filter(s=>s.tick>c.interruption.tick);
   if(after.length<20||(target==="base"&&after.some(s=>s.base.id!=="minecraft:"+c.scenario.replacement||s.actor!==null)))throw new Error("native piston replacement did not persist");
   emit(c);
  }
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30876)
    parser.add_argument("--target", choices=("base", "head"), default="base")
    args = parser.parse_args()
    args.offline_reference = True
    cases = [dict(sticky=s, facing=f, phase=p, motion=m, offset=o, payload=b, replacement=r, target=args.target)
             for s, f, p, m, o, b, r in itertools.product(
                 (False, True), range(6), (0, 1), ("expanding", "retracting"),
                 (0, 1), ("stone", "chest"), ("air", "stone"))]
    capture(args, SCRIPT.replace("CASES", json.dumps(cases)).replace("TARGET", json.dumps(args.target)), len(cases))
