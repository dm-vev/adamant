#!/usr/bin/env python3
"""Capture native barrier consumer initialisation and source pulse timing."""
import json

from redstone_bds_timing_probe import arguments, capture


def barrier_cases():
    kinds = [(k, 'door') for k in ('wooden_door', 'copper_door', 'iron_door')]
    kinds += [(k, 'trapdoor') for k in ('trapdoor', 'copper_trapdoor', 'iron_trapdoor')]
    kinds += [(k, 'gate') for k in ('fence_gate', 'crimson_fence_gate')]
    return [dict(kind=k, form=f, initial=i, powered=p, feed=feed, phase=phase)
            for k, f in kinds for i in (False, True) for p in (False, True)
            for feed in ('lower', 'upper', 'split') for phase in range(2)]


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
function definition(c,open,top=false){
 if(c.form==="door")return `${c.kind} ["minecraft:cardinal_direction"="east","door_hinge_bit"=false,"upper_block_bit"=${top},"open_bit"=${top?false:open}]`;
 if(c.form==="trapdoor")return `${c.kind} ["direction"=0,"open_bit"=${open},"upside_down_bit"=false]`;
 return `${c.kind} ["minecraft:cardinal_direction"="south","open_bit"=${open},"in_wall_bit"=false]`;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} barrier_audit_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,383])){if(system.currentTick-loaded>480)throw new Error("barrier chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){const scenario=cases[i],base=[8+i%8*16,65,8+Math.floor(i/8)*16];
   cmd(`fill ${base[0]-3} 63 ${base[2]-3} ${base[0]+3} 69 ${base[2]+3} air`);cmd(`fill ${base[0]-3} 64 ${base[2]-3} ${base[0]+3} 64 ${base[2]+3} stone`);
   const positions=[base,[base[0],66,base[2]]],sources=scenario.feed==="lower"?[[base[0]-1,65,base[2]]]:scenario.feed==="upper"?[[base[0]-1,66,base[2]]]:[[base[0]-1,65,base[2]],[base[0]+1,66,base[2]]];
   for(const p of sources)cmd(`setblock ${p.join(" ")} ${scenario.powered?"redstone_block":"air"}`);
   cmd(`setblock ${base.join(" ")} ${definition(scenario,scenario.initial)}`);
   if(scenario.form==="door")cmd(`setblock ${positions[1].join(" ")} ${definition(scenario,scenario.initial,true)}`);
   cells.push({scenario,base,positions,sources,setup_tick:system.currentTick,initial:positions.map(state),initial_sources:sources.map(state),changes:[],samples:[]});
  }ready=true;start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,blocks:c.positions.map(state)});
  const q=t-c.scenario.phase;
  if([20,21,28,30,36,39,46,50,58,66,86,94].includes(q)){
   const invert=[20,28,36,46,58,86].includes(q),power=invert?!c.scenario.powered:c.scenario.powered;
   // Split feed retains the independent upper input while pulsing the lower.
   const p=c.sources[0],id=power?"redstone_block":"air";cmd(`setblock ${p.join(" ")} ${id}`);
   c.changes.push({tick:system.currentTick,position:p,block:state(p)});
  }
  if(q===74){cmd(`setblock ${c.base.join(" ")} ${definition(c.scenario,!state(c.base).states.open_bit)}`);c.changes.push({tick:system.currentTick,position:c.base,block:state(c.base)});}
 }
 if(t===108){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = barrier_cases()
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
