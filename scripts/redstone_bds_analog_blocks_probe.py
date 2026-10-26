#!/usr/bin/env python3
"""Capture settled scalar comparator outputs from native block permutations."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
const add=(block,states={})=>cases.push({block,states,phase:0});
for(const block of ["beehive","bee_nest"])for(let honey=0;honey<=5;honey++)add(block,{honey_level:honey});
for(const wax of ["","waxed_"])for(const oxidation of ["","exposed_","weathered_","oxidized_"])for(const lit of [false,true])add(wax+oxidation+"copper_bulb",{lit});
for(const liquid of ["water","lava","powder_snow"])for(let level=0;level<=6;level++)add("cauldron",{cauldron_liquid:liquid,fill_level:level});
for(let bites=0;bites<=6;bites++)add("cake",{bite_counter:bites});
add("candle_cake");
for(let level=0;level<=8;level++)add("composter",{composter_fill_level:level});
for(let charge=0;charge<=4;charge++)add("respawn_anchor",{respawn_anchor_charge:charge});
for(const eye of [false,true])add("end_portal_frame",{end_portal_eye_bit:eye});
for(const oxidation of ["","exposed_","weathered_","oxidized_"])add("waxed_"+oxidation+"copper_golem_statue");
let dimension,index=-1,start=0,loadStarted=0,samples=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function setup(){
    const c=cases[index];cmd("fill -3 63 -2 3 68 2 air");cmd("fill -2 64 -1 2 64 1 stone");
    const states=Object.entries(c.states).map(([k,v])=>JSON.stringify(k)+"="+JSON.stringify(v)).join(",");
    cmd(`setblock -1 65 0 ${c.block}`+(states?" ["+states+"]":""));
    cmd('setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');cmd("setblock 1 65 0 redstone_wire");
    start=system.currentTick;samples=[];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-3,y:65,z:-2})||!dimension.getBlock({x:3,y:65,z:2})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting=true;}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        samples.push({tick:system.currentTick,source:read([-1,65,0]),comparator:read([0,65,0]),wire:read([1,65,0])});
        if(system.currentTick-start>=16){emit({scenario:c,samples,changes:[{tick:start,kind:"setup"}]});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 77)
