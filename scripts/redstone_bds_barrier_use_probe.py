#!/usr/bin/env python3
"""Capture actual empty-hand player use of native barriers, including steady power.

No scripted permutation toggle stands in for interaction. The loopback client
sends real use transactions; before/after events retain their actual tick and
the sampled world state. Each scene has distinct coordinates for packet scope.
"""
import json

import redstone_bds_switch_use_probe as player_probe
from redstone_bds_timing_probe import arguments


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,running=false,index=0,current=null,ready=false,lastSample=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const blocks=()=>current.positions.map(read);
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
function ours(e){return current&&e.player.name==="RedstoneAudit"&&e.block.location.x===current.target[0]&&e.block.location.y===current.target[1]&&e.block.location.z===current.target[2];}
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(ours(e))current.changes.push({kind:"click",tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,blocks:blocks(),rotation:e.player.getRotation()});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
 if(ours(e))current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),blocks:blocks()});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 79 100 79 audit_barrier_use true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,65,0])||!get([72,65,72])){if(system.currentTick-load>480)throw new Error("barrier use chunks not ready");return;}
  cmd("fill -3 63 -3 3 63 3 stone");cmd("setworldspawn 0 64 2");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!current){const c=cases[index],base=[4+index%12*6,65,4+Math.floor(index/12)*6],door=c.form==="door";
  cmd(`fill ${base[0]-2} 63 ${base[2]-2} ${base[0]+2} 64 ${base[2]+2} stone`);
  cmd(`fill ${base[0]-2} 65 ${base[2]-2} ${base[0]+2} 68 ${base[2]+2} air`);
  const sources=[[base[0]+1,65,base[2]],[base[0]+1,66,base[2]]];
  if(c.feed!=="none")cmd(`setblock ${sources[c.feed==="upper"?1:0].join(" ")} redstone_block`);
  const state=door?`"minecraft:cardinal_direction"="north","door_hinge_bit"=false,"upper_block_bit"=false`:
   c.form==="trapdoor"?`"direction"=0,"upside_down_bit"=${c.top}`:`"minecraft:cardinal_direction"="north","in_wall_bit"=false`;
  cmd(`setblock ${base.join(" ")} ${c.block} [${state},"open_bit"=${c.initial}]`);
  if(door)cmd(`setblock ${base[0]} 66 ${base[2]} ${c.block} ["minecraft:cardinal_direction"="north","door_hinge_bit"=false,"upper_block_bit"=true,"open_bit"=${c.initial}]`);
  const target=[base[0],65+(door&&c.top?1:0),base[2]],v=c.approach===0?1:-1;
  const feet=[base[0]+.5,65,base[2]+.5+v*2];
  const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("audit player missing");
  p.teleport({x:feet[0],y:feet[1],z:feet[2]});
  current={scenario:c,base,target,feet,sources,positions:door?[base,[base[0],66,base[2]]]:[base],setup_tick:system.currentTick,samples:[],changes:[],results:[],requests:[]};
  current.initial=blocks();current.initial_sources=sources.map(read);
  emit({action:{action:"look",rotation:[c.approach===0?180:0,0]},block_state:read(target)});return;
 }
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),blocks:blocks(),sources:current.sources.map(read)});lastSample=system.currentTick;
 const elapsed=system.currentTick-current.setup_tick;
 if(elapsed===12){const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
  p.sendMessage("ADAMANT_BARRIER_USE "+JSON.stringify({index,tick:system.currentTick,stage:"settled"}));}
 if((current.requests.length===0&&elapsed>=16||current.requests.length===1&&elapsed-current.requests[0].elapsed>=8)&&
    (system.currentTick&1)===current.scenario.request_phase){
  current.requests.push({tick:system.currentTick,elapsed});
  emit({action:{action:"click",block:current.target,face:current.scenario.approach===0?3:2,hit:[.5,.5,.5],feet:current.feet},block_state:read(current.target)});
 }
 if(current.requests.length===2&&system.currentTick-current.requests[1].tick>=14){
  if(current.changes.length!==2)throw new Error("missing/duplicate actual barrier use "+JSON.stringify({scenario:current.scenario,changes:current.changes}));
  emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def cases():
    result = []
    for form, names in (
        ('door', ('wooden_door', 'copper_door', 'iron_door')),
        ('trapdoor', ('trapdoor', 'copper_trapdoor', 'iron_trapdoor')),
        ('gate', ('fence_gate',)),
    ):
        for name in names:
            for top in ((False, True) if form != 'gate' else (False,)):
                for approach in ((0, 2) if form == 'gate' else (0,)):
                    for initial in (False, True):
                        for feed in (('none', 'lower', 'upper') if form == 'door' else ('none', 'lower')):
                            for phase in (0, 1):
                                result.append(dict(form=form, block=name, top=top, approach=approach,
                                                   initial=initial, feed=feed, request_phase=phase))
    return result


def scope_packets(result, log_filename='client.log'):
    from pathlib import Path
    packets = []
    for line in (Path(result['log_directory'])/log_filename).read_text(encoding='utf-8').splitlines():
        try:
            record = json.loads(line)
        except (TypeError, ValueError):
            continue
        if 'text' in record:
            message = record['text']['Message']
            if message.lstrip().startswith('{'):
                message = ''.join(x.get('text', '') for x in json.loads(message).get('rawtext', []))
            if message.startswith('ADAMANT_BARRIER_USE '):
                record['marker'] = json.loads(message[len('ADAMANT_BARRIER_USE '):])
        if any(k in record for k in ('sound', 'block_update', 'subchunk_update', 'level_event', 'block_event', 'marker')):
            packets.append(record)
    result['packet_observations'] = packets
    markers = {p['marker']['index']: p for p in packets if 'marker' in p}
    if len(markers) != len(result['scenarios']):
        raise RuntimeError('native player-use packet checkpoint missing')
    for index, scene in enumerate(result['scenarios']):
        checkpoint = markers[index]
        if checkpoint['marker']['tick'] != scene['setup_tick']+12:
            raise RuntimeError('native player-use checkpoint tick differs')
        scene['checkpoint'] = checkpoint
        scene['packets'] = []
        for record in packets:
            if 'marker' in record:
                continue
            if 'subchunk_update' in record:
                for b in record['subchunk_update']['Blocks']:
                    if b['BlockPos'] in scene['positions']:
                        scene['packets'].append(dict(block_update=dict(Position=b['BlockPos'],
                            NewBlockRuntimeID=b['BlockRuntimeID'],Flags=b['Flags'],Layer=0),
                            received_at_ms=record['received_at_ms'],packet_sequence=record['packet_sequence']))
            else:
                key = next(k for k in ('sound', 'block_update', 'level_event', 'block_event') if k in record)
                p = record[key]['Position']
                if [int(v//1) for v in p] in scene['positions']:
                    scene['packets'].append(record)
        scene['effect_packets'] = [p for p in scene['packets'] if p['packet_sequence']>checkpoint['packet_sequence']]
    return result


def read_packets(result):
    result = scope_packets(result)
    for scene in result['scenarios']:
        expected = 0 if scene['scenario']['block'].startswith('iron_') else 2
        if len(scene['results']) != expected:
            raise RuntimeError('native actual player use acceptance control failed: '+str(scene['scenario']))
    return result


if __name__ == '__main__':
    args = arguments()
    player_probe.SCRIPT = SCRIPT
    player_probe.capture_player_use(args, cases(), sound_prefixes=('door.', 'trapdoor.', 'fence_gate.'))
    result = json.loads(args.output.read_text(encoding='utf-8'))
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = read_packets(result)
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenes=len(result['scenarios']))), flush=True)
