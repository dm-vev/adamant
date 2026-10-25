#!/usr/bin/env python3
"""Capture observer activation after ordinary and sticky piston movement."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for (const facing of ["down","up","north","south","west","east"]) {
    for (const sticky of [false,true]) cases.push({facing,sticky,phase:0});
}
let dimension,index=-1,start=0,on=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function read(x) {
    const b=dimension.getBlock({x,y:65,z:0});
    if (!b) throw new Error("sampled chunk not loaded");
    return {id:b.typeId,states:b.permutation.getAllStates()};
}
function setup() {
    const c=cases[index];
    cmd("fill -5 63 -3 3 69 3 air");
    cmd("fill -5 64 -3 3 64 3 stone");
    cmd(`setblock -2 65 0 ${c.sticky?"sticky_piston":"piston"} ["facing_direction"=4]`);
    cmd(`setblock -1 65 0 observer ["minecraft:facing_direction"="${c.facing}"]`);
    start=system.currentTick;on=24+((c.phase-((start+24)&1)+2)%2);samples=[];changes=[];
}
const handle=system.runInterval(()=>{
    try {
        if (!dimension) {
            dimension=world.getDimension("overworld");
            cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");
            cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;
        }
        if(index<0) {
            if (!dimension.getBlock({x:-5,y:65,z:-3}) || !dimension.getBlock({x:3,y:65,z:3})) {
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }
            emit({ready:true});index=0;setup();return;
        }
        const c=cases[index],t=system.currentTick-start;
        samples.push({t,tick:system.currentTick,source:read(-1),destination:read(0),piston:read(-2)});
        if (t===on || t===on+26) {
            const block=t===on?"redstone_block":"air";
            cmd("setblock -3 65 0 "+block);changes.push({tick:system.currentTick,block});
        }
        if(t>=on+54) {
            if(!samples.some(s=>s.destination.id==="minecraft:observer"))throw new Error("piston positive control failed");
            emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}
            else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 12)
