#!/usr/bin/env python3
"""Drive real native arrow collisions against every bell attachment/direction."""
import json
from pathlib import Path
import subprocess

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,0,1],[-1,0,0],[0,0,-1],[1,0,0]];
let d,loaded=0,ready=false,start=0,cells=[],serial=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const state=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
world.afterEvents.projectileHitBlock.subscribe(e=>{
 const c=cells.find(c=>c.entity?.id===e.projectile.id);if(!c)return;
 const hit=e.getBlockHit();c.hits.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:hit.block.location,face:String(hit.face),face_location:hit.faceLocation});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");loaded=system.currentTick;
  cmd("tickingarea add 0 0 0 127 100 95 audit_bell_arrow_a true");cmd("tickingarea add 0 0 96 127 100 127 audit_bell_arrow_b true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-loaded>480)throw new Error("bell arrow chunks not ready");return;}
  if(system.currentTick%20!==2)return;
  for(let i=0;i<cases.length;i++){
   const scenario=cases[i],base=[8+i%8*16,65,8+Math.floor(i/8)*16],v=delta[scenario.direction];
   cmd(`fill ${base[0]-3} 63 ${base[2]-3} ${base[0]+3} 68 ${base[2]+3} air`);
   cmd(`setblock ${base[0]} 64 ${base[2]} stone`);
   if(scenario.attachment==="hanging")cmd(`setblock ${base[0]} 66 ${base[2]} stone`);
   if(scenario.attachment==="side"||scenario.attachment==="multiple"){
    cmd(`setblock ${base[0]-v[0]} 65 ${base[2]-v[2]} stone`);
    if(scenario.attachment==="multiple")cmd(`setblock ${base[0]+v[0]} 65 ${base[2]+v[2]} stone`);
   }
   cmd(`setblock ${base.join(" ")} bell ["attachment"="${scenario.attachment}","direction"=${scenario.direction},"toggle_bit"=false]`);
   cells.push({scenario,base,setup_tick:system.currentTick,changes:[],samples:[],hits:[],entity:null,spawn:null});
  }start=system.currentTick;ready=true;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){const name="audit_bell_arrow_frame_"+serial++;
  if(!cmd(`structure save ${name} ${c.base.join(" ")} ${c.base.join(" ")} false disk true`).successCount)throw new Error("bell arrow actor snapshot failed");
  const e=c.entity?.isValid?c.entity:null;
  c.samples.push({tick:system.currentTick,block:state(c.base),snapshot:"mystructure:"+name,projectile:e?{id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:typeof e.getAABB==="function"?e.getAABB():null}:null});
  if(t===40+c.scenario.phase){const v=delta[c.scenario.approach],position={x:c.base[0]+.5+v[0]*.8,y:65.5,z:c.base[2]+.5+v[2]*.8};
   const arrow=d.spawnEntity("minecraft:arrow",position),projectile=arrow.getComponent("minecraft:projectile");
   if(!projectile)throw new Error("native arrow lacks projectile component");
   projectile.shoot({x:-v[0]*.75,y:0,z:-v[2]*.75});c.entity=arrow;
   c.spawn={id:arrow.typeId,requested_position:position,position:arrow.location,velocity:arrow.getVelocity()};c.changes.push({tick:system.currentTick,kind:"spawn"});
  }
 }
 if(t===80){for(const c of cells){if(!c.spawn||!c.hits.length)throw new Error("native arrow collision control missing");delete c.entity;emit(c);}
  emit({complete:true,count:cells.length});system.clearRun(h);}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def readback(result, helper):
    completed = subprocess.run([str(helper), '-db', str(Path(result['log_directory'])/'worlds/redstone-reference/db'),
        '-prefix', 'structuretemplate_mystructure:audit_bell_arrow_frame_', '-immutable-audit-wal'],
        capture_output=True, text=True, encoding='utf-8', check=True)
    snapshots = {r['name']: r for line in completed.stdout.splitlines() if (r := json.loads(line))}
    used = set()
    for c in result['scenarios']:
        if len(c['samples']) != 80:
            raise RuntimeError('incomplete native bell projectile frames')
        for f in c['samples']:
            name = f['snapshot']
            if name in used:
                raise RuntimeError('reused native bell projectile snapshot')
            used.add(name)
            palette = snapshots[name]['structure']['structure']['palette']['default']
            b = palette['block_palette'][0]
            actor = palette['block_position_data']['0']['block_entity_data']
            states = {k: int(v) if isinstance(v, bool) else v for k, v in f['block']['states'].items()}
            if b['name'] != 'minecraft:bell' or b['states'] != states or actor['id'] != 'Bell':
                raise RuntimeError('native bell projectile snapshot control differs')
            f['actor'] = actor
    if used != snapshots.keys():
        raise RuntimeError('unexpected native bell projectile snapshot count')
    result['actor_readback'] = dict(method='native per-tick disk structures; stopped read-only LevelDB plus verified immutable WAL',snapshots=len(used),journal=completed.stderr.strip())
    return result


if __name__ == '__main__':
    args = arguments(); args.offline_reference = True
    cases = [dict(attachment=a,direction=d,approach=p,phase=(d+p)&1)
             for a in ('standing','hanging','side','multiple') for d in range(4) for p in range(4)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
    result = json.loads(args.output.read_text(encoding='utf-8')); result['complete'] = False
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    result = readback(result,Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete'] = True
    args.output.write_text(json.dumps(result,separators=(',',':'))+'\n',encoding='utf-8')
    print(json.dumps(dict(complete=True,scenes=len(cases),snapshots=result['actor_readback']['snapshots'])),flush=True)
