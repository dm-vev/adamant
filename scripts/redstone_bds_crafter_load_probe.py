#!/usr/bin/env python3
"""Save and load native crafter actors at each crafting animation age."""
import json
from pathlib import Path
import subprocess

from redstone_bds_hopper_crafter_slots_probe import crafter_structure
from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,loaded=0,ready=false,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  cmd("tickingarea add 0 0 0 127 100 95 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,95])){if(system.currentTick-loaded>240)throw new Error("load chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(const age of [0,1,2,3,4,5])for(const phase of [0,1]){
   const i=cells.length,base=[4+(i%4)*32,65,4+Math.floor(i/4)*32],source=[base[0]-1,65,base[2]],clone=[base[0]+2,65,base[2]];
   cmd(`fill ${base[0]-2} 64 ${base[2]-2} ${base[0]+4} 68 ${base[2]+2} air`);
   if(!cmd(`structure load audit:crafter_load_input ${base.join(" ")}`).successCount)throw new Error("load input failed");
   cmd(`setblock ${base[0]} 65 ${base[2]-1} barrel`);cmd(`setblock ${clone[0]} 65 ${clone[2]-1} barrel`);
   cells.push({scenario:{age,phase},base,source,clone,setup_tick:system.currentTick,changes:[],samples:[],first_craft:0,load:null});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(let i=0;i<cells.length;i++){const c=cells[i],b=state(c.base);
  c.samples.push({tick:system.currentTick,block:b,clone:state(c.clone)});
  if(!c.first_craft&&b.states.crafting)c.first_craft=system.currentTick;
  if(c.first_craft&&system.currentTick===c.first_craft+c.scenario.age){
   const name="audit_crafter_load_"+i;
   if(!cmd(`structure save ${name} ${c.base.join(" ")} ${c.base.join(" ")} false disk true`).successCount)throw new Error("save failed");
   if(!cmd(`structure load ${name} ${c.clone.join(" ")}`).successCount)throw new Error("reload failed");
   c.load={tick:system.currentTick,snapshot:"mystructure:"+name,block:b,clone:state(c.clone)};
  }
  if(t===40+c.scenario.phase||t===72+c.scenario.phase){const powered=t===40+c.scenario.phase;
   cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===112){for(const c of cells){if(!c.first_craft||!c.load)throw new Error("missing native craft/load control");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def readback(result, helper):
    database = Path(result['log_directory']) / 'worlds/redstone-reference/db'
    completed = subprocess.run([str(helper), '-db', str(database), '-prefix',
        'structuretemplate_mystructure:audit_crafter_load_', '-immutable-audit-wal'],
        capture_output=True, text=True, encoding='utf-8', check=True)
    snapshots = {r['name']: r for line in completed.stdout.splitlines() if (r := json.loads(line))}
    if len(snapshots) != 12:
        raise RuntimeError('incomplete native crafter load snapshots')
    for c in result['scenarios']:
        record = snapshots[c['load']['snapshot']]
        palette = record['structure']['structure']['palette']['default']
        block = palette['block_palette'][0]
        actor = palette['block_position_data']['0']['block_entity_data']
        if block['name'] != 'minecraft:crafter' or actor['id'] != 'Crafter' or actor['disabled_slots'] != 0:
            raise RuntimeError('invalid native crafter load actor')
        if {k: int(v) if isinstance(v, bool) else v for k, v in c['load']['block']['states'].items()} != block['states']:
            raise RuntimeError('saved native permutation differs')
        c['load']['actor'] = actor
    result['actor_readback'] = dict(method='stopped BDS immutable structures, read-only LevelDB and verified WAL',
                                    snapshots=12, journal=completed.stderr.strip())
    return result


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    structures = {'audit/crafter_load_input': crafter_structure([dict(id='minecraft:iron_ingot', count=1)]*9)}
    capture(args, SCRIPT, 12, structures=structures)
    result = json.loads(args.output.read_text(encoding='utf-8'))
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = readback(result, Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, snapshots=12)), flush=True)
