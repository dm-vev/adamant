#!/usr/bin/env python3
"""Capture native powered/activator straight-rail range and pulse timing."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x);
const get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded powered-rail cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;const maxZ=Math.ceil(cases.length/8)*32-1;cmd(`tickingarea add 0 0 0 127 100 ${maxZ} audit_west true`);cmd(`tickingarea add 128 0 0 255 100 ${maxZ} audit_east true`);cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){
  for(let x=0;x<256;x+=16)for(let z=0;z<Math.ceil(cases.length/8)*32;z+=16)if(!get([x,65,z])){if(system.currentTick-load>240)throw new Error("rail area load timeout");return;}
  if(system.currentTick&1)return;start=system.currentTick;
  for(let i=0;i<cases.length;i++){
   const x=3+(i%8)*32,z=3+Math.floor(i/8)*32,c=cases[i],delta=c.axis==="x"?[1,0,0]:[0,0,1];
   cmd(`fill ${x-2} 64 ${z-2} ${x+18} 77 ${z+18} air`);cmd(`fill ${x-2} 64 ${z-2} ${x+18} 64 ${z+18} stone`);
   const slope=c.topology==="rise"?1:c.topology==="fall"?-1:0,baseY=slope<0?74:65;
   const rail_positions=Array.from({length:c.length},(_,n)=>[x+n*delta[0],baseY+n*slope,z+n*delta[2]]),input=[x-delta[0],65,z-delta[2]];
   const faceDelta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
   const source=c.face!==undefined?rail_positions[c.source_index].map((n,axis)=>n+faceDelta[c.face][axis]):c.topology==="wrong_axis"?[x+4*delta[0],baseY+1,z+4*delta[2]]:c.topology?[x,baseY+1,z]:(c.input&&c.input!=="direct")?[x-2*delta[0],65,z-2*delta[2]]:input;
   const rail_directions=rail_positions.map((_,n)=>c.topology==="wrong_axis"&&n===4?(c.axis==="x"?0:1):slope&&(slope>0?n<c.length-1:n>0)?(c.axis==="x"?(slope>0?2:3):(slope>0?5:4)):(c.axis==="x"?1:0));
   const rail_blocks=rail_positions.map((_,n)=>n===4&&c.middle_block?c.middle_block:c.block);
   const extra_rails=c.topology==="wrong_axis"?[-1,1].map(sign=>({position:[x+4*delta[0]+sign*delta[2],baseY,z+4*delta[2]+sign*delta[0]],direction:c.axis==="x"?0:1})):[];
   const supports=[];
   for(let n=0;n<rail_positions.length;n++){
    const p=rail_positions[n],down=[p[0],p[1]-1,p[2]];set(down,"stone");supports.push(down);
    if(rail_directions[n]>=2){const q=[p[0]+delta[0]*slope,p[1],p[2]+delta[2]*slope];set(q,"stone");supports.push(q);}
   }
   const cell={scenario:c,setup_tick:start,source,input:c.topology?null:source===input?null:input,rail_positions,rail_directions,rail_blocks,supports,extra_rails,samples:[],changes:[]};
   if(c.input==="wire")set(input,"redstone_wire");
   if(c.input==="repeater")set(input,`unpowered_repeater ["minecraft:cardinal_direction"="${c.axis==="x"?"west":"north"}"]`);
   for(const extra of extra_rails)set(extra.position,c.block+` ["rail_direction"=${extra.direction}]`);
   for(let n=0;n<rail_positions.length;n++)set(rail_positions[n],rail_blocks[n]+` ["rail_direction"=${rail_directions[n]}${n===4&&c.middle_powered?',"rail_data_bit"=true':''}]`);
   if(c.topology==="wrong_axis")set(rail_positions[4],c.block+` ["rail_direction"=${rail_directions[4]}]`);
   cell.source_off=read(source).id;
   if(!["minecraft:air","minecraft:stone"].includes(cell.source_off))throw new Error("rail input overwrites a non-support block");
   cells.push(cell);
  }return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.samples.push({tick:system.currentTick,source:read(c.source),input:c.input?read(c.input):null,rails:c.rail_positions.map(read)});
  if(t===1)for(let n=0;n<c.rail_positions.length;n++)if(read(c.rail_positions[n]).states.rail_direction!==c.rail_directions[n])throw new Error("native rail setup shape changed "+JSON.stringify({scenario:c.scenario,index:n,position:c.rail_positions[n],expected:c.rail_directions[n],actual:read(c.rail_positions[n])}));
  if(t===24+c.scenario.phase||t===40+c.scenario.phase){const powered=t===24+c.scenario.phase;set(c.source,powered?"redstone_block":c.source_off);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===80){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(block=block, axis=axis, length=length, phase=phase)
             for block in ('golden_rail', 'activator_rail') for axis in ('x', 'z')
             for length in (1, 8, 9, 16) for phase in (0, 1)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
