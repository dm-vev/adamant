#!/usr/bin/env python3
"""Read observer block state and its actual lamp, wire and repeater outputs."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const faces={down:[0,-1,0],up:[0,1,0],north:[0,0,-1],south:[0,0,1],west:[-1,0,0],east:[1,0,0]};
const cases=[];
for(const facing of Object.keys(faces)) for(const phase of [0,1]) cases.push({facing,phase,output:"lamp"});
for(const output of ["wire","repeater"]) for(const facing of ["north","south","west","east"]) {
    for(const phase of [0,1]) cases.push({facing,phase,output});
}
let dimension,index=-1,start=0,on=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function read(p) {
    const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});
    if(!b)throw new Error("sampled chunk not loaded");
    return {id:b.typeId,states:b.permutation.getAllStates()};
}
function setup() {
    const c=cases[index],d=faces[c.facing],p=[-d[0],65-d[1],-d[2]];
    cmd("fill -3 62 -3 3 69 3 air");
    cmd(`setblock 0 65 0 observer ["minecraft:facing_direction"="${c.facing}"]`);
    if(c.output!=="lamp")cmd(`setblock ${p[0]} ${p[1]-1} ${p[2]} stone`);
    let block=c.output==="lamp"?"redstone_lamp":"redstone_wire";
    if(c.output==="repeater")block=`unpowered_repeater ["minecraft:cardinal_direction"="${c.facing}"]`;
    cmd(`setblock ${p.join(" ")} ${block}`);
    start=system.currentTick;on=24+((c.phase-((start+24)&1)+2)%2);samples=[];changes=[];
}
const handle=system.runInterval(()=>{
    try {
        if(!dimension) {
            dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");
            cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;
        }
        if(index<0) {
            if(!dimension.getBlock({x:-3,y:65,z:-3}) || !dimension.getBlock({x:3,y:65,z:3})) {
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }
            emit({ready:true});index=0;setup();return;
        }
        const c=cases[index],d=faces[c.facing],t=system.currentTick-start;
        samples.push({tick:system.currentTick,observer:read([0,65,0]),output:read([-d[0],65-d[1],-d[2]])});
        if(t===on || t===on+12) {
            const p=[d[0],65+d[1],d[2]],block=t===on?"stone":"air";
            cmd(`setblock ${p.join(" ")} ${block}`);changes.push({tick:system.currentTick,pos:p,block});
        }
        if(t>=on+32) {
            emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}
            else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 28)
