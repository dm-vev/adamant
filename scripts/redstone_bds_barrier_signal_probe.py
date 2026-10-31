#!/usr/bin/env python3
"""Observe barriers under real dust-strength changes and solid/glass inputs."""
import json

from redstone_bds_timing_probe import arguments, capture


def signal_cases():
    kinds = [(k, 'door') for k in ('wooden_door', 'copper_door', 'iron_door')]
    kinds += [(k, 'trapdoor') for k in ('trapdoor', 'copper_trapdoor', 'iron_trapdoor')]
    kinds += [(k, 'gate') for k in ('fence_gate', 'crimson_fence_gate')]
    return [dict(kind=k, form=f, initial=i, height=h, path=path, phase=phase)
            for k, f in kinds for i in (False, True) for h in (0, 1)
            for path in ('wire', 'solid', 'glass') for phase in range(2)]


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
function set(c,p,id,record=true){cmd(`setblock ${p.join(" ")} ${id}`);if(record)c.changes.push({tick:system.currentTick,position:p,block:state(p)});}
function source(c,near,record=true){
 set(c,c.near,near?"redstone_block":"redstone_wire",record);
 set(c,c.far,near?"redstone_wire":"redstone_block",record);
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} barrier_signal_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,383])){if(system.currentTick-loaded>480)throw new Error("signal chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){const scenario=cases[i],base=[8+i%8*16,65,8+Math.floor(i/8)*16],y=65+scenario.height;
   cmd(`fill ${base[0]-7} 63 ${base[2]-3} ${base[0]+3} 69 ${base[2]+3} air`);cmd(`fill ${base[0]-7} 64 ${base[2]-3} ${base[0]+3} 64 ${base[2]+3} stone`);
   const inputPositions=[],first=scenario.path==="wire"?-1:-2;
   for(let x=-7;x<=first;x++){
    const below=[base[0]+x,y-1,base[2]],wire=[base[0]+x,y,base[2]];
    cmd(`setblock ${below.join(" ")} glass`);cmd(`setblock ${wire.join(" ")} redstone_wire`);inputPositions.push(below,wire);
   }
   const bridge=[base[0]-1,y,base[2]];
   if(scenario.path!=="wire"){cmd(`setblock ${bridge.join(" ")} ${scenario.path==="solid"?"stone":"glass"}`);inputPositions.push(bridge);}
   const c={scenario,base,near:[base[0]-4,y,base[2]],far:[base[0]-6,y,base[2]],changes:[]};source(c,true,false);
   cmd(`setblock ${base.join(" ")} ${definition(scenario,scenario.initial)}`);
   if(scenario.form==="door")cmd(`setblock ${base[0]} 66 ${base[2]} ${definition(scenario,scenario.initial,true)}`);
   c.positions=[base,[base[0],66,base[2]],[base[0]+first,y,base[2]],scenario.path==="wire"?[base[0]-1,y-1,base[2]]:bridge];
   c.setup_tick=system.currentTick;c.initial=c.positions.map(state);c.inputs=inputPositions.map(position=>({position,block:state(position)}));c.samples=[];cells.push(c);
  }ready=true;start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){c.samples.push({tick:system.currentTick,blocks:c.positions.map(state)});const q=t-c.scenario.phase;
  if([20,40,60,80,100].includes(q))source(c,[40,100].includes(q));
  if(q===70){set(c,c.near,"redstone_wire");set(c,c.far,"redstone_wire");}
  if([32,52,92].includes(q)){cmd(`setblock ${c.base.join(" ")} ${definition(c.scenario,false)}`);c.changes.push({tick:system.currentTick,position:c.base,block:state(c.base)});}
 }
 if(t===120){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = signal_cases()
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
