#!/usr/bin/env python3
"""Capture observer replacement/removal/rotation with a pending pulse."""

from redstone_bds_timing_probe import arguments,capture


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const action of ["replace","remove","rotate"])for(const offset of [1,3])for(const phase of [0,1])cases.push({name:action,offset,phase,facing:"west"});
let dimension,index=-1,start=0,on=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function observer(facing){cmd(`setblock 0 65 0 observer ["minecraft:facing_direction"="${facing}"]`);changes.push({tick:system.currentTick,pos:[0,65,0],block:"observer",facing});}
function air(){cmd("setblock 0 65 0 air");changes.push({tick:system.currentTick,pos:[0,65,0],block:"air"});}
function setup(){cmd("fill -3 63 -3 3 69 3 air");cmd('setblock 0 65 0 observer ["minecraft:facing_direction"="west"]');start=system.currentTick;on=24+((cases[index].phase-((start+24)&1)+2)%2);samples=[];changes=[];}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-3,y:65,z:-3})||!dimension.getBlock({x:3,y:65,z:3})){if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;}emit({ready:true});index=0;setup();return;}
        const c=cases[index],t=system.currentTick-start,b=dimension.getBlock({x:0,y:65,z:0});
        samples.push({tick:system.currentTick,observer:{id:b.typeId,states:b.permutation.getAllStates()}});
        if(t===on){cmd("setblock -1 65 0 stone");changes.push({tick:system.currentTick,pos:[-1,65,0],block:"stone"});}
        if(t===on+c.offset){if(c.name==="rotate")observer("east");else{air();if(c.name==="replace")observer("west");}}
        if(c.name==="remove"&&t===on+c.offset+1)observer("west");
        if(t>=on+16){emit({scenario:c,samples,changes});index++;if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else setup();}
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__=="__main__":
    capture(arguments(),SCRIPT,12)
