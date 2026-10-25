#!/usr/bin/env python3
"""Check torch output and one-block conduction for all mounting/output faces."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const faces={down:[0,-1,0],up:[0,1,0],north:[0,0,-1],south:[0,0,1],west:[-1,0,0],east:[1,0,0]};
const cases=[];
for(const facing of ["down","north","south","west","east"])for(const output of Object.keys(faces))for(const conducted of [false,true])cases.push({facing,output,conducted});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[];
const cmd=x=>dimension.runCommand(x);
function read(pos){const b=dimension.getBlock({x:pos[0],y:pos[1],z:pos[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};}
function setup(){
    const c=cases[index],d=faces[c.facing],o=faces[c.output],n=c.conducted?2:1;
    cmd("fill -3 62 -3 3 69 3 air");
    cmd(`setblock ${d[0]} ${65+d[1]} ${d[2]} stone`);
    if(c.conducted)cmd(`setblock ${o[0]} ${65+o[1]} ${o[2]} stone`);
    cmd(`setblock ${o[0]*n} ${65+o[1]*n} ${o[2]*n} redstone_lamp`);
    cmd(`setblock 0 65 0 redstone_torch ["torch_facing_direction"="${c.facing==="down"?"top":c.facing}"]`);
    start=system.currentTick;c.phase=start&1;samples=[];changes=[{tick:start,kind:"setup"}];
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-3,y:65,z:-3})||!dimension.getBlock({x:3,y:65,z:3})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }emit({ready:true});index=0;setup();return;
        }
        const c=cases[index],o=faces[c.output],n=c.conducted?2:1;
        samples.push({tick:system.currentTick,torch:read([0,65,0]),output:read([o[0]*n,65+o[1]*n,o[2]*n]),conductor:read([o[0],65+o[1],o[2]])});
        if(system.currentTick-start>=20){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else setup();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 60)
