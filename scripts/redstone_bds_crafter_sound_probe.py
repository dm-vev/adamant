#!/usr/bin/env python3
"""Capture native crafter success/failure sound packets on an owned loopback client.

Packet receipt times associate scenes, not native simulation ticks. Script API
states and barrel output distinguish valid, invalid and empty crafting grids.
"""
import hashlib
import json

from redstone_bds_client import LocalClient
from redstone_bds_probe import Reference
from redstone_bds_hopper_crafter_slots_probe import crafter_structure
from redstone_bds_timing_probe import arguments


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_CRAFTER_SOUND "+JSON.stringify(x));
let d,loaded=0,ready=false,running=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;cmd("tickingarea add -16 0 -16 31 100 31 audit true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([-4,65,-4])||!get([28,65,28])){if(system.currentTick-loaded>240)throw new Error("sound chunks not loaded");return;}
  cmd("fill -4 63 -4 28 63 28 stone");cmd("fill -4 64 -4 28 70 28 air");cmd("setworldspawn 8 64 8");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!start){if(system.currentTick&1)return;
  for(const recipe of ["valid","invalid","empty","valid_air","valid_full","cake_air"])for(const phase of [0,1]){
   const i=cells.length,base=[4+(i%4)*6,65,4+Math.floor(i/4)*6],source=[base[0]-1,65,base[2]],target=[base[0],65,base[2]-1];
   const grid=recipe==="cake_air"?"cake":recipe.startsWith("valid")?"valid":recipe;
   if(!cmd(`structure load audit:crafter_sound_${grid} ${base.join(" ")}`).successCount)throw new Error("sound input load failed");
   if(!recipe.endsWith("_air"))cmd(`setblock ${target.join(" ")} barrel`);
   if(recipe==="valid_full")cmd(`replaceitem block ${target.join(" ")} slot.container 0 stone 64`);
   if(recipe==="valid_full")for(let slot=1;slot<27;slot++)cmd(`replaceitem block ${target.join(" ")} slot.container ${slot} stone 64`);
   cells.push({scenario:{recipe,phase},base,source,target,setup_tick:system.currentTick,setup_time_ms:Date.now(),samples:[],changes:[]});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const b=get(c.base),inventory=get(c.target).getComponent("minecraft:inventory")?.container;
  c.samples.push({tick:system.currentTick,block:{id:b.typeId,states:b.permutation.getAllStates()},outputs:inventory?.getItem(0)?.amount??0});
  if(t===40+c.scenario.phase||t===56+c.scenario.phase){const powered=t===40+c.scenario.phase;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===100){for(const c of cells){if(c.samples.some(s=>s.block.states.crafting)!==(c.scenario.recipe.startsWith("valid")||c.scenario.recipe==="cake_air"))throw new Error("sound recipe crafting control differs");
  if(c.scenario.recipe==="valid"&&c.samples.at(-1).outputs!==1)throw new Error("sound barrel output control differs");
  c.end_time_ms=Date.now();emit(c);}emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__=='__main__':
    args=arguments()
    structures={
        'audit/crafter_sound_valid':crafter_structure([dict(id='minecraft:iron_ingot',count=1)]*9),
        'audit/crafter_sound_invalid':crafter_structure([dict(id='minecraft:dirt',count=1)]*9),
        'audit/crafter_sound_empty':crafter_structure([None]*9),
        'audit/crafter_sound_cake':crafter_structure([dict(id='minecraft:'+name,count=1) for name in ('milk_bucket','milk_bucket','milk_bucket','sugar','egg','sugar','wheat','wheat','wheat')]),
    }
    result=dict(complete=False,reference=__doc__,binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),scenarios=[],packet_observations=[])
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT,structures=structures,local_client=True)
    client=None
    try:
        reference.until(lambda s:'ADAMANT_CRAFTER_SOUND ' in s and '"ready":true' in s,timeout=45)
        client=LocalClient(reference,args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line=reference.until(lambda s:'ADAMANT_CRAFTER_SOUND ' in s,timeout=30)[-1]
            record=json.loads(line.split('ADAMANT_CRAFTER_SOUND ',1)[1])
            if record.get('error'):raise RuntimeError(record)
            if 'scenario' in record:result['scenarios'].append(record)
            if record.get('complete'):
                if record['count']!=12 or len(result['scenarios'])!=12:raise RuntimeError('incomplete native sound scenes')
                result['complete']=True;break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:record=json.loads(line)
                except (ValueError,TypeError):continue
                if 'sound' in record and 'crafter' in record['sound']['SoundType'].lower():result['packet_observations'].append(record)
        reference.stop()
        if result['complete']:
            for c in result['scenarios']:
                sounds=[r['sound'] for r in result['packet_observations']
                        if c['setup_time_ms']<=r['received_at_ms']<=c['end_time_ms']
                        and all(abs(r['sound']['Position'][i]-c['base'][i])<=1 for i in range(3))]
                if (c['scenario']['recipe']=='valid' and sounds) or (c['scenario']['recipe']!='valid' and not sounds):
                    result['complete']=False;result['error']='native crafter sound packet control differs'
                c['sounds']=sounds
        result['log_directory']=str(reference.root)
        args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'],sounds=len(result['packet_observations']),logs=str(reference.root))),flush=True)
        if not result['complete']:raise RuntimeError('native crafter sound capture incomplete')
