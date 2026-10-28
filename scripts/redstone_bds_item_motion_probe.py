#!/usr/bin/env python3
"""Capture original-BDS item air and landing motion through stable Script API."""
import argparse
import hashlib
import json
from pathlib import Path
from redstone_bds_probe import Reference

SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,cases=[];
const point=e=>({position:e.location,velocity:e.getVelocity(),on_ground:e.isOnGround});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");d.runCommand("tickingarea add 0 0 0 31 100 31 audit true");d.runCommand("gamerule domobspawning false");return;}
 if(!start){if(!d.getBlock({x:31,y:65,z:31}))return;
  for(const [name,p,v] of [["air",{x:8,y:70,z:8},{x:.1,y:.2,z:-.1}],["landing",{x:24,y:65.05,z:24},{x:.1,y:-.1,z:0}]]){
   d.runCommand(`fill ${p.x-3} 63 ${p.z-3} ${p.x+3} 73 ${p.z+3} air`);d.runCommand(`fill ${p.x-3} 64 ${p.z-3} ${p.x+3} 64 ${p.z+3} stone`);
   const e=d.spawnItem(new ItemStack("minecraft:diamond",1),p);e.clearVelocity();e.applyImpulse(v);
   cases.push({name,spawn:point(e),samples:[],entity:e});
  }
  start=system.currentTick;return;
 }
 for(const c of cases)c.samples.push({tick:system.currentTick,...point(c.entity)});
 if(system.currentTick-start===3){for(const c of cases){c.entity.remove();delete c.entity;emit(c);}emit({complete:true,count:cases.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("binary","assets","output"):parser.add_argument("--"+name,type=Path,required=True)
    parser.add_argument("--port",type=int,default=30888)
    args=parser.parse_args()
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT,local_client=True)
    output=dict(complete=False,reference=__doc__,scenarios=[],binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest())
    try:
        while True:
            line=reference.until(lambda s:"ADAMANT_TIMING " in s,45)[-1]
            record=json.loads(line.split("ADAMANT_TIMING ",1)[1])
            if record.get("error"): raise RuntimeError(record)
            if "name" in record: output["scenarios"].append(record)
            if record.get("complete"):
                if record["count"]!=2 or len(output["scenarios"])!=2 or any(len(c["samples"])!=3 for c in output["scenarios"]):
                    raise RuntimeError("incomplete native item motion capture")
                output["complete"]=True;break
    finally:
        reference.stop()
        output["log_directory"]=str(reference.root)
        args.output.write_text(json.dumps(output,indent=2)+"\n",encoding="utf-8")
        print(json.dumps(dict(complete=output["complete"],cases=len(output["scenarios"]),logs=str(reference.root))))
