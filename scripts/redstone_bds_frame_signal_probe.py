#!/usr/bin/env python3
"""Capture native frame analog direction, rotation and conductor reads."""

import json

from redstone_audit_structure import byte, integer, string, one_block
from redstone_bds_timing_probe import arguments, capture


def inputs():
    cases, structures = [], {}
    for glowing in (False, True):
        for through in (False, True):
            for facing in range(6):
                # A west-facing frame directly behind this comparator would
                # require the comparator itself to be its support. Omit that
                # unsupported placement; the stone-backed case covers it.
                if not through and facing == 4:
                    continue
                for rotation in range(-1, 8):
                    block = "glow_frame" if glowing else "frame"
                    name = f"audit/{block}_{facing}_{rotation+1}"
                    actor = {"id": string("GlowItemFrame" if glowing else "ItemFrame"),
                             "ItemDropChance": (5, 1.0), "ItemRotation": byte(max(0, rotation))}
                    if rotation >= 0:
                        actor["Item"] = (10, {"Name": string("minecraft:stone"), "Count": byte(1), "Damage": (2, 0)})
                    structures[name] = one_block("minecraft:"+block, {"facing_direction": integer(facing),
                        "item_frame_map_bit": byte(0), "item_frame_photo_bit": byte(0)}, actor)
                    cases.append({"block": block, "through": through, "facing": facing, "rotation": rotation,
                                  "structure": name, "phase": 0})
    base = cases
    cases = []
    for query in (2, 3, 4, 5):
        # Rotate the tested geometry; authored actor rotations remain unchanged.
        for c in base:
            facing = c["facing"]
            if not c["through"]:
                # Base omitted face 4. Exchange it with the current query.
                if facing == query:
                    facing = 4
            name = f"audit/{c['block']}_{facing}_{c['rotation']+1}"
            cases.append({**c, "facing": facing, "query": query, "structure": name})
    return cases, structures


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
let d,load=0,start=0,cells=[],offset=0;
const cmd=x=>d.runCommand(x),block=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=block(x,65,z);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 175 100 79 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
if(!start){
    if(!block(0,65,0)||!block(174,65,78)){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
    emit({ready:true});cmd("fill 0 64 0 175 64 79 stone");cmd("fill 0 65 0 175 66 79 air");
    cells=[];
    for(let i=0;i<198&&offset+i<cases.length;i++){
        const c=cases[offset+i],x=i%22*8+3,z=Math.floor(i/22)*8+2,v=delta[c.facing],q=delta[c.query],distance=c.through?2:1,sx=x+q[0]*distance,sz=z+q[2]*distance,wx=x-q[0],wz=z-q[2];
        cmd(`setblock ${sx-v[0]} ${65-v[1]} ${sz-v[2]} stone`);
        if(c.through)cmd(`setblock ${x+q[0]} 65 ${z+q[2]} stone`);
        cmd(`setblock ${x} 65 ${z} unpowered_comparator ["minecraft:cardinal_direction"="${["down","up","north","south","west","east"][c.query]}"]`);cmd(`setblock ${wx} 65 ${wz} redstone_wire`);
        if(!cmd(`structure load ${c.structure.replace("/",":")} ${sx} 65 ${sz}`).successCount)throw new Error("frame structure failed");
        cells.push({scenario:c,x,z,sx,sz,wx,wz,samples:[],changes:[{tick:system.currentTick,kind:"setup"}]});
    }
    start=system.currentTick;return;
}
for(const c of cells){
    const source=read(c.sx,c.sz);if(source.id!=="minecraft:"+c.scenario.block)throw new Error("frame lost support "+JSON.stringify(c.scenario));
    c.samples.push({tick:system.currentTick,source,comparator:read(c.x,c.z),wire:read(c.wx,c.wz)});
}
if(system.currentTick-start>=16){
    for(const c of cells)emit(c);offset+=cells.length;
    if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;
}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    cases, structures = inputs()
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(cases)), len(cases), structures=structures)
