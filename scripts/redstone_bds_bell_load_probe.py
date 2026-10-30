#!/usr/bin/env python3
"""Save ringing and idle native bells, reload with and without initial power."""
import json
from pathlib import Path
import subprocess

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[],serial=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  cmd("tickingarea add 0 0 0 127 100 127 audit_bell_load true");cmd("gamerule randomtickspeed 0");
  cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-loaded>480)throw new Error("bell load chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%4*32,65,8+Math.floor(i/4)*32],source=[base[0]-1,65,base[2]],clone=[base[0]+4,65,base[2]],clone_source=[base[0]+3,65,base[2]];
   cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+6} 68 ${base[2]+2} air`);
   cmd(`setblock ${base[0]} 64 ${base[2]} stone`);cmd(`setblock ${clone[0]} 64 ${clone[2]} stone`);
   cmd(`setblock ${base.join(" ")} bell ["attachment"="standing","direction"=0,"toggle_bit"=false]`);
   cells.push({scenario,base,source,clone,clone_source,setup_tick:system.currentTick,changes:[],samples:[],first_ring:0,load:null});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(let i=0;i<cells.length;i++){const c=cells[i],b=state(c.base),f={tick:system.currentTick,block:b,source:state(c.source),clone:state(c.clone),clone_source:state(c.clone_source)};
  if(c.load){const name="audit_bell_load_frame_"+serial++;
   if(!cmd(`structure save ${name} ${c.clone.join(" ")} ${c.clone.join(" ")} false disk true`).successCount)throw new Error("bell load frame failed");
   f.snapshot="mystructure:"+name;
  }c.samples.push(f);
  if(!c.first_ring&&b.states.toggle_bit)c.first_ring=system.currentTick;
  if(c.first_ring&&system.currentTick===c.first_ring+c.scenario.age){
   const name="audit_bell_load_input_"+i;
   if(!cmd(`structure save ${name} ${c.base.join(" ")} ${c.base.join(" ")} false disk true`).successCount)throw new Error("bell load save failed");
   if(c.scenario.initial_power)cmd(`setblock ${c.clone_source.join(" ")} redstone_block`);
   if(!cmd(`structure load ${name} ${c.clone.join(" ")}`).successCount)throw new Error("bell reload failed");
   c.load={tick:system.currentTick,snapshot:"mystructure:"+name,block:b,clone:state(c.clone)};
  }
  if(t===40+c.scenario.phase||t===76+c.scenario.phase){const powered=t===40+c.scenario.phase;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered,target:"original"});}
  if(t===104+c.scenario.phase||t===120+c.scenario.phase||t===132+c.scenario.phase||t===140+c.scenario.phase){const powered=t===120+c.scenario.phase||t===140+c.scenario.phase;
   cmd(`setblock ${c.clone_source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered,target:"clone"});}
 }
 if(t===160){for(const c of cells){if(!c.load||c.changes.length!==6)throw new Error("bell load controls missing");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def readback(result, helper):
    completed = subprocess.run([str(helper), '-db', str(Path(result['log_directory'])/'worlds/redstone-reference/db'),
        '-prefix', 'structuretemplate_mystructure:audit_bell_load_', '-immutable-audit-wal'],
        capture_output=True, text=True, encoding='utf-8', check=True)
    snapshots = {r['name']: r for line in completed.stdout.splitlines() if (r := json.loads(line))}
    used = set()
    for c in result['scenarios']:
        frames = [c['load']] + [f for f in c['samples'] if 'snapshot' in f]
        for f in frames:
            name = f['snapshot']
            if name in used:
                raise RuntimeError('reused native bell load snapshot')
            used.add(name)
            root = snapshots[name]['structure']
            palette = root['structure']['palette']['default']
            b = palette['block_palette'][0]
            actor = palette['block_position_data']['0']['block_entity_data']
            state = f['block'] if f is c['load'] else f['clone']
            states = {k: int(v) if isinstance(v, bool) else v for k, v in state['states'].items()}
            if root['size'] != [1, 1, 1] or b['name'] != 'minecraft:bell' or b['states'] != states or actor['id'] != 'Bell':
                raise RuntimeError('native bell reload state/actor controls differ')
            f['actor'] = actor
    if used != snapshots.keys():
        raise RuntimeError('unexpected native bell reload snapshot count')
    result['actor_readback'] = dict(method='native disk structures; stopped read-only LevelDB plus verified immutable WAL',
                                   snapshots=len(used), journal=completed.stderr.strip())
    return result


if __name__ == '__main__':
    args = arguments(); args.offline_reference = True
    cases = [dict(age=age, initial_power=power, phase=phase) for age in (0, 5, 24, 25)
             for power in (False, True) for phase in (0, 1)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
    result = json.loads(args.output.read_text(encoding='utf-8')); result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = readback(result, Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, snapshots=result['actor_readback']['snapshots'])), flush=True)
