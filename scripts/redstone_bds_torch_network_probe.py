#!/usr/bin/env python3
"""Capture directed torch chains/rings, placement order and external input."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const length of [2,3,4,5,6])for(const closed of [false,true])for(const reverse of [false,true])for(const phase of [0,1]){
    if(length===6&&!closed)continue;
    cases.push({length,closed,reverse,phase});
}
function layout(c){
    const torches=Array.from({length:c.length},(_,i)=>[i,65+i,0]),supports=[[0,64,0],[-1,63,0]],wires=[];
    for(const p of torches)supports.push([p[0],p[1]+1,p[2]]);
    if(c.closed){
        wires.push([c.length,65+c.length,0]);
        for(let k=1;k<=c.length;k++)wires.push([c.length,65+c.length-k,-k]);
        for(let x=c.length-1;x>=0;x--)wires.push([x,64,-c.length]);
        for(let z=-c.length+1;z<0;z++)wires.push([0,64,z]);
        for(const p of wires)supports.push([p[0],p[1]-1,p[2]]);
    }
    return {torches,supports,wires};
}
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false,blocks;
const cmd=x=>dimension.runCommand(x);
function read(p){const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};}
function setup(){
    const c=cases[index];blocks=layout(c);cmd("fill -4 61 -8 8 75 4 air");
    for(const p of blocks.supports)cmd(`setblock ${p.join(" ")} stone`);
    for(const p of blocks.wires)cmd(`setblock ${p.join(" ")} redstone_wire`);
    cmd('setblock -1 64 0 unpowered_repeater ["minecraft:cardinal_direction"="west"]');
    const order=Array.from({length:c.length},(_,i)=>i);if(c.reverse)order.reverse();
    for(const i of order)cmd(`setblock ${blocks.torches[i].join(" ")} redstone_torch ["torch_facing_direction"="${i===0?"top":"west"}"]`);
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-4,y:64,z:-8})||!dimension.getBlock({x:8,y:70,z:4})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }emit({ready:true});index=0;waiting=true;
        }
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,torches:blocks.torches.map(read),wires:blocks.wires.map(read),input:read([-1,64,0])});
        if(t===24||t===48){const block=t===24?"redstone_block":"air";cmd(`setblock -2 64 0 ${block}`);changes.push({tick:system.currentTick,pos:[-2,64,0],block});}
        if(t>=112){emit({scenario:c,layout:blocks,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 36)
