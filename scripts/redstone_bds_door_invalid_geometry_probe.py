#!/usr/bin/env python3
"""Observe native door ray shapes for absent, mixed and wrongly flagged halves.

Rays run immediately after explicit API permutations, before their scheduled
survival callbacks. The reference game performs every ray cast.
"""
import hashlib
import json

from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


CASES = [dict(block=b, topology=topology, lower_direction=n, upper_direction=(n+1)%4,
              lower_hinge=hinge, upper_hinge=not hinge, open=opened)
         for b in ('wooden_door', 'copper_door', 'iron_door')
         for topology in ('missing_above', 'missing_below', 'mixed', 'two_lowers', 'two_uppers')
         for n in range(4) for hinge in (False, True) for opened in (False, True)]

SCRIPT = r'''
import {world,system,BlockPermutation} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_INVALID_SHAPE "+JSON.stringify(x));
const directions=["north","east","south","west"],offsets=[.09375,.5,.90625];
let d,load=0,ready=false,index=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(p,id,states={})=>get(p).setPermutation(BlockPermutation.resolve("minecraft:"+id,states));
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  cmd("tickingarea add -16 0 -16 79 100 127 audit_invalid_shapes true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,124])){if(system.currentTick-load>480)throw new Error("invalid shape chunks not ready");return;}ready=true;emit({ready:true});return;}
 const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6];
 set([base[0],64,base[2]],"stone");
 for(let half=0;half<2;half++){
  const p=[base[0],65+half,base[2]];
  if((c.topology==="missing_above"&&half===1)||(c.topology==="missing_below"&&half===0)){set(p,"air");continue;}
  const top=c.topology==="two_lowers"?false:c.topology==="two_uppers"?true:!!half;
  const id=c.topology==="mixed"&&half===1?"spruce_door":c.block;
  set(p,id,{"minecraft:cardinal_direction":directions[half?c.upper_direction:c.lower_direction],"door_hinge_bit":half?c.upper_hinge:c.lower_hinge,"open_bit":c.open,"upper_block_bit":top});
 }
 const current={scenario:c,base,tick:system.currentTick,blocks:[read(base),read([base[0],66,base[2]])],rays:[]};
 for(let half=0;half<2;half++)for(const axis of [0,2])for(const sign of [-1,1])for(const offset of offsets){
  const origin=[base[0]+.5,65.5+half,base[2]+.5],vector=[0,0,0];origin[axis]=base[axis]+(sign<0?2:-1);origin[2-axis]=base[2-axis]+offset;vector[axis]=sign;
  const hit=d.getBlockFromRay({x:origin[0],y:origin[1],z:origin[2]},{x:vector[0],y:0,z:vector[2]},
   {maxDistance:4,includePassableBlocks:true,includeLiquidBlocks:false});
  let result=null;
  if(hit){const p=hit.block.location;if(p.x!==base[0]||p.y!==65+half||p.z!==base[2])throw new Error("invalid-pair ray hit unrelated cell");
   result={position:[p.x,p.y,p.z],face:String(hit.face),face_location:[hit.faceLocation.x,hit.faceLocation.y,hit.faceLocation.z]};}
  current.rays.push({half,origin,direction:vector,hit:result});
 }
 if(!current.rays.some(r=>r.hit)||current.rays.every(r=>r.hit))throw new Error("invalid-pair ray controls failed");
 emit(current);index++;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture(args):
    result = dict(complete=False, reference=__doc__,
                  binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), scenarios=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=SCRIPT.replace('CASES', json.dumps(CASES)))
    try:
        while True:
            line = reference.until(lambda s: 'ADAMANT_INVALID_SHAPE ' in s, timeout=45)[-1]
            record = json.loads(line.split('ADAMANT_INVALID_SHAPE ', 1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
                print(json.dumps(dict(count=len(result['scenarios']), scenario=record['scenario'])), flush=True)
            if record.get('complete'):
                if record['count'] != len(CASES) or len(result['scenarios']) != len(CASES):
                    raise RuntimeError('incomplete native invalid-pair geometry corpus')
                result['complete'] = True
                break
    finally:
        reference.stop()
        result['log_directory'] = str(reference.root)
        result['build_log'] = [s for s in reference.log if 'Version:' in s or 'Build ID:' in s]
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], scenes=len(result['scenarios']),
                              logs=str(reference.root))), flush=True)


if __name__ == '__main__':
    capture(arguments())
