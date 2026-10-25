#!/usr/bin/env python3
"""Capture neighbour notifications before/at/after native reignition eligibility."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const phase of [0,1])for(const at of [32,48,64,66,96])cases.push({name:`reignite_${at}`,phase,at});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
function read(p){const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};}
function setup(){
    cmd("fill -3 62 -3 4 69 3 air");
    for(const p of [[0,64,0],[1,63,0],[1,65,0]])cmd(`setblock ${p.join(" ")} stone`);
    cmd("setblock 1 66 0 redstone_wire");cmd("setblock 0 65 0 redstone_wire");
    cmd('setblock 1 64 0 redstone_torch ["torch_facing_direction"="west"]');
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-3,y:64,z:-3})||!dimension.getBlock({x:4,y:64,z:3})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }emit({ready:true});index=0;waiting=true;
        }
        const c=cases[index];
        if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        samples.push({tick:system.currentTick,torch:read([1,64,0]),top:read([1,66,0]),side:read([0,65,0]),extra:read([2,64,0])});
        if(system.currentTick-start===c.at){cmd("setblock 2 64 0 stone");changes.push({tick:system.currentTick,pos:[2,64,0],block:"stone"});}
        if(system.currentTick-start>=180){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 10)
