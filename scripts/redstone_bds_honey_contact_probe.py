#!/usr/bin/env python3
"""Capture actual BDS item/armor-stand honey contacts, including no-contact controls."""
import argparse
import hashlib
import itertools
import json
from pathlib import Path
from redstone_bds_probe import Reference

SCRIPT=r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_HONEY "+JSON.stringify(x));
let d,start=0,offset=0,cells=[];
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");d.runCommand("tickingarea add 0 0 0 127 100 127 audit true");d.runCommand("gamerule domobspawning false");d.runCommand("gamerule randomtickspeed 0");return;}
 if(!start){
  for(let x=0;x<8;x++)for(let z=0;z<8;z++)if(!d.getBlock({x:x*16+8,y:65,z:z*16+8}))return;
  cells=[];
  for(let i=0;i<64&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],base=[8+(i%8)*16,65,8+Math.floor(i/8)*16];
   d.runCommand(`fill ${base[0]-4} 63 ${base[2]-4} ${base[0]+4} 71 ${base[2]+4} air`);
   d.runCommand(`fill ${base[0]-4} 64 ${base[2]-4} ${base[0]+4} 64 ${base[2]+4} stone`);
   d.runCommand(`fill ${base[0]} 65 ${base[2]} ${base[0]} 67 ${base[2]} honey_block`);
   const width=scenario.entity==="minecraft:item"?.25:.5,p={x:base[0]+.5,y:65+scenario.height,z:base[2]+.5};
   p[scenario.axis===0?"x":"z"]=base[scenario.axis]+.9375+width/2+scenario.gap;
   const e=scenario.entity==="minecraft:item"?d.spawnItem(new ItemStack("minecraft:diamond",1),p):d.spawnEntity(scenario.entity,p);
   e.clearVelocity();e.applyImpulse({x:scenario.axis===0?0:.16,y:scenario.velocity_y,z:scenario.axis===0?.16:0});
   cells.push({scenario,base,spawn:{position:e.location,velocity:e.getVelocity()},entity:e,samples:[]});
  }
  start=system.currentTick;return;
 }
 for(const c of cells)c.samples.push({tick:system.currentTick,position:c.entity.location,velocity:c.entity.getVelocity(),on_ground:c.entity.isOnGround});
 if(system.currentTick-start===20){for(const c of cells){c.entity.remove();delete c.entity;emit(c);}offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("binary","assets","output"):parser.add_argument("--"+name,type=Path,required=True)
    parser.add_argument("--port",type=int,default=30890)
    args=parser.parse_args()
    cases=[dict(entity=entity,axis=axis,gap=gap,height=height,velocity_y=velocity)
           for entity,axis,gap,height,velocity in itertools.product(("minecraft:item","minecraft:armor_stand"),(0,2),
                (-.0625,0,.03125,.0625,.125),(.25,2.875),(-.2,-.1,.1))]
    cases += [dict(entity=entity,axis=0,gap=-.5625 if entity=="minecraft:item" else -.6875,height=3.5,velocity_y=velocity)
              for entity,velocity in itertools.product(("minecraft:item","minecraft:armor_stand"),(-.2,.1))]
    # Bracket the native AABB-bottom - 0.1 friction query independently of
    # the landing collision. Initial upward motion keeps the first move clear.
    cases += [dict(entity="minecraft:armor_stand",axis=0,gap=-.6875,height=height,velocity_y=velocity)
              for height,velocity in itertools.product((3,3.0625,3.099,3.101,3.25),(-.2,.1))]
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT.replace("CASES",json.dumps(cases)),local_client=True)
    output=dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),scenarios=[])
    try:
        while True:
            line=reference.until(lambda s:"ADAMANT_HONEY " in s,45)[-1]
            record=json.loads(line.split("ADAMANT_HONEY ",1)[1])
            if record.get("error"):raise RuntimeError(record)
            if "scenario" in record:output["scenarios"].append(record)
            if record.get("complete"):
                if record["count"]!=len(cases) or len(output["scenarios"])!=len(cases) or any(len(c["samples"])!=20 for c in output["scenarios"]):raise RuntimeError("incomplete native honey capture")
                output["complete"]=True;break
    finally:
        reference.stop();output["log_directory"]=str(reference.root)
        args.output.write_text(json.dumps(output,separators=(",",":"))+"\n",encoding="utf-8")
        print(json.dumps(dict(complete=output["complete"],cases=len(output["scenarios"]),logs=str(reference.root))))
