#!/usr/bin/env python3
"""Observe configured comparator direction, analog actor rotation and replacement."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const method of ["state","command","variant","replace"])for(const facing of ["north","east"])for(const subtract of [false,true])for(const phase of [0,1])cases.push({method,facing,subtract,phase,westBites:0,northBites:3});
// A larger new side analog input forces an internal lit/identifier transition
// after a bare rotation, independently of the later explicit replacements.
for(const method of ["state","command","variant","replace"])for(const phase of [0,1])cases.push({method,facing:"north",subtract:true,phase,westBites:3,northBites:0});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function comparator(block,facing){
    cmd(`setblock 0 65 0 ${block}`+(block==="air"?"":` ["minecraft:cardinal_direction"="${facing}","output_subtract_bit"=${cases[index].subtract}]`));
    changes.push({tick:system.currentTick,kind:"comparator",pos:[0,65,0],block,facing});
}
function rotate(facing){
    const c=cases[index];let block=read([0,65,0]).id.split(":")[1];
    if(c.method==="state"){
        const b=dimension.getBlock({x:0,y:65,z:0});b.setPermutation(b.permutation.withState("minecraft:cardinal_direction",facing));
        changes.push({tick:system.currentTick,kind:"state",pos:[0,65,0],block,facing});return;
    }
    if(c.method==="variant")block=block==="powered_comparator"?"unpowered_comparator":"powered_comparator";
    if(c.method==="replace")comparator("air",facing);
    comparator(block,facing);
}
function cake(pos,bites){
    const block=bites<0?"air":"cake";cmd(`setblock ${pos.join(" ")} ${block}`+(bites<0?"":` ["bite_counter"=${bites}]`));
    changes.push({tick:system.currentTick,kind:"cake",pos,block,bites});
}
function setup(){
    const c=cases[index];cmd("fill -3 63 -3 5 68 5 air");cmd("fill -2 64 -2 4 64 4 stone");
    cmd(`setblock -1 65 0 cake ["bite_counter"=${c.westBites}]`);cmd(`setblock 0 65 -1 cake ["bite_counter"=${c.northBites}]`);
    cmd(`setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west","output_subtract_bit"=${c.subtract}]`);
    for(let d=1;d<=3;d++){cmd(`setblock ${d} 65 0 redstone_wire`);cmd(`setblock 0 65 ${d} redstone_wire`);}
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-3,y:65,z:-3})||!dimension.getBlock({x:5,y:65,z:5})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting=true;}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,comparator:read([0,65,0]),east:[1,2,3].map(d=>read([d,65,0])),south:[1,2,3].map(d=>read([0,65,d])),west:read([-1,65,0]),north:read([0,65,-1])});
        if(t===16)rotate(c.facing);
        if(t===28)cake([0,65,-1],6);
        if(t===36)cake([-1,65,0],-1);
        if(t===40)cake([-1,65,0],3);
        if(t===48){const block=read([0,65,0]).id.endsWith(":powered_comparator")?"unpowered_comparator":"powered_comparator";comparator(block,c.facing);}
        if(t===56)rotate("west");
        if(t===64)comparator("air",c.facing);if(t===66)comparator("unpowered_comparator",c.facing);
        if(t>=80){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 40)
