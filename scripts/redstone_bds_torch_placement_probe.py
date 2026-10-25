#!/usr/bin/env python3
"""Observe torch initialization on pre-powered supports and later inversion.

BDS commands select the lit torch permutation; the disassembled client
TorchBlock::getPlacementBlock independently establishes that player placement
starts from the item's default block and only changes its facing. This probe
observes world setup/publication, not a player network interaction.
"""

import argparse
import hashlib
import json
from pathlib import Path

from redstone_bds_timing_probe import capture
from redstone_native_probe import ClientPE


INSPECTED = {
    "TorchBlock::getPlacementBlock": (0x402F6B0, 0x60C, "3e26512bb7818dda1d45dbffb5c7e4fc3eca5359b18d7a736557d60d9f2b86fd"),
    "RedstoneTorchBlock::setupRedstoneComponent initialization span":
        (0x995C16E, 0x3F, "1d92c6a7a30be268441929d93cf7f0242db779ccdeb3267d395fe29ea8a9267e"),
}


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const faces={top:[0,-1,0],north:[0,0,-1],south:[0,0,1],west:[-1,0,0],east:[1,0,0]};
const cases=[];
for(const face of Object.keys(faces))for(const mode of ["direct","block_behind_stone","lever"])for(const initiallyPowered of [false,true])for(const phase of [0,1])cases.push({face,mode,initiallyPowered,phase});
const torch=[0,65,0],lamp=[0,66,0];
let dimension,index=-1,start=0,preparedTick=0,loadStarted=0,samples=[],changes=[],waiting=false,support,source,placement;
const cmd=x=>dimension.runCommand(x);
const read=p=>{const b=dimension.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("sampled chunk is not loaded");return {id:b.typeId,states:b.permutation.getAllStates()};};
function input(powered){
    const c=cases[index];let block;
    if(c.mode==="lever"){
        const direction=c.face==="top"?"down_north_south":c.face;
        block="lever";cmd(`setblock ${source.join(" ")} lever ["lever_direction"="${direction}","open_bit"=${powered}]`);
    }else{
        block=powered?"redstone_block":c.mode==="direct"?"stone":"air";
        cmd(`setblock ${source.join(" ")} ${block}`);
    }
    return block;
}
function prepare(){
    const c=cases[index],f=faces[c.face];
    support=torch.map((v,i)=>v+f[i]);source=c.mode==="direct"?support:torch.map((v,i)=>v+2*f[i]);
    cmd("fill -4 61 -4 4 69 4 air");
    cmd(`setblock ${support.join(" ")} stone`);input(c.initiallyPowered);
    cmd(`setblock ${lamp.join(" ")} redstone_lamp`);
    preparedTick=system.currentTick;waiting=true;
}
function setup(){
    const c=cases[index];
    cmd(`setblock ${torch.join(" ")} redstone_torch ["torch_facing_direction"="${c.face}"]`);
    start=system.currentTick;samples=[];changes=[{tick:start,kind:"setup"}];waiting=false;
    placement={torch:read(torch),lamp:read(lamp),support:read(support),source:read(source)};
}
const handle=system.runInterval(()=>{
    try{
        if(!dimension){dimension=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");cmd("gamerule randomtickspeed 0");loadStarted=system.currentTick;return;}
        if(index<0){
            if(!dimension.getBlock({x:-4,y:65,z:-4})||!dimension.getBlock({x:4,y:65,z:4})){
                if(system.currentTick-loadStarted>200)throw new Error("ticking area failed to load");return;
            }
            emit({ready:true});index=0;prepare();return;
        }
        const c=cases[index];
        if(waiting){if(system.currentTick-preparedTick>=8&&(system.currentTick&1)===c.phase)setup();return;}
        const t=system.currentTick-start;
        samples.push({tick:system.currentTick,torch:read(torch),lamp:read(lamp),support:read(support),source:read(source)});
        if(t===8){const lit=read(torch).id==="minecraft:redstone_torch",expected=c.mode==="block_behind_stone"||!c.initiallyPowered;if(lit!==expected)throw new Error("initial support-power control failed");}
        if(t===12||t===24){const powered=t===12?!c.initiallyPowered:c.initiallyPowered;const block=input(powered);changes.push({tick:system.currentTick,pos:source,block,powered});}
        if(t>=40){emit({scenario:c,torch,lamp,support,source,prepared_tick:preparedTick,placement,samples,changes});index++;
            if(index===cases.length){emit({complete:true,count:cases.length});system.clearRun(handle);}else prepare();
        }
    }catch(error){emit({error:String(error),stack:error.stack});system.clearRun(handle);}
},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "client", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30861)
    args = parser.parse_args()
    client = ClientPE(args.client)
    evidence = {"method": "disassembly, not executed by this world probe", "client_sha256": hashlib.sha256(client.data).hexdigest()}
    for name, (rva, size, expected) in INSPECTED.items():
        actual = hashlib.sha256(client.read(rva, size)).hexdigest()
        if actual != expected:
            raise RuntimeError("client does not match inspected placement code: " + name)
        evidence[name] = {"rva": hex(rva), "size": hex(size), "sha256": actual}
    capture(args, SCRIPT, 60)
    result = json.loads(args.output.read_text(encoding="utf-8"))
    result["client_placement_evidence"] = evidence
    args.output.write_text(json.dumps(result, separators=(",", ":")) + "\n", encoding="utf-8")
