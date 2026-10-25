#!/usr/bin/env python3
"""Capture settled comparator side-source filtering and analog side controls."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const source of ["none","redstone_block","torch","lever","wire","repeater","comparator","parallel_repeater","cake","cauldron","composter","powered_stone"])for(const subtract of [false,true])for(const side of [-1,1])cases.push({source,subtract,side,phase:0});
let dimension,index=-1,start=0,loadStarted=0,samples=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk is not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function setup(){
    const c=cases[index],z=c.side,side=z>0?"south":"north";
    cmd("fill -4 63 -4 4 68 4 air");cmd("fill -3 64 -3 3 64 3 stone");
    cmd('setblock -1 65 0 cake ["bite_counter"=3]');
    cmd(`setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west","output_subtract_bit"=${c.subtract}]`);
    cmd("setblock 1 65 0 redstone_wire");
    if(c.source==="redstone_block")cmd(`setblock 0 65 ${z} redstone_block`);
    if(c.source==="torch")cmd(`setblock 0 65 ${z} redstone_torch ["torch_facing_direction"="top"]`);
    if(c.source==="lever")cmd(`setblock 0 65 ${z} lever ["lever_direction"="up_north_south","open_bit"=true]`);
    if(c.source==="wire"){cmd(`setblock 0 65 ${z} redstone_wire`);cmd(`setblock 0 65 ${z*2} redstone_block`);}
    if(c.source==="repeater"||c.source==="comparator"){
        cmd(`setblock 0 65 ${z} unpowered_${c.source} ["minecraft:cardinal_direction"="${side}"]`);cmd(`setblock 0 65 ${z*2} redstone_block`);
    }
    if(c.source==="parallel_repeater"){
        cmd(`setblock 0 65 ${z} unpowered_repeater ["minecraft:cardinal_direction"="west"]`);cmd(`setblock -1 65 ${z} redstone_block`);
    }
    if(c.source==="cake")cmd(`setblock 0 65 ${z} cake ["bite_counter"=0]`);
    if(c.source==="cauldron")cmd(`setblock 0 65 ${z} cauldron ["fill_level"=6]`);
    if(c.source==="composter")cmd(`setblock 0 65 ${z} composter ["composter_fill_level"=8]`);
    if(c.source==="powered_stone"){
        cmd(`setblock 0 65 ${z} stone`);cmd(`setblock 0 65 ${z*2} lever ["lever_direction"="${side}","open_bit"=true]`);
    }
    start=system.currentTick;samples=[];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-4,y:65,z:-4})||!dimension.getBlock({x:4,y:65,z:4})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting=true;}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,comparator:read([0,65,0]),output:read([1,65,0]),side:read([0,65,c.side]),rear:read([-1,65,0])});
        if(t>=24){
            if(c.source==="none"&&read([1,65,0]).states.redstone_signal!==8)throw new Error("cake rear analog positive control failed");
            emit({scenario:c,samples,changes:[{tick:start,kind:"setup"}]});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 48)
