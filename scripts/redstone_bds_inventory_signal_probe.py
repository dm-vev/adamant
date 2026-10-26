#!/usr/bin/env python3
"""Capture native comparator fullness, stack limits and double-chest inputs."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const cases=[];
for(const block of ["chest","trapped_chest","barrel","hopper","dispenser","dropper","furnace","brewing_stand","double_chest"])
for(const pattern of ["empty","one_stone","one_egg","one_sword","partial_stone","half_stone","full_stone","mixed","boundary"])cases.push({block,pattern,phase:0});
let dimension,index=-1,start=0,loadStarted=0,preparedTick=0,samples=[],waiting="",inventory=[],initialInventory=[];
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function fill(container,pattern){
    for(let i=0;i<container.size;i++)container.setItem(i,undefined);
    if(pattern==="empty")return;
    if(pattern==="one_stone"||pattern==="one_egg"||pattern==="one_sword"){
        container.setItem(0,new ItemStack(pattern==="one_stone"?"minecraft:stone":pattern==="one_egg"?"minecraft:egg":"minecraft:wooden_sword",1));return;
    }
    if(pattern==="boundary"){
        let remaining=Math.max(1,Math.floor(container.size*64*5/14));
        for(let i=0;i<container.size&&remaining>0;i++){const n=Math.min(64,remaining);container.setItem(i,new ItemStack("minecraft:stone",n));remaining-=n;}return;
    }
    for(let i=0;i<container.size;i++){
        const item=pattern==="mixed"?["minecraft:stone","minecraft:egg","minecraft:wooden_sword"][i%3]:"minecraft:stone";
        const maximum=pattern==="mixed"?[64,16,1][i%3]:64;
        const count=pattern==="partial_stone"?17:pattern==="half_stone"?32:pattern==="full_stone"?64:Math.max(1,Math.floor(maximum/2));
        container.setItem(i,new ItemStack(item,count));
    }
}
function snapshot(container){const result=[];for(let i=0;i<container.size;i++){const item=container.getItem(i);result.push(item?{id:item.typeId,count:item.amount,maximum:item.maxAmount}:null);}return result;}
function prepare(){
    const c=cases[index];cmd("fill -4 63 -2 4 68 3 air");cmd("fill -3 64 -1 3 64 2 stone");
    cmd(`setblock -1 65 0 ${c.block==="double_chest"?'chest ["minecraft:cardinal_direction"="east"]':c.block}`);
    if(c.block==="double_chest")cmd('setblock -1 65 1 chest ["minecraft:cardinal_direction"="east"]');
    const b=dimension.getBlock({x:-1,y:65,z:0}),container=b.getComponent("minecraft:inventory")?.container;
    if(!container)throw new Error("no native container for "+c.block);
    if(c.block==="double_chest"&&container.size!==54)throw new Error("double chest did not pair");
    fill(container,c.pattern);
    // BDS consumes invalid items in brewing fuel slot 4 once per tick while
    // fuel remains zero. Leave that slot empty to isolate scalar fullness;
    // the initial and settled inventories below retain the actual inputs.
    if(c.block==="brewing_stand")container.setItem(4,undefined);
    initialInventory=snapshot(container);preparedTick=system.currentTick;waiting="settle";
}
function setup(){
    const container=dimension.getBlock({x:-1,y:65,z:0}).getComponent("minecraft:inventory")?.container;
    if(!container)throw new Error("container disappeared during preparation");inventory=snapshot(container);
    cmd('setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');cmd("setblock 1 65 0 redstone_wire");
    start=system.currentTick;samples=[];waiting="";
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){if(!dimension.getBlock({x:-4,y:65,z:-2})||!dimension.getBlock({x:4,y:65,z:3})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }emit({ready:true});index=0;waiting="prepare";}
        const c=cases[index];if(waiting){if((system.currentTick&1)===c.phase){if(waiting==="prepare")prepare();else if(system.currentTick-preparedTick>=8)setup();}return;}
        const container=dimension.getBlock({x:-1,y:65,z:0}).getComponent("minecraft:inventory")?.container;
        if(!container||JSON.stringify(snapshot(container))!==JSON.stringify(inventory))throw new Error("native inventory changed during capture: "+JSON.stringify(c));
        samples.push({tick:system.currentTick,source:read([-1,65,0]),comparator:read([0,65,0]),wire:read([1,65,0])});
        if(system.currentTick-start>=16){emit({scenario:c,input_mode:"Script API inventory readback",initial_inventory:initialInventory,prepared_tick:preparedTick,inventory,samples,changes:[{tick:start,kind:"setup"}]});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else waiting="prepare";
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 81)
