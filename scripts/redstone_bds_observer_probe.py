#!/usr/bin/env python3
"""Read native observer pulses on all faces and at both circuit phases."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const faces={down:[0,-1,0],up:[0,1,0],north:[0,0,-1],south:[0,0,1],west:[-1,0,0],east:[1,0,0]};
const cases=[];
for (const facing of Object.keys(faces)) for (const phase of [0,1]) {
    cases.push({name:"front",facing,changed:facing,phase,gap:12});
}
for (const gap of [1,2,3,4,5,6,8]) for (const phase of [0,1]) {
    cases.push({name:"rapid",facing:"west",changed:"west",phase,gap});
}
for (const changed of Object.keys(faces)) for (const phase of [0,1]) {
    cases.push({name:"face_filter",facing:"west",changed,phase,gap:12});
}
let dimension,index=-1,start=0,on=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function read(x=0,y=65,z=0) {
    const b=dimension.getBlock({x,y,z});
    if (!b) throw new Error("sampled chunk not loaded");
    return {id:b.typeId,states:b.permutation.getAllStates()};
}
function setup() {
    const c=cases[index];
    cmd("fill -3 62 -3 3 69 3 air");
    cmd(`setblock 0 65 0 observer ["minecraft:facing_direction"="${c.facing}"]`);
    start=system.currentTick; on=24+((c.phase-((start+24)&1)+2)%2); samples=[];changes=[];
}
const handle=system.runInterval(()=>{
    try {
        if (!dimension) {
            dimension=world.getDimension("overworld");
            cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");
            cmd("gamerule randomtickspeed 0"); loadStarted=system.currentTick; return;
        }
        if (index<0) {
            if (!dimension.getBlock({x:-3,y:65,z:-3}) || !dimension.getBlock({x:3,y:65,z:3})) {
                if (system.currentTick-loadStarted>200) throw new Error("ticking area failed to load");return;
            }
            emit({ready:true}); index=0;setup();return;
        }
        const c=cases[index],t=system.currentTick-start;
        samples.push({t,tick:system.currentTick,observer:read()});
        if (t===on || t===on+c.gap) {
            const p=faces[c.changed],block=t===on?"stone":"air";
            cmd(`setblock ${p[0]} ${65+p[1]} ${p[2]} ${block}`);
            changes.push({tick:system.currentTick,pos:[p[0],65+p[1],p[2]],block});
        }
        if (t>=on+c.gap+20) {
            emit({scenario:c,samples,changes});index++;
            if (index===cases.length) {emit({complete:true,count:cases.length});system.clearRun(handle);}
            else setup();
        }
    } catch(error) {emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 38)
