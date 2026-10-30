#!/usr/bin/env python3
"""Fill decorated pots through actual native hopper transfers, then read signals."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const patterns=["empty","one_stone","one_egg","one_sword","partial_stone","half_stone","full_stone","mixed","boundary"];
let dimension,preparedTick=0,loadStarted=0,cells=[];
const cmd=x=>dimension.runCommand(x);
const read=(x,y,z)=>{const b=dimension.getBlock({x,y,z});if(!b)throw new Error("sampled chunk not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function remaining(cell){const c=dimension.getBlock({x:cell.x-1,y:66,z:0}).getComponent("minecraft:inventory")?.container;if(!c)throw new Error("hopper disappeared");let count=0;for(let i=0;i<c.size;i++)count+=c.getItem(i)?.amount??0;return count;}
function prepare(){
    cmd("fill -3 63 -2 68 69 2 air");cmd("fill -2 64 -1 67 64 1 stone");
    for(let i=0;i<patterns.length;i++){
        const pattern=patterns[i],x=i*8,id=pattern==="one_egg"?"minecraft:egg":pattern==="one_sword"?"minecraft:wooden_sword":"minecraft:stone";
        const count=pattern==="empty"?0:pattern.startsWith("one_")?1:pattern==="partial_stone"?17:pattern==="full_stone"?64:pattern==="boundary"?Math.floor(64*5/14):32;
        cmd(`setblock ${x-1} 65 0 decorated_pot`);cmd(`setblock ${x-1} 66 0 hopper ["facing_direction"=0]`);
        const cell={x,pattern,id,count,maximum:new ItemStack(id,Math.max(1,count)).maxAmount,start:0,samples:[],done:false};cells.push(cell);
        const c=dimension.getBlock({x:x-1,y:66,z:0}).getComponent("minecraft:inventory")?.container;
        if(!c)throw new Error("no native hopper inventory");if(count)c.setItem(0,new ItemStack(id,count));
    }
    preparedTick=system.currentTick;
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 80 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");loadStarted=system.currentTick;return;}
        if(!preparedTick){if(!dimension.getBlock({x:-3,y:65,z:-2})||!dimension.getBlock({x:68,y:65,z:2})){
            if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
        }if(system.currentTick&1)return;emit({ready:true});prepare();return;}
        for(const cell of cells){
            if(cell.done)continue;
            const left=remaining(cell);
            if(left<0||left>cell.count)throw new Error("unexpected hopper contents");
            if(!cell.start){
                if(left!==0){if(system.currentTick-preparedTick>700)throw new Error("hopper failed to fill pot: "+cell.pattern);continue;}
                if(system.currentTick&1)continue;
                // Only the pot is below the hopper. With an empty intake and
                // no item entities, emptied hopper contents have entered it.
                const drops=dimension.getEntities({type:"minecraft:item",location:{x:cell.x-1,y:65,z:0},maxDistance:3});
                if(drops.length)throw new Error("item drops invalidate pot conservation");
                cmd(`setblock ${cell.x} 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]`);cmd(`setblock ${cell.x+1} 65 0 redstone_wire`);
                cell.start=system.currentTick;continue;
            }
            if(left!==0)throw new Error("hopper acquired unexpected items after filling");
            cell.samples.push({tick:system.currentTick,source:read(cell.x-1,65,0),comparator:read(cell.x,65,0),wire:read(cell.x+1,65,0)});
            if(system.currentTick-cell.start>=16){
                emit({scenario:{block:"decorated_pot",pattern:cell.pattern,phase:0},input_mode:"native hopper transfer conservation",input_count:cell.count,hopper_remaining:left,
                    inventory:[cell.count?{id:cell.id,count:cell.count,maximum:cell.maximum}:null],samples:cell.samples,changes:[{tick:cell.start,kind:"setup"}]});cell.done=true;
            }
        }
        if(cells.every(c=>c.done)){emit({complete:true,count:cells.length});system.clearRun(handle);}
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 9)
