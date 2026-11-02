#!/usr/bin/env python3
"""Record native setblock/fill eligibility and complete linked door output."""
import hashlib
import json

from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


CONTEXTS=('clear','no_support','above_stone','above_other','old_lower','old_upper','below_lower','below_upper')
CASES=[dict(block=b,command=command,context=context,top=top,direction=n,
            open=bool(n&1),hinge=bool(n&2))
       for b in ('wooden_door','copper_door','iron_door') for command in ('setblock','fill')
       for context in CONTEXTS for top in (False,True) for n in range(4)]

SCRIPT=r'''
import {world,system,BlockPermutation} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_COMMAND "+JSON.stringify(x));
const directions=["north","east","south","west"];
let d,load=0,ready=false,index=0,current=null;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(p,id,states={})=>get(p).setPermutation(BlockPermutation.resolve("minecraft:"+id,states));
const door=(p,id,top,dir="west",open=false,hinge=true)=>set(p,id,{"minecraft:cardinal_direction":dir,"open_bit":open,"door_hinge_bit":hinge,"upper_block_bit":top});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 79 100 207 audit_door_commands true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,200])){if(system.currentTick-load>480)throw new Error("door command chunks not ready");return;}
  ready=true;emit({ready:true});return;}
 if(!current){const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6],positions=[];
  if(c.size){for(let x=0;x<c.size[0];x++)for(let y=-1;y<=c.size[1];y++)for(let z=0;z<c.size[2];z++)positions.push([base[0]+x,65+y,base[2]+z]);}
  else for(const y of [-1,0,1,2])positions.push([base[0],65+y,base[2]]);
  cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+2} 64 ${base[2]+2} stone`);
  const [below,target,above,above2]=positions;
  if(c.context==="no_support")set(below,"air");
  if(c.context==="above_stone")set(above,"stone");
  if(c.context==="above_other")door(above,"spruce_door",true);
  if(c.context==="old_lower"){door(target,c.block,false);door(above,c.block,true);}
  if(c.context==="old_upper"){door(below,c.block,false);door(target,c.block,true);}
  if(c.context==="below_lower")door(below,"spruce_door",false);
  if(c.context==="below_upper")door(below,"spruce_door",true);
  current={scenario:c,base,positions,setup_tick:system.currentTick,initial:positions.map(read),samples:[]};
  const block=`${c.block} ["minecraft:cardinal_direction"="${directions[c.direction]}","door_hinge_bit"=${c.hinge},"open_bit"=${c.open},"upper_block_bit"=${c.top}]`;
  current.requested={id:"minecraft:"+c.block,states:{"minecraft:cardinal_direction":directions[c.direction],door_hinge_bit:c.hinge,open_bit:c.open,upper_block_bit:c.top}};
  const end=c.size?base.map((v,i)=>v+c.size[i]-1):base;
  current.command=c.command==="setblock"?`setblock ${base.join(" ")} ${block}`:`fill ${base.join(" ")} ${end.join(" ")} ${block}`;
  if(c.mode)current.command+=" "+c.mode;
  const result=cmd(current.command);current.success_count=result.successCount;current.after=positions.map(read);return;
 }
 current.samples.push({tick:system.currentTick,blocks:current.positions.map(read)});
 if(system.currentTick-current.setup_tick>=8){emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture(args,cases=CASES):
    result=dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),scenarios=[])
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT.replace('CASES',json.dumps(cases)))
    try:
        while True:
            line=reference.until(lambda s:'ADAMANT_COMMAND ' in s,timeout=45)[-1]
            record=json.loads(line.split('ADAMANT_COMMAND ',1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
                print(json.dumps(dict(count=len(result['scenarios']),scenario=record['scenario'])),flush=True)
            if record.get('complete'):
                if record['count']!=len(cases) or len(result['scenarios'])!=len(cases):
                    raise RuntimeError('incomplete native door command matrix')
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
