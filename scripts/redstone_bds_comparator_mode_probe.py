#!/usr/bin/env python3
"""Capture comparator mode setup versus bare permutation and identifier edits."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const method of ["state","command","variant","replace"])for(const subtract of [false,true])for(const bites of [0,3])for(const phase of [0,1])cases.push({method,subtract,bites,phase});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function comparator(block,subtract){
    cmd(`setblock 0 65 0 ${block}`+(block==="air"?"":` ["minecraft:cardinal_direction"="west","output_subtract_bit"=${subtract}]`));
    changes.push({tick:system.currentTick,kind:"comparator",pos:[0,65,0],block,subtract});
}
function mode(subtract){
    const c=cases[index];let block=read([0,65,0]).id.split(":")[1];
    if(c.method==="state"){
        const b=dimension.getBlock({x:0,y:65,z:0});b.setPermutation(b.permutation.withState("output_subtract_bit",subtract));
        changes.push({tick:system.currentTick,kind:"comparator",pos:[0,65,0],block,subtract});return;
    }
    if(c.method==="variant")block=block==="powered_comparator"?"unpowered_comparator":"powered_comparator";
    if(c.method==="replace")comparator("air",subtract);
    comparator(block,subtract);
}
function rear(bites){
    const block=bites<0?"air":"cake";cmd(`setblock -1 65 0 ${block}`+(bites<0?"":` ["bite_counter"=${bites}]`));
    changes.push({tick:system.currentTick,kind:"rear",pos:[-1,65,0],block,bites});
}
function setup(){
    const c=cases[index];cmd("fill -3 63 -2 5 68 4 air");cmd("fill -2 64 -1 4 64 3 stone");
    cmd(`setblock -1 65 0 cake ["bite_counter"=${c.bites}]`);
    cmd(`setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west","output_subtract_bit"=${c.subtract}]`);
    cmd("setblock 1 65 0 redstone_wire");cmd('setblock 2 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');cmd("setblock 3 65 0 redstone_wire");
    cmd("setblock 0 65 1 redstone_wire");cmd('setblock 0 65 2 unpowered_comparator ["minecraft:cardinal_direction"="south"]');cmd('setblock 0 65 3 cake ["bite_counter"=3]');
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-3,y:65,z:-2})||!dimension.getBlock({x:5,y:65,z:4})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting=true;}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,first:read([0,65,0]),wire:read([1,65,0]),second:read([2,65,0]),output:read([3,65,0]),side:read([0,65,1]),side_comparator:read([0,65,2]),rear:read([-1,65,0])});
        if(t===16)mode(!c.subtract);
        if(t===28)rear(c.bites===0?3:0);
        if(t===36)rear(-1);
        if(t===40)rear(c.bites);
        if(t===48)mode(c.subtract);
        if(t===56){const block=read([0,65,0]).id.endsWith(":powered_comparator")?"unpowered_comparator":"powered_comparator";comparator(block,c.subtract);}
        if(t===64)comparator("air",c.subtract);if(t===66)comparator("unpowered_comparator",!c.subtract);
        if(t>=80){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 32)
