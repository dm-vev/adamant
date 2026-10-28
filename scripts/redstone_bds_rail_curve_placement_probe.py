#!/usr/bin/env python3
"""Capture powered/activator placement beside an already connected rail curve."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],corners={6:[3,5],7:[3,4],8:[2,4],9:[2,5]};
let d,start=0,load=0,cells=[];
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const read=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded curve cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd(`tickingarea add 0 0 0 63 100 ${Math.ceil(cases.length/4)*16-1} audit true`);cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){if(!d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:63,y:65,z:Math.ceil(cases.length/4)*16-1})){if(system.currentTick-load>240)throw new Error("curve area load timeout");return;}if(system.currentTick&1)return;start=system.currentTick;
  for(let i=0;i<cases.length;i++){
   const c=cases[i],center=[3+i%4*16,65,3+Math.floor(i/4)*16];
   cmd(`fill ${center[0]-3} 63 ${center[2]-3} ${center[0]+3} 68 ${center[2]+3} air`);cmd(`fill ${center[0]-3} 64 ${center[2]-3} ${center[0]+3} 64 ${center[2]+3} stone`);
   const arms=corners[c.corner].map(face=>center.map((n,axis)=>n+delta[face][axis])),target=c.mode==="add_corner"?arms[1]:center.map((n,axis)=>n+delta[c.corner<8?2:3][axis]);
   for(let n=0;n<(c.mode==="add_corner"?1:arms.length);n++)set(arms[n],`rail ["rail_direction"=${corners[c.corner][n]>=4?1:0}]`);
   set(center,`rail ["rail_direction"=${c.mode==="add_corner"?0:c.corner}]`);
   if(read(center).states.rail_direction!==(c.mode==="add_corner"?0:c.corner))throw new Error("native curve setup mismatch");
   cells.push({scenario:c,setup_tick:start,center,arms,target,samples:[],changes:[]});
  }return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,blocks:[c.center,...c.arms,c.target].map(read)});
  if(t===24){set(c.target,c.scenario.block+` ["rail_direction"=${c.scenario.mode==="add_corner"?1:0}]`);c.changes.push({tick:system.currentTick,kind:"place"});}
 }
 if(t===64){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block=block,corner=corner,phase=0,mode=mode)
             for block in ('golden_rail','activator_rail') for corner in (6,7,8,9) for mode in ('preserve','add_corner')]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
