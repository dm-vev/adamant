#!/usr/bin/env python3
"""Observe piston-head survival and queued checks on the unmodified BDS."""

import argparse
import itertools
import json
from pathlib import Path

from redstone_bds_timing_probe import capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),cases=CASES;
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
let d,load=0,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const side=(p,f)=>p.map((v,i)=>v+delta[f][i]);
const set=(p,id,face)=>cmd(`setblock ${p.join(" ")} ${id}`+(face===undefined?"":` ["facing_direction"=${nativeFace[face]}]`));
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded head cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const actor=p=>{const a=get(p).getComponent("minecraft:piston");return a?{state:a.state,moving:a.isMoving,attached:a.getAttachedBlocksLocations()}:null;};
const snapshot=c=>({base:read(c.base),head:read(c.head),source:read(c.source),notifier:read(c.notifier),actor:actor(c.base)});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 127 100 127 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){
  if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  if(system.currentTick&1)return;
  cells=[];
  for(let i=0;i<64&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],base=[4+(i%8)*16,65,4+Math.floor(i/8)*16],source=side(base,scenario.facing^1),head=side(base,scenario.facing),notifier=side(head,scenario.facing<2?2:1);
   cmd(`fill ${base[0]-4} 61 ${base[2]-4} ${base[0]+4} 69 ${base[2]+4} air`);
   set(base,scenario.sticky?"sticky_piston":"piston",scenario.facing);
   cells.push({scenario,base,source,head,notifier,setup_tick:system.currentTick,changes:[],samples:[],motion_seen:0,injection:null});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const a=actor(c.base),edge=40+c.scenario.phase;
  c.samples.push({tick:system.currentTick,...snapshot(c)});
  if(t===edge||t===edge+16){const powered=t===edge&&c.scenario.stage!=="retracted";set(c.source,powered?"redstone_block":"air");c.changes.push({tick:system.currentTick,powered});}
  const stage=c.scenario.stage;
  const ready=stage==="retracted"||stage==="expanded"?t===edge+10:a?.moving&&a.state.toLowerCase()===stage&&c.motion_seen++===c.scenario.offset;
  if(!c.injection&&ready){
   const edits=[],before=snapshot(c),edit=(pos,id,face)=>{set(pos,id,face);edits.push({pos,block:read(pos)});};
   const headID=c.scenario.sticky?"sticky_piston_arm_collision":"piston_arm_collision";
   if(stage==="retracted")edit(c.head,headID,c.scenario.facing);
   if(c.scenario.edit==="opposite_head")edit(c.head,c.scenario.sticky?"piston_arm_collision":"sticky_piston_arm_collision",c.scenario.facing);
   if(c.scenario.edit==="wrong_head_facing")edit(c.head,headID,c.scenario.facing^1);
   if(c.scenario.edit==="wrong_base_facing")edit(c.base,c.scenario.sticky?"sticky_piston":"piston",c.scenario.facing^1);
   if(c.scenario.edit==="repair_head"){edit(c.head,"air");edit(c.head,headID,c.scenario.facing);}
   edit(c.notifier,"stone");
   c.injection={tick:system.currentTick,before,edits,after:snapshot(c)};
  }
 }
 if(t===100){
  for(const c of cells){if(!c.injection||c.changes.length!==2)throw new Error("missing head control "+JSON.stringify(c.scenario));emit(c);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30877)
    args = parser.parse_args()
    args.offline_reference = True
    stages = [("retracted", 0), ("expanded", 0), ("expanding", 0),
              ("expanding", 1), ("retracting", 0), ("retracting", 1)]
    cases = [dict(sticky=s, facing=f, phase=p, stage=stage, offset=offset, edit=edit)
             for s, f, p, (stage, offset), edit in itertools.product(
                 (False, True), range(6), (0, 1), stages,
                 ("notify", "opposite_head", "wrong_head_facing", "wrong_base_facing", "repair_head"))]
    capture(args, SCRIPT.replace("CASES", json.dumps(cases)), len(cases))
