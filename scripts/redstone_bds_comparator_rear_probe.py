#!/usr/bin/env python3
"""Observe analog look-through and direct-power priority behind comparators."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const material of ["stone","redstone_block","glass","air","glowstone"])for(const bites of [0,3,6])for(const powered of [false,true])cases.push({material,bites,powered,phase:0});
let dimension,index=-1,start=0,loadStarted=0,samples=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function setup(){
    const c=cases[index];cmd("fill -4 63 -3 4 68 3 air");cmd("fill -3 64 -2 3 64 2 stone");
    cmd(`setblock -2 65 0 cake ["bite_counter"=${c.bites}]`);
    cmd(`setblock -1 65 0 ${c.material}`);
    // The repeater points south at the intervening block, independently of
    // the cake behind it. Its source is north of the repeater.
    cmd('setblock -1 65 -1 unpowered_repeater ["minecraft:cardinal_direction"="north"]');
    if(c.powered)cmd("setblock -1 65 -2 redstone_block");
    cmd('setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');
    cmd("setblock 1 65 0 redstone_wire");
    start=system.currentTick;samples=[];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-4,y:65,z:-3})||!dimension.getBlock({x:4,y:65,z:3})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting=true;}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        samples.push({tick:system.currentTick,comparator:read([0,65,0]),output:read([1,65,0]),near:read([-1,65,0]),far:read([-2,65,0]),input:read([-1,65,-1])});
        if(system.currentTick-start>=24){emit({scenario:c,samples,changes:[{tick:start,kind:"setup"}]});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 30)
