#!/usr/bin/env python3
"""Capture detector visible flag versus retained producer configuration."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x);
const read=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded detector lifecycle cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const place=(c,power,dir)=>set(c.source,`detector_rail ["rail_data_bit"=${power},"rail_direction"=${dir}]`);
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 63 100 63 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){
  if(!d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:63,y:65,z:63})){if(system.currentTick-load>240)throw new Error("native rail area load timeout");return;}if(system.currentTick&1)return;
  start=system.currentTick;
  for(let i=0;i<cases.length;i++){
   const x=3+(i%4)*16,z=3+Math.floor(i/4)*16;
   cmd(`fill ${x-3} 62 ${z-3} ${x+4} 69 ${z+3} air`);cmd(`fill ${x-3} 64 ${z-3} ${x+4} 64 ${z+3} stone`);
   const c={scenario:cases[i],source:[x,65,z],wire:[x+1,65,z],lamp:[x,63,z],setup_tick:start,changes:[{tick:start,kind:"setup"}],samples:[]};
   set(c.wire,"redstone_wire");set(c.lamp,"redstone_lamp");place(c,c.scenario.powered,0);cells.push(c);
  }return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.samples.push({tick:system.currentTick,source:read(c.source),wire:read(c.wire),lamp:read(c.lamp)});
  if(t===24+c.scenario.edge_phase){if(c.scenario.replace)set(c.source,"air");place(c,!c.scenario.powered,c.scenario.direction);c.changes.push({tick:system.currentTick,kind:"edit"});}
  if(t===48+c.scenario.edge_phase){set(c.source,"air");place(c,c.scenario.powered,0);c.changes.push({tick:system.currentTick,kind:"reset"});}
 }
 if(t===80){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(powered=power, replace=replace, edge_phase=phase, direction=direction, phase=0)
             for power in (False, True) for replace in (False, True)
             for phase in (0, 1) for direction in (0, 1)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
