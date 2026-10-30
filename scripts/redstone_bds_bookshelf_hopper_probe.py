#!/usr/bin/env python3
"""Observe actual native hopper transfers into/out of chiseled bookshelves."""

import json

from redstone_audit_structure import byte, integer, string, listing, one_block
from redstone_bds_timing_probe import arguments, capture


def inputs():
    cases, structures = [], {}
    for facing in range(4):
        for mode in ("insert", "extract"):
            for count in range(1, 7):
                mask = (1 << count)-1 if mode == "extract" else 0
                name = f"audit/shelf_{facing}_{mask}"
                items = [{"Name": string("minecraft:book" if mask & (1 << i) else ""),
                          "Count": byte(1 if mask & (1 << i) else 0), "Damage": (2, 0), "WasPickedUp": byte(0)} for i in range(6)]
                structures[name] = one_block("minecraft:chiseled_bookshelf", {"direction": integer(facing), "books_stored": integer(mask)}, {
                    "id": string("ChiseledBookshelf"), "Items": listing(10, items), "LastInteractedSlot": integer(0),
                })
                cases.append({"block": "chiseled_bookshelf", "mode": mode, "count": count, "facing": facing,
                              "structure": name, "mask": mask, "phase": 0})
    cases.extend({"block": "barrel", "mode": mode, "count": 6, "phase": 0} for mode in ("insert", "extract"))
    return cases, structures


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x),block=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=block(x,65,z);return {id:b.typeId,states:b.permutation.getAllStates()};};
const inv=(x,y,z)=>block(x,y,z).getComponent("minecraft:inventory").container;
function count(inv){let n=0;for(let i=0;i<inv.size;i++){const it=inv.getItem(i);if(it){if(it.typeId!=="minecraft:book")throw new Error("unexpected transfer item");n+=it.amount;}}return n;}
const h=system.runInterval(()=>{try{
if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 95 100 47 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
if(!start){
    if(!block(0,65,0)||!block(94,65,46)){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
    emit({ready:true});cmd("fill 0 63 0 95 63 47 stone");cmd("fill 0 64 0 95 64 47 stone");cmd("fill 0 65 0 95 66 47 air");
    for(let i=0;i<cases.length;i++){
        const c=cases[i],x=i%12*8+2,z=Math.floor(i/12)*8+2,y=c.mode==="insert"?66:64;
        if(c.structure){if(!cmd(`structure load ${c.structure.replace("/",":")} ${x-1} 65 ${z}`).successCount)throw new Error("structure failed");}
        else {cmd(`setblock ${x-1} 65 ${z} barrel`);if(c.mode==="extract")inv(x-1,65,z).setItem(0,new ItemStack("minecraft:book",c.count));}
        cmd(`setblock ${x-1} ${y} ${z} hopper ["facing_direction"=0]`);
        if(c.mode==="insert")inv(x-1,y,z).setItem(0,new ItemStack("minecraft:book",c.count));
        cmd(`setblock ${x} 65 ${z} unpowered_comparator ["minecraft:cardinal_direction"="west"]`);cmd(`setblock ${x+1} 65 ${z} redstone_wire`);
        cells.push({scenario:c,x,z,y,samples:[],changes:[{tick:system.currentTick,kind:"setup"}]});
    }
    start=system.currentTick;return;
}
for(const c of cells)c.samples.push({tick:system.currentTick,source:read(c.x-1,c.z),comparator:read(c.x,c.z),wire:read(c.x+1,c.z),hopper_count:count(inv(c.x-1,c.y,c.z))});
if(system.currentTick-start>=128){
    for(const c of cells){
        if(c.scenario.block==="barrel"&&c.samples.at(-1).hopper_count!==(c.scenario.mode==="insert"?0:c.scenario.count))throw new Error("native positive transfer control failed");
        emit(c);
    }
    emit({complete:true,count:cells.length});system.clearRun(h);
}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    cases, structures = inputs()
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(cases)), len(cases), structures=structures)
