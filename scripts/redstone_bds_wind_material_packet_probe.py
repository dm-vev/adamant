#!/usr/bin/env python3
"""Retain actual BDS packets for wind-material activation and powered rejection."""
import hashlib
import json
import time

from redstone_bds_client import LocalClient
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments
from redstone_bds_wind_material_probe import SCRIPT, material_cases


def capture_material_packets(args):
    cases = material_cases()
    script = SCRIPT.replace('CASES', json.dumps(cases))
    script = replace_once(script, 'let d,loaded=0,ready=false,start=0,cells=[];',
        'let d,loaded=0,ready=false,start=0,cells=[],requested=false,announced=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
    script = replace_once(script,
        'for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} audit_wind_material_${z} true`);',
        'cmd("tickingarea add -16 0 -16 95 100 111 audit_wind_material_packets true");cmd("setworldspawn 36 65 44");')
    script = replace_once(script, '!get([0,65,0])||!get([127,65,383])',
        '!get([-4,65,-4])||!get([80,65,100])')
    script = replace_once(script, '  if(system.currentTick%20!==2)return;',
        '  if(!requested){if(!announced){announced=true;emit({ready:true});}return;}if(system.currentTick%20!==2)return;')
    script = replace_once(script, 'base=[8+i%8*16,65,8+Math.floor(i/8)*16]',
        'base=[4+i%12*6,65,4+Math.floor(i/12)*6]')
    script = replace_once(script, ' const t=system.currentTick-start;',
        ' const t=system.currentTick-start;if(t===10)emit({before_burst:true,tick:system.currentTick});')
    result = dict(reference=__doc__, complete=False,
        binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
        scenarios=[], packet_observations=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=script, local_client=True)
    client = None
    try:
        reference.until(lambda s: 'ADAMANT_TIMING ' in s and '"ready":true' in s, timeout=45)
        client = LocalClient(reference, args.port)
        reference.command('scriptevent audit:start now')
        while True:
            line = reference.until(lambda s: 'ADAMANT_TIMING ' in s, timeout=45)[-1]
            record = json.loads(line.split('ADAMANT_TIMING ', 1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if record.get('before_burst'):
                result['before_burst'] = dict(tick=record['tick'], received_at_ms=int(time.time()*1000))
            if 'scenario' in record:
                result['scenarios'].append(record)
            if record.get('complete'):
                if record['count'] != len(cases) or len(result['scenarios']) != len(cases):
                    raise RuntimeError('incomplete native material packet scenes')
                result['complete'] = True
                break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:
                    p = json.loads(line)
                except (ValueError, TypeError):
                    continue
                if any(k in p for k in ('block_update', 'subchunk_update', 'sound', 'level_event', 'block_event')):
                    result['packet_observations'].append(p)
        reference.stop()
        for c in result['scenarios']:
            positions = c['positions']
            c['packets'] = []
            for p in result['packet_observations']:
                if 'subchunk_update' in p:
                    for b in p['subchunk_update']['Blocks']:
                        if b['BlockPos'] in positions:
                            c['packets'].append(dict(block_update=dict(Position=b['BlockPos'], NewBlockRuntimeID=b['BlockRuntimeID'], Flags=b['Flags'], Layer=0), received_at_ms=p['received_at_ms']))
                else:
                    key = next(k for k in ('block_update', 'sound', 'level_event', 'block_event') if k in p)
                    position = p[key]['Position']
                    cell = [int(v//1) for v in position]
                    if cell in positions:
                        c['packets'].append(p)
            boundary = result.get('before_burst', {}).get('received_at_ms')
            if boundary is None or c['setup_tick']+10 != result['before_burst']['tick']:
                result['complete'] = False
            else:
                # Retain all setup packets too. The explicit Script checkpoint
                # is ten GT before spawning and separates this effects scope;
                # packet timestamps are not used to infer simulation tick order.
                c['effect_packets'] = [p for p in c['packets'] if p['received_at_ms'] >= boundary]
                initial_updates = [p for p in c['packets'] if p['received_at_ms'] < boundary and 'block_update' in p]
                if c['initial'][1]['id'] != 'minecraft:air' and not any(p['block_update']['Position'] == c['target'] for p in initial_updates):
                    result['complete'] = False
        for c in result['scenarios']:
            scenario = c['scenario']
            form, kind = scenario['form'], scenario['kind']
            expected = 0
            if form == 'door' and kind != 'iron_door' and not scenario['powered']:
                expected = 3
            elif form in ('trapdoor', 'gate') and kind != 'iron_trapdoor' and not scenario['powered']:
                expected = 2
            elif form in ('candle', 'cake') and scenario['initial']:
                expected = 2
            if form != 'campfire' and len(c.get('effect_packets', [])) != expected:
                result['complete'] = False
        # Campfire actors can publish their own periodic refresh after the
        # checkpoint. Preserve those packets as controls without labelling
        # their independent actor timing as a wind-interaction result.
        result['log_directory'] = str(reference.root)
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], scenes=len(result['scenarios']), logs=str(reference.root))), flush=True)
    if not result['complete']:
        raise RuntimeError('incomplete wind-material packet capture')


if __name__ == '__main__':
    capture_material_packets(arguments())
