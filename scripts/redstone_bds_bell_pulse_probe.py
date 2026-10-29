#!/usr/bin/env python3
"""Capture bell redstone pulses and per-tick native actor state using structures."""
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
  cmd("tickingarea add 0 0 0 127 100 95 audit_bell_a true");cmd("tickingarea add 0 0 96 127 100 127 audit_bell_b true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-loaded>480)throw new Error("bell chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%8*16,65,8+Math.floor(i/8)*16],support=[base[0]-1,65,base[2]],source=[base[0]-(scenario.conducted?2:1),65,base[2]];
   cmd(`fill ${base[0]-3} 64 ${base[2]-2} ${base[0]+2} 68 ${base[2]+2} air`);
   cmd(`setblock ${base[0]} 64 ${base[2]} stone`);
   cmd(`setblock ${base.join(" ")} bell ["attachment"="standing","direction"=${scenario.direction},"toggle_bit"=false]`);
   if(scenario.conducted)cmd(`setblock ${support.join(" ")} stone`);
   cells.push({scenario,base,source,support,setup_tick:system.currentTick,changes:[],samples:[]});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const name="audit_bell_snap_"+serial++;
  if(!cmd(`structure save ${name} ${c.base.join(" ")} ${c.base.join(" ")} false disk true`).successCount)throw new Error("bell actor snapshot failed");
  c.samples.push({tick:system.currentTick,block:state(c.base),source:state(c.source),snapshot:"mystructure:"+name});
  for(let edge=0;edge<c.scenario.edges.length;edge++)if(t===40+c.scenario.phase+c.scenario.edges[edge]){
   const powered=edge%2===0;
   if(c.scenario.conducted){cmd(`setblock ${c.source.join(" ")} air`);cmd(`setblock ${c.source.join(" ")} stone_button ["facing_direction"=4,"button_pressed_bit"=${powered}]`);}
   else cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);
   c.changes.push({tick:system.currentTick,powered});
  }
 }
 if(t===112){for(const c of cells){if(c.changes.length!==c.scenario.edges.length)throw new Error("bell source control differs");emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def bell_cases():
    return [dict(direction=direction,conducted=conducted,phase=phase,edges=edges)
            for direction in range(4) for conducted in (False,True) for phase in (0,1)
            for edges in ([0,1],[0,2],[0,8],[0,8,20,28])]


def readback(result,helper):
    database=Path(result['log_directory'])/'worlds/redstone-reference/db'
    completed=subprocess.run([str(helper),'-db',str(database),'-prefix','structuretemplate_mystructure:audit_bell_snap_',
        '-immutable-audit-wal'],capture_output=True,text=True,encoding='utf-8',check=True)
    snapshots={r['name']:r for line in completed.stdout.splitlines() if (r:=json.loads(line))}
    expected=len(result['scenarios'])*112
    if len(snapshots)!=expected:
        raise RuntimeError('incomplete native bell actor snapshots')
    used=set()
    for c in result['scenarios']:
        if len(c['samples'])!=112:
            raise RuntimeError('incomplete native bell frames')
        for frame in c['samples']:
            name=frame['snapshot']
            if name in used:
                raise RuntimeError('reused native bell snapshot')
            used.add(name)
            root=snapshots[name]['structure']
            if root['format_version']!=2 or root['size']!=[1,1,1]:
                raise RuntimeError('invalid native bell structure')
            palette=root['structure']['palette']['default']
            block=palette['block_palette'][0]
            actor=palette['block_position_data']['0']['block_entity_data']
            state={k:int(v) if isinstance(v,bool) else v for k,v in frame['block']['states'].items()}
            if block['name']!='minecraft:bell' or block['states']!=state or actor['id']!='Bell':
                raise RuntimeError('native bell state/snapshot controls differ')
            if actor['Ringing'] not in (0,1) or not 0<=actor['Ticks']<=50:
                raise RuntimeError('invalid native bell ring state')
            frame['actor']=actor
    result['actor_readback']=dict(method='native per-tick disk structures; stopped read-only LevelDB plus verified immutable WAL',
                                  snapshots=expected,journal=completed.stderr.strip())
    return result


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    cases=bell_cases()
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
    result=json.loads(args.output.read_text(encoding='utf-8'));result['complete']=False
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    result=readback(result,Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete']=True
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,snapshots=len(cases)*112)),flush=True)
