#!/usr/bin/env python3
"""Read back actual burned-out loops and strongly powered external controls."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const phase of [0,1])for(const name of ["support_corner","adjacent","above","below","wire_corner","break_loop","strong_fast","strong_medium","strong_slow"])
    cases.push({name,phase});
let dimension,index=-1,start=0,loadStarted=0,samples=[],changes=[],waiting=false;
const cmd=x=>dimension.runCommand(x);
function read(pos){const b=dimension.getBlock({x:pos[0],y:pos[1],z:pos[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};}
function change(pos,block){cmd(`setblock ${pos.join(" ")} ${block}`);changes.push({tick:system.currentTick,pos,block});}
function setup(){
    const c=cases[index];cmd("fill -4 62 -3 5 69 3 air");
    cmd("setblock 0 64 0 stone");cmd("setblock 1 63 0 stone");
    if(c.name.startsWith("strong_")){
        cmd("setblock -1 63 0 stone");
        cmd('setblock -1 64 0 unpowered_repeater ["minecraft:cardinal_direction"="west"]');
    }else{
        cmd("setblock 1 65 0 stone");cmd("setblock 1 66 0 redstone_wire");cmd("setblock 0 65 0 redstone_wire");
        if(c.name==="wire_corner"){cmd("setblock 2 63 0 stone");cmd("setblock 2 64 0 redstone_wire");}
    }
    cmd('setblock 1 64 0 redstone_torch ["torch_facing_direction"="west"]');
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-4,y:64,z:-3})||!dimension.getBlock({x:5,y:64,z:3})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }emit({ready:true});index=0;waiting=true;
        }
        const c=cases[index];
        if(waiting){if((system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,torch:read([1,64,0]),top:read([1,66,0]),side:read([0,65,0]),input:read([-1,64,0]),extra:read([2,64,0])});
        const pos={support_corner:[0,64,-1],adjacent:[2,64,0],above:[1,65,0],below:[1,63,0],wire_corner:[2,64,-1],break_loop:[0,65,0]}[c.name];
        // Above/below already contain stone: these are idempotent-command
        // controls, not evidence about replacing either support block.
        if(pos){
            if(t===96)change(pos,c.name==="break_loop"?"air":"stone");
            if(t===128)change(pos,c.name==="break_loop"?"redstone_wire":c.name==="above"||c.name==="below"?"stone":"air");
        }
        if(c.name.startsWith("strong_")){
            const period={strong_fast:2,strong_medium:4,strong_slow:8}[c.name];
            if(t>=24&&t<144&&(t-24)%period===0)change([-2,64,0],((t-24)/period)&1?"air":"redstone_block");
            if(t===144)change([-2,64,0],"air");
        }
        if(t>=180){emit({scenario:c,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting=true;
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 18)
