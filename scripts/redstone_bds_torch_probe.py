#!/usr/bin/env python3
"""Capture native torch self-loops, external toggling and burnout recovery."""

from redstone_bds_timing_probe import arguments,capture


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=["loop","break_loop","touch_support","external_fast","external_slow","isolated"].map(name=>({name}));
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function read(x,y,z=0) {
    const b=dimension.getBlock({x,y,z});
    if(!b)throw new Error("sampled chunk not loaded");
    return {id:b.typeId,states:b.permutation.getAllStates()};
}
function change(pos,block){cmd(`setblock ${pos.join(" ")} ${block}`);changes.push({tick:system.currentTick,pos,block});}
function setup(){
    const c=cases[index];cmd("fill -3 62 -3 4 69 3 air");
    cmd("setblock 0 64 0 stone");cmd("setblock 1 63 0 stone");
    if(["loop","break_loop","touch_support"].includes(c.name)){
        cmd("setblock 1 65 0 stone");cmd("setblock 1 66 0 redstone_wire");cmd("setblock 0 65 0 redstone_wire");
    }
    cmd('setblock 1 64 0 redstone_torch ["torch_facing_direction"="west"]');
    start=system.currentTick;c.phase=start&1;samples=[];changes=[{tick:start,kind:"setup"}];
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-3,y:64,z:-3})||!dimension.getBlock({x:4,y:64,z:3})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }
            emit({ready:true});index=0;setup();return;
        }
        const c=cases[index],t=system.currentTick-start;
        samples.push({tick:system.currentTick,torch:read(1,64),top:read(1,66),side:read(0,65)});
        if(c.name==="break_loop"){
            if(t===128)change([0,65,0],"air");if(t===160)change([0,65,0],"redstone_wire");
        }
        if(c.name==="touch_support"){
            if(t===128)change([0,64,-1],"stone");if(t===160)change([0,64,-1],"air");
        }
        if(c.name.startsWith("external_")){
            const period=c.name==="external_fast"?2:4;
            if(t>=24&&t<224&&(t-24)%period===0)change([-1,64,0],((t-24)/period)&1?"air":"redstone_block");
            if(t===224)change([-1,64,0],"air");
        }
        if(t>=300){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__=="__main__":
    capture(arguments(),SCRIPT,6)
