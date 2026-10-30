#!/usr/bin/env python3
"""Enumerate native block permutations and read their movable components.

Permutations are constructed from the original BDS BlockStates domains and
canonicalized after each property, retaining native alias normalization. Reads
that immediately transform the authored block are recorded separately. This
does not simulate push/pull, support survival, actor state or entity movement.
"""

import argparse
import hashlib
import json
from pathlib import Path

from redstone_bds_probe import Reference


SCRIPT = r'''
import {world,system,BlockTypes,BlockStates,BlockPermutation} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_MOVABLE_STATES "+JSON.stringify(x));
const key=p=>JSON.stringify(Object.entries(p.getAllStates()).sort((a,b)=>a[0].localeCompare(b[0])));
let d,load=0,types,domains,typeIndex=0,index=0,permutations,record;
function prepare(){
 const name=types[typeIndex],initial=BlockPermutation.resolve(name);
 let states=[initial],rejected=[];
 for(const property of Object.keys(initial.getAllStates())){
  const values=domains.get(property);if(!values)throw new Error("missing state domain: "+property);
  const next=new Map();
  for(const p of states)for(const v of values){
   try{const q=p.withState(property,v);next.set(key(q),q);}
   catch(e){rejected.push({states:p.getAllStates(),property,value:v,error:String(e)});}
  }
  if(!next.size)throw new Error("native property cannot be enumerated: "+name+"/"+property);
  states=[...next.values()];
 }
 permutations=states;index=0;
 record={name,default_states:initial.getAllStates(),permutation_count:states.length,observed_count:0,classification:null,changed:[],exceptions:[],rejected};
 if(!states.length)throw new Error("empty native permutation enumeration");
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;d.runCommand("tickingarea add 0 0 0 15 100 15 audit true");d.runCommand("gamerule randomtickspeed 0");d.runCommand("gamerule domobspawning false");return;}
 const b=d.getBlock({x:1,y:65,z:1});if(!b){if(system.currentTick-load>200)throw new Error("load timeout");return;}
 if(!types){types=BlockTypes.getAll().map(t=>t.id).sort();const definitions=JSON.parse(JSON.stringify(BlockStates.getAll().map(s=>({name:s.id,values:s.validValues}))));domains=new Map(definitions.map(s=>[s.name,s.values]));emit({domains:definitions});}
 for(let n=0;n<128&&typeIndex<types.length;n++){
  if(!record)prepare();
  const p=permutations[index++],states=p.getAllStates();
  b.setPermutation(BlockPermutation.resolve("minecraft:air"));b.setPermutation(p);
  if(b.typeId!==record.name){record.changed.push({states,changed_to:b.typeId});}
  else{
   const c=b.getComponent("minecraft:movable"),classification={movement:c?.movementType??null,sticky:c?.stickyType??null};
   if(!classification.movement)throw new Error("missing native movable component: "+record.name);
   record.observed_count++;
   if(record.classification===null)record.classification=classification;
   if(classification.movement!==record.classification.movement||classification.sticky!==record.classification.sticky)record.exceptions.push({states,...classification});
  }
  if(index===permutations.length){emit(record);record=null;typeIndex++;}
 }
 if(typeIndex===types.length){emit({complete:true,count:typeIndex});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack,typeIndex,index,name:types?.[typeIndex]});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30902)
    args = parser.parse_args()
    result = dict(complete=False, reference=__doc__, offline_reference=True,
                  binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), domains=[], blocks=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=SCRIPT, local_client=True)
    try:
        while True:
            line = reference.until(lambda s: "ADAMANT_MOVABLE_STATES " in s, timeout=45)[-1]
            record = json.loads(line.split("ADAMANT_MOVABLE_STATES ", 1)[1])
            if record.get("error"):
                raise RuntimeError(record)
            if "domains" in record:
                result["domains"] = record["domains"]
            if "name" in record:
                result["blocks"].append(record)
            if record.get("complete"):
                blocks = result["blocks"]
                if record["count"] != 1477 or len(blocks) != 1477 or len(result["domains"]) != 157:
                    raise RuntimeError("native permutation component export incomplete")
                if len({b["name"] for b in blocks}) != len(blocks):
                    raise RuntimeError("duplicate native type")
                for b in blocks:
                    if b["observed_count"] + len(b["changed"]) != b["permutation_count"]:
                        raise RuntimeError("unaccounted native permutation: " + b["name"])
                result["complete"] = True
                break
    finally:
        reference.stop()
        result["log_directory"] = str(reference.root)
        result["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(result, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps(dict(complete=result["complete"], blocks=len(result["blocks"]), logs=str(reference.root))))
