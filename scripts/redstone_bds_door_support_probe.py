#!/usr/bin/env python3
"""Record native door survival checks, cancellation and neighbour tick phases."""
import hashlib
import json

from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


CASES = [dict(block=b, action=a, phase=p)
         for b in ('wooden_door', 'copper_door', 'iron_door')
         for a in ('support_remove', 'support_restore', 'upper_remove', 'lower_remove',
                   'other_material', 'two_lowers', 'two_uppers', 'side_floating')
         for p in (0, 1)]

SCRIPT = r'''
import {world,system,BlockPermutation} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_SUPPORT "+JSON.stringify(x));
let d,load=0,ready=false,index=0,current=null,lastSample=0;
const maxZ=16*Math.ceil((8+Math.floor((cases.length-1)/12)*6)/16)-1;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(p,id,states={})=>get(p).setPermutation(BlockPermutation.resolve("minecraft:"+id,states));
const door=(p,id,top)=>set(p,id,{"minecraft:cardinal_direction":"north","open_bit":false,"door_hinge_bit":false,"upper_block_bit":top});
const itemBody=e=>{const s=e.getComponent("minecraft:item").itemStack;return {actor_id:e.id,id:s.typeId,count:s.amount,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB()};};
const items=()=>d.getEntities({type:"minecraft:item",location:{x:current.base[0]+.5,y:65.5,z:current.base[2]+.5},maxDistance:4}).map(itemBody);
world.afterEvents.entitySpawn.subscribe(e=>{try{
 if(!current||e.entity.typeId!=="minecraft:item")return;
 const p=e.entity.location;if(Math.abs(p.x-current.base[0]-.5)>2.5||Math.abs(p.z-current.base[2]-.5)>2.5)return;
 current.spawns.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,body:itemBody(e.entity)});
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  cmd(`tickingarea add -16 0 -16 79 100 ${maxZ} audit_door_support true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,maxZ-3])){if(system.currentTick-load>480)throw new Error("door support chunks not ready");return;}ready=true;emit({ready:true});return;}
 if(!current){const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6],positions=[-1,0,1,2].map(y=>[base[0],65+y,base[2]]);
  cmd(`gamerule dotiledrops ${!!c.drops}`);
  if(c.foundation)cmd(`setblock ${base[0]} 63 ${base[2]} stone`);
  if(c.action!=="side_floating")cmd(`setblock ${base[0]} 64 ${base[2]} stone`);
  const success=cmd(`setblock ${base.join(" ")} ${c.block} ["minecraft:cardinal_direction"="north","door_hinge_bit"=false,"open_bit"=false,"upper_block_bit"=false]`).successCount;
  if(success!==1)throw new Error("native lower placement failed");
  current={scenario:c,base,positions,setup_tick:system.currentTick,initial:positions.map(read),samples:[],spawns:[]};return;
 }
 const c=current.scenario,base=current.base,positions=current.positions;
 current.samples.push({tick:system.currentTick,blocks:positions.map(read),items:items(),spawn_count:current.spawns.length});lastSample=system.currentTick;
 if(!current.change_tick&&system.currentTick-current.setup_tick>=8&&(system.currentTick&1)===c.phase){
  const [below,lower,upper]=positions;current.change_tick=system.currentTick;
  if(c.action==="support_remove"||c.action==="support_restore")set(below,"air");
  if(c.action==="support_restore")set(below,"stone");
  if(c.action==="upper_remove")set(upper,"air");
  if(c.action==="lower_remove")set(lower,"air");
  if(c.action==="other_material")door(upper,"spruce_door",true);
  if(c.action==="two_lowers")door(upper,c.block,false);
  if(c.action==="two_uppers")door(lower,c.block,true);
  if(c.action==="side_floating")set([base[0]+1,65,base[2]],"stone");
  if(c.replacement){const p=c.top?upper:lower,states=get(p).permutation.getAllStates();delete states.direction;set(p,c.replacement,states);}
  if(c.support){const input=BlockPermutation.resolve("minecraft:"+c.support.id,c.support.states||{});current.support_input={id:"minecraft:"+c.support.id,states:input.getAllStates()};get(below).setPermutation(input);}
  current.after=positions.map(read);
 }
 if(current.change_tick&&system.currentTick-current.change_tick>=8){emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture(args, cases=CASES, source_script=None, description=None):
    result = dict(complete=False, reference=description or __doc__,
                  binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), scenarios=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=(SCRIPT if source_script is None else source_script).replace('CASES', json.dumps(cases)))
    try:
        while True:
            line = reference.until(lambda s: 'ADAMANT_SUPPORT ' in s, timeout=45)[-1]
            record = json.loads(line.split('ADAMANT_SUPPORT ', 1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
                print(json.dumps(dict(count=len(result['scenarios']), scenario=record['scenario'])), flush=True)
            if record.get('complete'):
                if record['count'] != len(cases) or len(result['scenarios']) != len(cases):
                    raise RuntimeError('incomplete native door survival corpus')
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
