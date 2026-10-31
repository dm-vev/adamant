#!/usr/bin/env python3
"""Capture native bell wind-burst packets, including rejected repeated hits."""
import hashlib
import json
from pathlib import Path

from redstone_bds_bell_projectile_probe import readback
from redstone_bds_bell_breeze_probe import bell_breeze_script
from redstone_bds_bell_wind_probe import bell_wind_cases, bell_wind_script
from redstone_bds_client import LocalClient
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


def capture_bell_wind_packets(args, breeze=False):
    cases = bell_wind_cases()
    if breeze:
        cases = [dict(c, projectile='minecraft:breeze_wind_charge_projectile') for c in cases]
    script = bell_breeze_script(cases) if breeze else bell_wind_script(cases)
    script = replace_once(script, 'let d,loaded=0,ready=false,start=0,cells=[],serial=0;',
        'let d,loaded=0,ready=false,start=0,cells=[],serial=0,requested=false,announced=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});')
    ready = 'if(!requested){if(!announced){announced=true;emit({ready:true});}return;}'
    if breeze:
        script = replace_once(script, 'cmd("tickingarea add 0 0 0 127 100 127 audit_bell_wind_a true");',
            'cmd("tickingarea add -16 0 -16 63 100 63 audit_bell_wind true");cmd("setworldspawn 24 65 20");')
        script = replace_once(script, 'if(!ready){if(!get([0,65,0])||!get([143,65,383]))',
            'if(!ready){if(!get([-4,65,-4])||!get([143,65,383]))')
        script = replace_once(script, '  if(!factoryReady)', '  '+ready+'if(!factoryReady)')
    else:
        script = replace_once(script,
            'cmd("tickingarea add 0 0 0 127 100 127 audit_bell_wind_a true");cmd("tickingarea add 0 0 128 127 100 255 audit_bell_wind_b true");',
            'cmd("tickingarea add -16 0 -16 63 100 63 audit_bell_wind true");cmd("setworldspawn 24 65 20");')
        script = replace_once(script, 'if(!ready){if(!get([0,65,0])||!get([127,65,255]))',
            'if(!ready){if(!get([-4,65,-4])||!get([52,65,52]))')
        script = replace_once(script, '  if(system.currentTick%20!==2)return;',
            '  '+ready+'if(system.currentTick%20!==2)return;')
    script = replace_once(script, 'base=[8+i%8*16,65,8+Math.floor(i/8)*16]',
        'base=[4+i%8*6,65,4+Math.floor(i/8)*6]')
    result = dict(complete=False, reference=__doc__, breeze=breeze,
        binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), scenarios=[], packet_observations=[])
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
            if 'scenario' in record:
                result['scenarios'].append(record)
            if record.get('complete'):
                if record['count'] != len(cases) or len(result['scenarios']) != len(cases):
                    raise RuntimeError('incomplete native bell wind packet scenes')
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
                if any(k in p for k in ('block_actor', 'block_update', 'subchunk_update', 'block_event', 'sound', 'level_event')):
                    result['packet_observations'].append(p)
        reference.stop()
        for c in result['scenarios']:
            c['packets'] = []
            for p in result['packet_observations']:
                if 'subchunk_update' in p:
                    for b in p['subchunk_update']['Blocks']:
                        if b['BlockPos'] == c['base']:
                            c['packets'].append(dict(block_update=dict(Position=b['BlockPos'], NewBlockRuntimeID=b['BlockRuntimeID'], Flags=b['Flags'], Layer=0), received_at_ms=p['received_at_ms']))
                else:
                    key = next(k for k in ('block_actor', 'block_update', 'block_event', 'sound', 'level_event') if k in p)
                    if p[key]['Position'] == c['base']:
                        c['packets'].append(p)
            sounds = [p for p in c['packets'] if 'sound' in p and p['sound']['SoundType'] == 'block.bell.hit']
            if len(sounds) != 1:
                result['complete'] = False
                result['error'] = 'native bell wind ring count differs'
        result['log_directory'] = str(reference.root)
        complete = result['complete']
        result['complete'] = False
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        if complete:
            result = readback(result, Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
            result['complete'] = True
            args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], scenes=len(result['scenarios']), logs=str(reference.root))), flush=True)
        if not result['complete']:
            raise RuntimeError('native bell wind packet capture incomplete')


if __name__ == '__main__':
    capture_bell_wind_packets(arguments())
