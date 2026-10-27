#!/usr/bin/env python3
"""Read every original-BDS block type's native movable component at default state.

The stable Script API dispatches to the same BlockMovableComponent inspected
in the original-client piston walker. Authored blocks are changed and read in
one callback. Immediate survival/placement changes and per-type errors are
retained explicitly without attributing another block's component to that type.
This exports component data, not a full world push test.
"""

import argparse
import hashlib
import json
from pathlib import Path

from redstone_bds_probe import Reference


SCRIPT = r'''
import {world,system,BlockTypes,BlockPermutation} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_MOVABILITY "+JSON.stringify(x));
let d,load=0,types,index=0;
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;d.runCommand("tickingarea add 0 0 0 15 100 15 audit true");d.runCommand("gamerule randomtickspeed 0");d.runCommand("gamerule domobspawning false");return;}
 const b=d.getBlock({x:1,y:65,z:1});if(!b){if(system.currentTick-load>200)throw new Error("load timeout");return;}
 if(!types)types=BlockTypes.getAll().map(t=>t.id).sort();
 for(let n=0;n<32&&index<types.length;n++,index++){
  const name=types[index];
  try{
   const permutation=BlockPermutation.resolve(name),states=permutation.getAllStates();
   b.setPermutation(BlockPermutation.resolve("minecraft:air"));b.setPermutation(permutation);
   if(b.typeId!==name){emit({name,states,changed_to:b.typeId});continue;}
   const c=b.getComponent("minecraft:movable");
   emit({name,states:b.permutation.getAllStates(),movement:c?.movementType??null,sticky:c?.stickyType??null});
  }catch(e){emit({name,error:String(e)});}
 }
 if(index===types.length){emit({complete:true,count:index});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack,index});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30876)
    args = parser.parse_args()
    result = dict(complete=False, reference=__doc__, offline_reference=True,
                  binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), blocks=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=SCRIPT, local_client=True)
    try:
        while True:
            line = reference.until(lambda s: "ADAMANT_MOVABILITY " in s, timeout=45)[-1]
            record = json.loads(line.split("ADAMANT_MOVABILITY ", 1)[1])
            if record.get("error"):
                raise RuntimeError(record)
            if "name" in record:
                result["blocks"].append(record)
            if record.get("complete"):
                blocks = result["blocks"]
                if record["count"] != len(blocks) or len(blocks) < 1000 or len({b["name"] for b in blocks}) != len(blocks):
                    raise RuntimeError("native movable-component export incomplete")
                by_name = {b["name"]: b for b in blocks}
                for name, movement, sticky in (("stone", "PushPull", "None"), ("obsidian", "Immovable", "None"),
                                               ("torch", "Popped", "None"), ("slime", "PushPull", "Same"),
                                               ("honey_block", "PushPull", "Same")):
                    b = by_name["minecraft:" + name]
                    if (b["movement"], b["sticky"]) != (movement, sticky):
                        raise RuntimeError("native movement/sticky control failed: " + name)
                result["complete"] = True
                break
    finally:
        reference.stop()
        result["log_directory"] = str(reference.root)
        result["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(result, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps(dict(complete=result["complete"], blocks=len(result["blocks"]), logs=str(reference.root))))
