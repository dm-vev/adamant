#!/usr/bin/env python3
"""Capture native circular-reference ordering for torches sharing a support."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const faces={east:[1,0,0],west:[-1,0,0],north:[0,0,-1],south:[0,0,1]};
const opposite={east:"west",west:"east",north:"south",south:"north"};
const cases=[];
for(const sides of [["east","west"],["east","north"],["east","north","west"],["east","north","west","south"]])for(const reverse of [false,true])for(const phase of [0,1])cases.push({sides,reverse,phase});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false,positions;
const cmd=x=>dimension.runCommand(x);
function read(p){const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};}
function setup(){
    const c=cases[index];positions=c.sides.map(s=>[faces[s][0],64,faces[s][2]]);
    cmd("fill -3 61 -3 3 68 3 air");cmd("setblock 0 64 0 stone");
    for(const p of positions)cmd(`setblock ${p[0]} ${p[1]+1} ${p[2]} stone`);
    cmd("setblock 0 65 0 redstone_wire");
    cmd('setblock 0 63 0 lever ["lever_direction"="down_north_south","open_bit"=false]');
    const order=Array.from({length:c.sides.length},(_,i)=>i);if(c.reverse)order.reverse();
    for(const i of order)cmd(`setblock ${positions[i].join(" ")} redstone_torch ["torch_facing_direction"="${opposite[c.sides[i]]}"]`);
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-3,y:64,z:-3})||!dimension.getBlock({x:3,y:64,z:3})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }emit({ready:true});index=0;waiting=true;
        }
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,torches:positions.map(read),wire:read([0,65,0]),input:read([0,63,0])});
        if(t===24||t===48){const powered=t===24;cmd(`setblock 0 63 0 lever ["lever_direction"="down_north_south","open_bit"=${powered}]`);changes.push({tick:system.currentTick,pos:[0,63,0],powered});}
        if(t>=112){emit({scenario:c,positions,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 16)
