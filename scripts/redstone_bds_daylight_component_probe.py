#!/usr/bin/env python3
"""Read active/pending detector producers in an owned native BDS, without writes.

Only the authored Script API callback holds its own main-thread work briefly
after emitting a marker. No suspension, injection or process memory writes are
used. Native map descriptors are checked before/after each read.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct

from redstone_bds_probe import Reference
from redstone_bds_circuit_order_probe import Memory, inspect, torch_vtables
from redstone_native_probe import ClientPE

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_COMPONENT "+JSON.stringify(x)),positions=[[1,64,0],[0,64,-1],[-1,64,0],[0,64,1]],faces=["west","south","east","north"];
let d,load=0,prepared=false,locate=0,index=0,start=0,current=false;
const cmd=x=>d.runCommand(x),read=z=>{const b=d.getBlock({x:8,y:65,z});return {id:b.typeId,states:b.permutation.getAllStates()};};
function checkpoint(kind){emit({kind,tick:system.currentTick,phase:index,age:system.currentTick-start,blocks:[read(0),read(4)]});const until=Date.now()+750;while(Date.now()<until){}}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("time set 6000");cmd("scoreboard objectives add audit_gate dummy");cmd("scoreboard players set advance audit_gate 0");load=system.currentTick;return;}
 if(!prepared){
  if(!d.getBlock({x:-3,y:64,z:-3})||!d.getBlock({x:10,y:65,z:6})){if(system.currentTick-load>200)throw new Error("load timeout");return;}
  cmd("fill -3 61 -3 10 68 6 air");cmd("setblock 0 64 0 stone");
  for(const p of positions)cmd(`setblock ${p[0]} ${p[1]+1} ${p[2]} stone`);
  cmd("setblock 0 65 0 redstone_wire");cmd('setblock 0 63 0 lever ["lever_direction"="down_north_south","open_bit"=false]');
  for(let i=0;i<4;i++)cmd(`setblock ${positions[i].join(" ")} redstone_torch ["torch_facing_direction"="${faces[i]}"]`);
  locate=system.currentTick+40;prepared=true;return;
 }
 if(system.currentTick<locate)return;
 if(locate){emit({locate:true,positions});locate=0;return;}
 if(world.scoreboard.getObjective("audit_gate").getScore("advance")!==1)return;
 if(!current){
  if((system.currentTick&1)!==index)return;
  for(const z of [0,4]){cmd(`setblock 8 65 ${z} air`);cmd(`setblock 8 64 ${z} stone`);cmd(`setblock 8 65 ${z} daylight_detector ["redstone_signal"=${z===0?0:15}]`);}
  start=system.currentTick;current=true;checkpoint("setup");return;
 }
 checkpoint("tick");
 if(system.currentTick-start>=4){index++;current=false;if(index===2){emit({complete:true,count:2});system.clearRun(h);}}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def sample(memory, system):
    memory.live = True
    active_before = memory.read(system + 8, 0x40)
    pending_before = memory.read(system + 0xC8, 0x40)
    result = {"active": [], "pending": []}
    active_head, active_count = struct.unpack_from("<QQ", active_before, 8)
    if active_count > 256:
        raise RuntimeError("invalid active map count")
    p = struct.unpack("<Q", memory.read(active_head, 8))[0]
    active_visited = 0
    while p != active_head and active_visited <= active_count:
        entry = memory.read(p, 0x28)
        pos = struct.unpack_from("<iii", entry, 0x10)
        if pos in ((8, 65, 0), (8, 65, 4)):
            pointer = struct.unpack_from("<Q", entry, 0x20)[0]
            state = memory.read(pointer, 0x60)
            vtable = struct.unpack_from("<Q", state)[0]
            if not memory.base <= vtable < memory.base + memory.size:
                raise RuntimeError("producer vtable is outside the owned main module")
            result["active"].append({"pos": pos, "strength": struct.unpack_from("<h", state, 0x4E)[0],
                                     "removed": bool(state[0x55]), "base_type": hex(struct.unpack_from("<Q", state, 0x58)[0])})
        p = struct.unpack_from("<Q", entry)[0]
        active_visited += 1
    head, count = struct.unpack_from("<QQ", pending_before, 8)
    if count > 256:
        raise RuntimeError("invalid pending map count")
    p = struct.unpack("<Q", memory.read(head, 8))[0]
    visited = 0
    while p != head and visited <= count:
        entry = memory.read(p, 0x30)
        pos = struct.unpack_from("<iii", entry, 0x10)
        if pos in ((8, 65, 0), (8, 65, 4)):
            pointer = struct.unpack_from("<Q", entry, 0x28)[0]
            result["pending"].append({"pos": pos, "strength": struct.unpack("<h", memory.read(pointer + 0x4E, 2))[0]})
        p = struct.unpack_from("<Q", entry)[0]
        visited += 1
    if active_visited != active_count or visited != count or memory.read(system + 8, 0x40) != active_before or memory.read(system + 0xC8, 0x40) != pending_before:
        raise RuntimeError("component maps changed during checkpoint read")
    return result


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "client", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30861)
    args = parser.parse_args()
    vtables, locator = torch_vtables(ClientPE(args.binary), ClientPE(args.client))
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=SCRIPT)
    result = {"complete": False, "reference": "read-only producer map checkpoints in owned unmodified Windows BDS",
              "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(), "locator": locator, "checkpoints": []}
    memory = None
    try:
        while True:
            lines = reference.until(lambda s: "ADAMANT_COMPONENT " in s, timeout=45)
            record = json.loads(lines[-1].split("ADAMANT_COMPONENT ", 1)[1].strip())
            if record.get("error"):
                raise RuntimeError(record)
            if record.get("locate"):
                memory = Memory(reference.process)
                inspect(memory, vtables, {tuple(p) for p in record["positions"]})
                memory.live = True
                reference.process.stdin.write("scoreboard players set advance audit_gate 1\n")
                reference.process.stdin.flush()
            if "kind" in record:
                result["checkpoints"].append({**record, **sample(memory, memory.system_address)})
                print(json.dumps(result["checkpoints"][-1]), flush=True)
            if record.get("complete"):
                if record["count"] != 2 or len(result["checkpoints"]) != 10:
                    raise RuntimeError("incomplete producer checkpoints")
                result["complete"] = True
                break
    finally:
        if memory:
            memory.close()
        reference.stop()
        result["log_directory"] = str(reference.root)
        result["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(result, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "complete": result["complete"], "logs": str(reference.root)}), flush=True)
