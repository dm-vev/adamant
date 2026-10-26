#!/usr/bin/env python3
"""Capture comparator edits around electrical evaluation and publication."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const method of ["none","mode","rotation","variant","remove","replace"])for(const rising of [false,true])for(const phase of [0,1])for(const offset of [1,2])cases.push({method,rising,phase,offset});
let dimension,index=-1,start=0,edge=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function source(powered){cmd("setblock -1 65 0 "+(powered?"redstone_block":"air"));changes.push({tick:system.currentTick,kind:"source",powered});}
function comparator(block){cmd(`setblock 0 65 0 ${block}`+(block==="air"?"":' ["minecraft:cardinal_direction"="west"]'));changes.push({tick:system.currentTick,kind:"comparator",block});}
function edit(){
    const c=cases[index],b=dimension.getBlock({x:0,y:65,z:0});
    if(c.method==="none")return;
    if(c.method==="mode"||c.method==="rotation"){
        b.setPermutation(b.permutation.withState(c.method==="mode"?"output_subtract_bit":"minecraft:cardinal_direction",c.method==="mode"?true:"north"));
        changes.push({tick:system.currentTick,kind:c.method});return;
    }
    let block=b.typeId.split(":")[1];
    if(c.method==="variant")block=block==="powered_comparator"?"unpowered_comparator":"powered_comparator";
    if(c.method==="remove"||c.method==="replace")comparator("air");
    if(c.method!=="remove")comparator(block);
}
function setup(){
    const c=cases[index];cmd("fill -3 63 -3 5 68 4 air");cmd("fill -2 64 -2 4 64 3 stone");
    cmd('setblock 0 65 -1 cake ["bite_counter"=3]');
    if(!c.rising)cmd("setblock -1 65 0 redstone_block");
    cmd('setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');
    cmd("setblock 1 65 0 redstone_wire");cmd('setblock 2 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');cmd("setblock 3 65 0 redstone_wire");
    cmd("setblock 0 65 1 redstone_wire");cmd('setblock 0 65 2 unpowered_comparator ["minecraft:cardinal_direction"="south"]');cmd('setblock 0 65 3 cake ["bite_counter"=3]');
    start=system.currentTick;edge=16+((c.phase-((start+16)&1)+2)%2);samples=[];changes=[];
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-3,y:65,z:-3})||!dimension.getBlock({x:5,y:65,z:4})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;setup();return;}
        const c=cases[index],t=system.currentTick-start;
        samples.push({tick:system.currentTick,first:read([0,65,0]),wire:read([1,65,0]),second:read([2,65,0]),output:read([3,65,0]),side:read([0,65,1]),side_comparator:read([0,65,2]),rear:read([-1,65,0])});
        if(t===edge)source(c.rising);
        if(t===edge+c.offset)edit();
        if(t>=edge+20){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 48)
