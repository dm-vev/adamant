#!/usr/bin/env python3
"""Capture wire-network propagation from direct, observer and repeater sources."""

from redstone_bds_timing_probe import arguments,capture


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const source of ["block","observer","repeater"]) for(const output of ["lamp","repeater"]) {
    for(const width of [1,2,3,8]) for(const phase of [0,1])cases.push({source,output,width,phase});
}
let dimension,index=-1,start=0,on=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function read(x) {
    const b=dimension.getBlock({x,y:65,z:0});
    if(!b)throw new Error("sampled chunk not loaded");
    return {id:b.typeId,states:b.permutation.getAllStates()};
}
function setup() {
    const c=cases[index];
    cmd("fill -4 63 -2 6 69 2 air");cmd("fill -4 64 -2 6 64 2 stone");
    if(c.source==="observer")cmd('setblock -1 65 0 observer ["minecraft:facing_direction"="west"]');
    if(c.source==="repeater")cmd('setblock -1 65 0 unpowered_repeater ["minecraft:cardinal_direction"="west"]');
    for(let x=0;x<4;x++)cmd(`setblock ${x} 65 0 redstone_wire`);
    cmd("setblock 4 65 0 "+(c.output==="lamp"?"redstone_lamp":'unpowered_repeater ["minecraft:cardinal_direction"="west"]'));
    start=system.currentTick;on=24+((c.phase-((start+24)&1)+2)%2);samples=[];changes=[];
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){
            dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");
            cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;
        }
        if(index<0){
            if(!dimension.getBlock({x:-4,y:65,z:-2}) || !dimension.getBlock({x:6,y:65,z:2})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }
            emit({ready:true});index=0;setup();return;
        }
        const c=cases[index],t=system.currentTick-start;
        samples.push({tick:system.currentTick,source:read(-1),wires:[read(0),read(1),read(2),read(3)],output:read(4)});
        if(t===on || t===on+c.width){
            const x=c.source==="block"?-1:-2;
            const block=t===on?(c.source==="observer"?"stone":"redstone_block"):"air";
            cmd(`setblock ${x} 65 0 ${block}`);changes.push({tick:system.currentTick,pos:[x,65,0],block});
        }
        if(t>=on+c.width+28){
            emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}
            else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__=="__main__":
    capture(arguments(),SCRIPT,48)
