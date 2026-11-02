#!/usr/bin/env python3
"""Observe native door ray shapes with independently authored half properties.

These are controlled command permutations, not player placement traces.
The native dimension performs every ray cast; no shape is reconstructed here.
"""
import hashlib
import json

from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


CASES = [dict(block=block,lower_direction=lower,upper_direction=upper,
              lower_hinge=lh,upper_hinge=uh,open=opened)
         for block in ('wooden_door','copper_door','iron_door')
         for lower in range(4) for upper in range(4) for lh in (False,True)
         for uh in (False,True) for opened in (False,True)]

SCRIPT = r'''
import {world,system,BlockPermutation} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_SHAPE "+JSON.stringify(x));
const directions=["north","east","south","west"],offsets=[.09375,.5,.90625];
let d,load=0,ready=false,index=0,current=null;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  cmd("tickingarea add -16 0 -16 79 100 207 audit_door_shapes true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,200])){if(system.currentTick-load>480)throw new Error("door shape chunks not ready");return;}ready=true;emit({ready:true});return;}
 if(!current){const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6];
  cmd(`setblock ${base[0]} 64 ${base[2]} stone`);
  const command_success=[];
  for(let half=0;half<2;half++)command_success.push(cmd(`setblock ${base[0]} ${65+half} ${base[2]} ${c.block} ["minecraft:cardinal_direction"="${directions[half?c.upper_direction:c.lower_direction]}","door_hinge_bit"=${half?c.upper_hinge:c.lower_hinge},"open_bit"=${c.open},"upper_block_bit"=${!!half}]`).successCount);
  current={scenario:c,base,setup_tick:system.currentTick,command_success,command_blocks:[read(base),read([base[0],66,base[2]])]};
  // /setblock runs DoorBlock::onFillBlock and can normalize the other half.
  // Direct API permutations separately supply the geometry dependency inputs.
  for(let half=0;half<2;half++)get([base[0],65+half,base[2]]).setPermutation(BlockPermutation.resolve("minecraft:"+c.block,
   {"minecraft:cardinal_direction":directions[half?c.upper_direction:c.lower_direction],"door_hinge_bit":half?c.upper_hinge:c.lower_hinge,"open_bit":c.open,"upper_block_bit":!!half}));
  current.initial_blocks=[read(base),read([base[0],66,base[2]])];return;
 }
 if(system.currentTick-current.setup_tick<4)return;
 const c=current.scenario,b=current.base;current.tick=system.currentTick;current.blocks=[read(b),read([b[0],66,b[2]])];current.rays=[];
 for(let half=0;half<2;half++){
  const state=current.blocks[half];
  if(state.id!=="minecraft:"+c.block||state.states.upper_block_bit!==!!half||state.states.open_bit!==c.open||state.states.door_hinge_bit!==(half?c.upper_hinge:c.lower_hinge)||state.states["minecraft:cardinal_direction"]!==directions[half?c.upper_direction:c.lower_direction])throw new Error("native half permutation control changed "+JSON.stringify(current));
  for(const axis of [0,2])for(const sign of [-1,1])for(const offset of offsets){
   const origin=[b[0]+.5,65.5+half,b[2]+.5],vector=[0,0,0];origin[axis]=b[axis]+(sign<0?2:-1);origin[2-axis]=b[2-axis]+offset;vector[axis]=sign;
   const hit=d.getBlockFromRay({x:origin[0],y:origin[1],z:origin[2]},{x:vector[0],y:0,z:vector[2]},
    {maxDistance:4,includePassableBlocks:true,includeLiquidBlocks:false});
   let result=null;
   if(hit){const p=hit.block.location;if(p.x!==b[0]||p.y!==65+half||p.z!==b[2])throw new Error("native ray hit an unrelated block");
    result={position:[p.x,p.y,p.z],face:String(hit.face),face_location:[hit.faceLocation.x,hit.faceLocation.y,hit.faceLocation.z]};}
   current.rays.push({half,origin,direction:vector,hit:result});
  }
 }
 if(!current.rays.some(r=>r.hit)||current.rays.every(r=>r.hit))throw new Error("native partial-door ray shape control failed "+JSON.stringify(current));
 emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture(args,cases=CASES):
    result=dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),scenarios=[])
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT.replace('CASES',json.dumps(cases)))
    try:
        while True:
            line=reference.until(lambda s:'ADAMANT_SHAPE ' in s,timeout=45)[-1]
            record=json.loads(line.split('ADAMANT_SHAPE ',1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
                print(json.dumps(dict(count=len(result['scenarios']),scenario=record['scenario'])),flush=True)
            if record.get('complete'):
                if record['count']!=len(cases) or len(result['scenarios'])!=len(cases):
                    raise RuntimeError('incomplete native door ray corpus')
                result['complete']=True
                break
    finally:
        reference.stop()
        result['log_directory']=str(reference.root)
        result['build_log']=[s for s in reference.log if 'Version:' in s or 'Build ID:' in s]
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],scenes=len(result['scenarios']),logs=str(reference.root))),flush=True)


if __name__=='__main__':
    capture(arguments())
