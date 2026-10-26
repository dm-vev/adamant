#!/usr/bin/env python3
"""Capture settled book/record analog outputs on the original Bedrock server.

Jukebox inputs use the native record-player API. Lectern and bookshelf inputs
are authored persistence states loaded by the native structure loader. Page
turning, book insertion, and bookshelf interaction timing are separate audits.
"""

import json

from redstone_audit_structure import byte, integer, string, listing, one_block
from redstone_bds_timing_probe import arguments, capture


DISCS = ["13", "cat", "blocks", "chirp", "far", "mall", "mellohi", "stal", "strad", "ward", "11", "wait",
         "otherside", "pigstep", "5", "relic", "creator", "creator_music_box", "precipice", "tears", "lava_chicken", "bounce"]


def inputs():
    cases, structures = [], {}
    for disc in [None, *DISCS]:
        for paused in (False, True):
            cases.append({"block": "jukebox", "disc": disc, "paused": paused, "phase": 0})
    for book in ("writable_book", "written_book"):
        for count in (0, 1, 2, 3, 4, 5, 6, 7, 8, 15, 16, 49, 50):
            for page in range(max(1, count // 2)):
                name = f"audit/lectern_{book}_{count}_{page}"
                pages = [{"text": string(f"page {i}"), "photoname": string("")} for i in range(count)]
                stack = {"Name": string("minecraft:" + book), "Count": byte(1), "Damage": (2, 0),
                         "tag": (10, {"pages": listing(10, pages)})}
                structures[name] = one_block("minecraft:lectern", {
                    "minecraft:cardinal_direction": string("north"), "powered_bit": byte(0)}, {
                    "id": string("Lectern"), "hasBook": byte(1), "book": (10, stack),
                    "page": integer(page), "totalPages": integer(count),
                })
                cases.append({"block": "lectern", "book": book, "pages": count, "page": page, "structure": name, "phase": 0})
    cases.append({"block": "lectern", "book": None, "pages": 0, "page": 0, "phase": 0})
    for facing in range(4):
        for mask in (0, 63):
            for last in range(7):
                name = f"audit/bookshelf_{facing}_{mask}_{last}"
                books = [{"Name": string("minecraft:book" if mask else ""), "Count": byte(1 if mask else 0),
                          "Damage": (2, 0), "WasPickedUp": byte(0)} for _ in range(6)]
                structures[name] = one_block("minecraft:chiseled_bookshelf", {
                    "direction": integer(facing), "books_stored": integer(mask)}, {
                    "id": string("ChiseledBookshelf"), "Items": listing(10, books), "LastInteractedSlot": integer(last),
                })
                cases.append({"block": "chiseled_bookshelf", "facing": facing, "mask": mask, "last": last,
                              "structure": name, "phase": 0})
    return cases, structures


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES;
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x);
const block=(x,z)=>d.getBlock({x,y:65,z});
const read=(x,z)=>{const b=block(x,z);if(!b)throw new Error("unloaded sample");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
    if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 175 100 111 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
    if(!start){
        if(!d.getBlock({x:0,y:65,z:0})||!d.getBlock({x:174,y:65,z:110})){
            if(system.currentTick-load>200)throw new Error("ticking area failed to load");return;
        }
        if(system.currentTick&1)return;
        emit({ready:true});cmd("fill 0 64 0 175 64 111 stone");cmd("fill 0 65 0 175 65 111 air");
        for(let i=0;i<cases.length;i++){
            const c=cases[i],x=(i%22)*8+2,z=Math.floor(i/22)*8+2;
            if(z>110)throw new Error("case grid out of bounds");
            if(c.structure){const result=cmd(`structure load ${c.structure.replace("/",":")} ${x-1} 65 ${z}`);if(!result.successCount)throw new Error("structure load failed "+c.structure);}
            else cmd(`setblock ${x-1} 65 ${z} ${c.block}`);
            let input;
            if(c.block==="jukebox"){
                const rp=block(x-1,z).getComponent("minecraft:record_player");
                if(c.disc)rp.setRecord("minecraft:music_disc_"+c.disc);
                if(c.paused&&c.disc)rp.pauseRecord();
                input={disc:rp.getRecord()?.typeId??null,playing:rp.isPlaying()};
                if(input.disc!==(c.disc?"minecraft:music_disc_"+c.disc:null)||input.playing!==Boolean(c.disc&&!c.paused))throw new Error("record input mismatch");
            }else if(c.block==="lectern"){
                const it=block(x-1,z).getComponent("minecraft:inventory").container.getItem(0);
                input={book:it?.typeId??null,pages:it?.getComponent("minecraft:book")?.pageCount??0};
                if(input.book!==(c.book?"minecraft:"+c.book:null)||input.pages!==c.pages)throw new Error("book structure input mismatch "+JSON.stringify({c,input}));
            }else if(read(x-1,z).states.books_stored!==c.mask)throw new Error("bookshelf input mask mismatch");
            cmd(`setblock ${x} 65 ${z} unpowered_comparator ["minecraft:cardinal_direction"="west"]`);
            cmd(`setblock ${x+1} 65 ${z} redstone_wire`);
            cells.push({scenario:c,x,z,input,samples:[],changes:[{tick:system.currentTick,kind:"setup"}]});
        }
        start=system.currentTick;return;
    }
    for(const cell of cells)cell.samples.push({tick:system.currentTick,source:read(cell.x-1,cell.z),comparator:read(cell.x,cell.z),wire:read(cell.x+1,cell.z)});
    if(system.currentTick-start>=16){for(const cell of cells)emit(cell);emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    cases, structures = inputs()
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(cases)), len(cases), structures=structures)
