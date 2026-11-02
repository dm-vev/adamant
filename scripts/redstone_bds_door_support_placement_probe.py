#!/usr/bin/env python3
"""Capture actual door placements and rejections on the native support surfaces."""
import argparse
import json
from pathlib import Path

from redstone_bds_barrier_placement_probe import ITEMS, SCRIPT
from redstone_bds_door_support_surfaces_probe import SURFACES
from redstone_bds_switch_use_probe import capture_player_use

CASES = [dict(block=b, held_network_id=rid, form='door', direction=0, powered=False,
              request_phase=i & 1, support=surface)
         for b, rid, form in ITEMS if form == 'door'
         for i, surface in enumerate(SURFACES)]

def replace_once(source, old, new):
    if source.count(old) != 1:
        raise ValueError('native placement probe template changed: '+old)
    return source.replace(old, new)

SOURCE = SCRIPT.replace('{world,system}', '{world,system,BlockPermutation}')
SOURCE = replace_once(SOURCE, 'const blocks=()=>', r'''
const networkState=b=>{
 const states={...b.states},alias=(k,want)=>{if(k in states){if(states[k]!==want)throw new Error("unexpected native alias "+k);delete states[k];}};
 if(b.id.startsWith("minecraft:oak_"))alias("wood_type","oak");
 if(b.id==="minecraft:oak_leaves")alias("old_leaf_type","oak");
 if(b.id==="minecraft:cobblestone_wall")alias("wall_block_type","cobblestone");
 if(b.id==="minecraft:white_carpet")alias("color","white");
 if("minecraft:vertical_half" in states)alias("top_slot_bit",states["minecraft:vertical_half"]==="top");
 return {id:b.id,states};
};
const blocks=()=>''')
SOURCE = replace_once(SOURCE, '  if(c.powered)cmd(`setblock ${support.join(" ")} redstone_block`);',
    '  cmd(`setblock ${support[0]} 63 ${support[2]} stone`);get(support).setPermutation(BlockPermutation.resolve("minecraft:"+c.support.id,c.support.states||{}));')
SOURCE = replace_once(SOURCE, 'positions:c.form==="door"?[base,[base[0],66,base[2]]]:[base]',
    'positions:[support,base,[base[0],66,base[2]]]')
SOURCE = SOURCE.replace('block_state:read(support)', 'block_state:networkState(read(support))')
SOURCE = SOURCE.replace('block_state:read(current.support)', 'block_state:networkState(read(current.support))')
SOURCE = replace_once(SOURCE, 'if(current.requested&&!current.results.length&&system.currentTick-current.requested>60)',
    'if(current.requested&&!current.changes.length&&system.currentTick-current.requested>60)')
SOURCE = replace_once(SOURCE, 'if(current.results.length&&system.currentTick-current.results[0].tick>=20){',
    'if(current.requested&&current.changes.length&&system.currentTick-current.requested>=20){')
SOURCE = replace_once(SOURCE,
    '  if(current.changes.length!==1||current.changes[0].held!=="minecraft:"+current.scenario.block||current.changes[0].count!==2||blocks().some(b=>b.id!=="minecraft:"+current.scenario.block))throw new Error("native barrier placement control failed "+JSON.stringify(current));', r'''
  const held=player().getComponent("minecraft:inventory").container.getItem(0),doors=blocks().filter(b=>b.id==="minecraft:"+current.scenario.block);
  current.accepted=doors.length===2;current.final=blocks();current.final_held={id:held?.typeId,count:held?.amount};
  if(current.changes.length!==1||current.changes[0].held!=="minecraft:"+current.scenario.block||current.changes[0].count!==2||
   (doors.length!==0&&doors.length!==2)||current.results.length!==(current.accepted?1:0)||held?.typeId!=="minecraft:"+current.scenario.block||held?.amount!==(current.accepted?1:2))
   throw new Error("native door support placement control failed "+JSON.stringify(current));''')

def validate_capture(path):
    """Retain the client's native cached block hash beside each input hash."""
    result = json.loads(path.read_text(encoding='utf-8'))
    if not result['complete']:
        raise ValueError('cannot validate an incomplete native capture')
    result['complete'] = False
    path.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    entries = [json.loads(line) for line in (Path(result['log_directory'])/'client.log').read_text(encoding='utf-8').splitlines()
               if line.startswith('{')]
    queued = [e for e in entries if e.get('queued') == 'click']
    sent = [e for e in entries if e.get('sent') == 'interaction']
    if len(queued) != len(sent) or len(sent) != len(result['scenarios']):
        raise ValueError('native placement action count differs')
    for q, s, scene in zip(queued, sent, result['scenarios']):
        if s['block'] != scene['support'] or q['rid'] != s['rid']:
            raise ValueError('native placement action does not match the cached server block hash')
        scene['packet_input'] = dict(cached_runtime_id=q['rid'], sent_runtime_id=s['rid'], position=s['block'])
    result['complete'] = True
    path.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'assets', 'output', 'client-executable'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--port', type=int, default=30952)
    parser.add_argument('--limit', type=int, default=len(CASES))
    args = parser.parse_args()
    if not 1 <= args.limit <= len(CASES):
        parser.error('invalid native support placement limit')
    capture_player_use(args, CASES[:args.limit], require_parities=False, source_script=SOURCE,
                       client_executable=args.client_executable.resolve(), sound_prefixes=())
    validate_capture(args.output)
