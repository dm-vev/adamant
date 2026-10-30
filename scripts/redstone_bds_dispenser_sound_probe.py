#!/usr/bin/env python3
"""Capture ordinary dispenser/dropper sound and particle packets on owned BDS."""
import hashlib
import json

from redstone_bds_client import LocalClient
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_DISPENSER_SOUND "+JSON.stringify(x));
let d,loaded=0,ready=false,running=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const vectors=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;cmd("tickingarea add -16 0 -16 63 100 95 audit true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([-4,65,-4])||!get([52,65,74])){if(system.currentTick-loaded>240)throw new Error("sound chunks not loaded");return;}
  cmd("fill -4 63 -4 52 63 74 stone");cmd("fill -4 64 -4 52 70 74 air");cmd("setworldspawn 24 64 36");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!start){if(system.currentTick%20!==2)return;
  for(const kind of ["dropper","dispenser"])for(const face of [0,1,2,3,4,5])for(const destination of ["empty","air","barrel","full"])for(const phase of [0,1]){
   const i=cells.length,base=[4+(i%8)*6,65,4+Math.floor(i/8)*6],source=[base[0]+(face===4?1:-1),65,base[2]],target=base.map((v,j)=>v+vectors[face][j]);
   cmd(`setblock ${base.join(" ")} ${kind} ["facing_direction"=${face}]`);
   if(destination!=="empty")cmd(`replaceitem block ${base.join(" ")} slot.container 4 iron_ingot 3`);
   if(destination==="barrel"||destination==="full")cmd(`setblock ${target.join(" ")} barrel`);
   if(destination==="full")for(let slot=0;slot<27;slot++)cmd(`replaceitem block ${target.join(" ")} slot.container ${slot} stone 64`);
   cells.push({scenario:{kind,face,destination,phase},base,source,target,setup_tick:system.currentTick,samples:[],changes:[]});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const b=get(c.base),inventory=get(c.target).getComponent("minecraft:inventory")?.container;
  c.samples.push({tick:system.currentTick,block:{id:b.typeId,states:b.permutation.getAllStates()},
   source_count:b.getComponent("minecraft:inventory").container.getItem(4)?.amount??0,outputs:inventory?.getItem(0)?.amount??0});
  if(t===40+c.scenario.phase||t===56+c.scenario.phase){const powered=t===40+c.scenario.phase;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===100){for(const c of cells){const q=c.scenario,last=c.samples.at(-1),retained=q.destination==="empty"?0:q.kind==="dropper"&&q.destination==="full"?3:2;
  if(last.source_count!==retained)throw new Error("native sound inventory control differs "+JSON.stringify(q));emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    result = dict(complete=False, reference=__doc__, binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                  scenarios=[], packet_observations=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=SCRIPT, local_client=True)
    client = None
    try:
        reference.until(lambda s: 'ADAMANT_DISPENSER_SOUND ' in s and '"ready":true' in s, timeout=45)
        client = LocalClient(reference, args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line = reference.until(lambda s: 'ADAMANT_DISPENSER_SOUND ' in s, timeout=30)[-1]
            record = json.loads(line.split('ADAMANT_DISPENSER_SOUND ', 1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'scenario' in record:
                result['scenarios'].append(record)
            if record.get('complete'):
                if record['count'] != 96 or len(result['scenarios']) != 96:
                    raise RuntimeError('incomplete native sound scenes')
                result['complete'] = True
                break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:
                    record = json.loads(line)
                except (ValueError, TypeError):
                    continue
                if 'sound' in record or 'level_event' in record:
                    result['packet_observations'].append(record)
        reference.stop()
        for c in result['scenarios']:
            c['packets'] = [r for r in result['packet_observations']
                            if all(abs((r.get('sound') or r['level_event'])['Position'][i]-c['base'][i]) <= 1.25 for i in range(3))]
            q = c['scenario']
            expected = 1 if q['destination'] == 'empty' else 2 if q['kind'] == 'dispenser' else 3 if q['destination'] == 'air' else 0
            if len(c['packets']) != expected:
                result['complete'] = False
                result['error'] = 'native sound/particle packet controls differ'
        result['log_directory'] = str(reference.root)
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], packets=len(result['packet_observations']), logs=str(reference.root))), flush=True)
        if not result['complete']:
            raise RuntimeError('native dispenser sound capture incomplete')
