#!/usr/bin/env python3
"""Capture comparator pulse sampling, analog inputs and a two-diode chain."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const source of ["block","wire","cake"])for(const subtract of [false,true])for(const width of [1,2,3,4,8])for(const phase of [0,1])cases.push({source,subtract,width,phase});
let dimension,index=-1,start=0,on=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function input(powered){
    const c=cases[index],p=c.source==="wire"?[-2,65,0]:[-1,65,0],block=powered?c.source==="cake"?"cake":"redstone_block":"air";
    cmd(`setblock ${p.join(" ")} ${block}`);changes.push({tick:system.currentTick,pos:p,block,powered});
}
function setup(){
    const c=cases[index];cmd("fill -4 63 -2 6 69 4 air");cmd("fill -3 64 -1 5 64 3 stone");
    cmd(`setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west","output_subtract_bit"=${c.subtract}]`);
    cmd("setblock 1 65 0 redstone_wire");
    cmd('setblock 2 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');cmd("setblock 3 65 0 redstone_wire");
    cmd("setblock 0 65 1 redstone_wire");cmd('setblock 0 65 2 unpowered_comparator ["minecraft:cardinal_direction"="south"]');cmd('setblock 0 65 3 cake ["bite_counter"=3]');
    if(c.source==="wire")cmd("setblock -1 65 0 redstone_wire");
    start=system.currentTick;samples=[];changes=[];
    on=16+((c.phase-((start+16)&1)+2)%2);
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-4,y:65,z:-2})||!dimension.getBlock({x:6,y:65,z:4})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;setup();return;}
        const c=cases[index],t=system.currentTick-start;
        samples.push({tick:system.currentTick,first:read([0,65,0]),wire:read([1,65,0]),second:read([2,65,0]),output:read([3,65,0]),side:read([0,65,1]),side_comparator:read([0,65,2]),rear:read([-1,65,0])});
        if(t===on||t===on+c.width)input(t===on);
        if(t>=on+c.width+20){
            if(read([0,65,1]).states.redstone_signal!==8)throw new Error("side comparator/wire positive control failed");
            emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 60)
