#!/usr/bin/env python3
"""Record real native player bell use, direction gates and saved actor frames."""
import json
from pathlib import Path
import subprocess

import redstone_bds_switch_use_probe as player_probe
from redstone_bds_timing_probe import arguments


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
const delta=[[0,0,1],[-1,0,0],[0,0,-1],[1,0,0]],faces=[3,4,2,5];
let d,load=0,running=false,index=0,current=null,ready=false,lastSample=0,serial=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=()=>{const b=get(current.base);return {id:b.typeId,states:b.permutation.getAllStates()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||e.block.location.x!==current.base[0]||e.block.location.y!==65||e.block.location.z!==current.base[2])return;
 current.changes.push({kind:"click",tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,before:read(),rotation:e.player.getRotation()});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||e.block.location.x!==current.base[0]||e.block.location.y!==65||e.block.location.z!==current.base[2])return;
 current.results.push({tick:system.currentTick,source:read()});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 79 100 79 audit_bell_use true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([64,65,64])){if(system.currentTick-load>480)throw new Error("bell use chunks not ready");return;}
  cmd("fill -3 63 -3 3 63 3 stone");cmd("setworldspawn 0 64 2");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!current){const c=cases[index],base=[4+index%8*8,65,4+Math.floor(index/8)*8],v=delta[c.approach];
  cmd(`fill ${base[0]-3} 63 ${base[2]-3} ${base[0]+3} 63 ${base[2]+3} stone`);
  cmd(`fill ${base[0]-3} 64 ${base[2]-3} ${base[0]+3} 68 ${base[2]+3} air`);
  cmd(`setblock ${base[0]} 64 ${base[2]} stone`);
  if(c.attachment==="hanging")cmd(`setblock ${base[0]} 66 ${base[2]} stone`);
  if(c.attachment==="side"||c.attachment==="multiple"){
   const support=delta[c.direction];cmd(`setblock ${base[0]-support[0]} 65 ${base[2]-support[2]} stone`);
   if(c.attachment==="multiple")cmd(`setblock ${base[0]+support[0]} 65 ${base[2]+support[2]} stone`);
  }
  cmd(`setblock ${base.join(" ")} bell ["attachment"="${c.attachment}","direction"=${c.direction},"toggle_bit"=false]`);
  const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("audit player missing");
  const feet=[base[0]+.5+v[0]*2,64,base[2]+.5+v[2]*2];p.teleport({x:feet[0],y:feet[1],z:feet[2]});
  current={scenario:c,base,feet,setup_tick:system.currentTick,samples:[],changes:[],results:[],requested:false};
  emit({action:{action:"look",rotation:[[180,-90,0,90][c.approach],0]},block_state:read()});return;
 }
 const state=read();if(state.id!=="minecraft:bell"||state.states.attachment!==current.scenario.attachment)throw new Error("bell use attachment control changed "+JSON.stringify(current.scenario)+JSON.stringify(state));
 const name="audit_bell_use_frame_"+serial++;
 if(!cmd(`structure save ${name} ${current.base.join(" ")} ${current.base.join(" ")} false disk true`).successCount)throw new Error("bell use actor snapshot failed");
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),source:state,snapshot:"mystructure:"+name});lastSample=system.currentTick;
 if(!current.requested&&system.currentTick-current.setup_tick>=8&&(system.currentTick&1)===current.scenario.request_phase){
  const v=delta[current.scenario.approach];emit({action:{action:"click",block:current.base,face:faces[current.scenario.approach],hit:[.5+v[0]*.25,.5,.5+v[2]*.25],feet:current.feet},block_state:read()});
  current.requested=true;current.request_tick=system.currentTick;
 }
 if(current.requested&&!current.changes.length&&system.currentTick-current.request_tick>60)throw new Error("native bell interaction not delivered "+JSON.stringify(current.scenario));
 if(current.changes.length&&system.currentTick-current.changes[0].tick>=36){
  if(current.changes.length!==1||current.results.length>1)throw new Error("missing/duplicate bell interaction "+JSON.stringify({scenario:current.scenario,changes:current.changes,results:current.results}));
  emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def readback(result, helper):
    root = Path(result['log_directory'])
    completed = subprocess.run([str(helper), '-db', str(root/'worlds/redstone-reference/db'),
        '-prefix', 'structuretemplate_mystructure:audit_bell_use_frame_', '-immutable-audit-wal'],
        capture_output=True, text=True, encoding='utf-8', check=True)
    snapshots = {r['name']: r for line in completed.stdout.splitlines() if (r := json.loads(line))}
    used = set()
    packets = []
    for line in (root/'client.log').read_text(encoding='utf-8').splitlines():
        try:
            p = json.loads(line)
        except (TypeError, ValueError):
            continue
        if any(k in p for k in ('sound', 'block_actor', 'block_update', 'subchunk_update', 'block_event', 'level_event')):
            packets.append(p)
    result['packet_observations'] = packets
    for c in result['scenarios']:
        c['packets'] = []
        for p in packets:
            if 'subchunk_update' in p:
                for b in p['subchunk_update']['Blocks']:
                    if b['BlockPos'] == c['base']:
                        c['packets'].append(dict(block_update=dict(Position=b['BlockPos'],NewBlockRuntimeID=b['BlockRuntimeID'],Flags=b['Flags'],Layer=0),received_at_ms=p['received_at_ms']))
            else:
                key = next(k for k in ('sound', 'block_actor', 'block_update', 'block_event', 'level_event') if k in p)
                if p[key]['Position'] == c['base']:
                    c['packets'].append(p)
        for f in c['samples']:
            name = f['snapshot']
            if name in used:
                raise RuntimeError('reused bell use snapshot')
            used.add(name)
            palette = snapshots[name]['structure']['structure']['palette']['default']
            b = palette['block_palette'][0]
            actor = palette['block_position_data']['0']['block_entity_data']
            states = {k: int(v) if isinstance(v, bool) else v for k, v in f['source']['states'].items()}
            if b['name'] != 'minecraft:bell' or b['states'] != states or actor['id'] != 'Bell':
                raise RuntimeError('bell use snapshot controls differ')
            f['actor'] = actor
        sounds = [p for p in c['packets'] if 'sound' in p and p['sound']['SoundType'] == 'block.bell.hit']
        ringing = any(f['actor']['Ringing'] for f in c['samples'])
        expected_yaw = (180, -90, 0, 90)[c['scenario']['approach']]
        yaw = c['changes'][0]['rotation']['y']
        if abs((yaw-expected_yaw+180)%360-180) > .01:
            raise RuntimeError('native player did not retain the requested yaw')
        if len(sounds) != int(ringing) or len(c['results']) != int(ringing):
            raise RuntimeError('native bell use sound/ring controls differ')
    if used != snapshots.keys():
        raise RuntimeError('unexpected bell use snapshot count')
    result['actor_readback'] = dict(method='native per-tick disk structures; stopped read-only LevelDB plus verified immutable WAL',snapshots=len(used),journal=completed.stderr.strip())
    return result


if __name__ == '__main__':
    args = arguments()
    cases = [dict(attachment=a, direction=d, approach=p, request_phase=(d+p)&1)
             for a in ('standing', 'hanging', 'side', 'multiple') for d in range(4) for p in range(4)]
    player_probe.SCRIPT = SCRIPT
    player_probe.capture_player_use(args, cases, sound_prefixes=('block.bell.hit',))
    result = json.loads(args.output.read_text(encoding='utf-8')); result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = readback(result, Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True,scenes=len(cases),snapshots=result['actor_readback']['snapshots'])),flush=True)
