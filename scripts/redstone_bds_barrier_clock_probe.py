#!/usr/bin/env python3
"""Bound barrier packet delivery with native Script tick markers on one client.

Markers and block/sound packets are retained in client receive order. They
bound delivery relative to the Script interval; a marker is not itself proof
of a lower-level native sender's simulation-tick phase.
"""
import hashlib
import json
from pathlib import Path
import time

from redstone_bds_barrier_probe import SCRIPT, barrier_cases
from redstone_bds_client import LocalClient
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


def tick_marker(packet):
    if 'text' not in packet:
        return None
    message = packet['text']['Message']
    if message.lstrip().startswith('{'):
        raw = json.loads(message)
        message = ''.join(entry.get('text', '') for entry in raw.get('rawtext', []))
    if message.startswith('ADAMANT_CLOCK '):
        return json.loads(message[len('ADAMANT_CLOCK '):])
    return None


def clock_script(cases, source_script=SCRIPT):
    script = source_script.replace('CASES', json.dumps(cases))
    script = replace_once(script, 'let d,loaded=0,ready=false,start=0,cells=[];',
        'let d,loaded=0,ready=false,start=0,cells=[],requested=false,announced=false;'
        'system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")requested=true;});'
        'function marker(stage){const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");'
        'if(!p)throw new Error("clock witness missing");'
        'p.sendMessage("ADAMANT_CLOCK "+JSON.stringify({tick:system.currentTick,game_tick:world.getAbsoluteTime(),stage}));}')
    script = replace_once(script,
        'for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} barrier_audit_${z} true`);',
        'cmd("tickingarea add -16 0 -16 63 100 63 barrier_clock true");cmd("setworldspawn 24 65 20");')
    script = replace_once(script, '!get([0,65,0])||!get([127,65,383])',
        '!get([-4,65,-4])||!get([52,65,40])')
    script = replace_once(script, '  if(system.currentTick%20!==2)return;',
        '  if(!requested){if(!announced){announced=true;emit({ready:true});}return;}if(system.currentTick%20!==2)return;')
    script = replace_once(script, 'base=[8+i%8*16,65,8+Math.floor(i/8)*16]',
        'base=[4+i%8*6,65,4+Math.floor(i/8)*6]')
    script = replace_once(script, ' const t=system.currentTick-start;',
        ' const t=system.currentTick-start;marker("before_inputs");')
    return replace_once(script, ' if(t===108)', ' marker("after_inputs");if(t===108)')


def capture_clock(args, wind=False):
    if wind:
        from redstone_bds_wind_material_probe import SCRIPT as WIND_SCRIPT, material_cases
        kinds = {c['kind'] for c in barrier_cases()}
        cases = [c for c in material_cases() if c['kind'] in kinds and not c['powered']]
        # Reuse the same marker and client setup adapters against the wind pack.
        script = replace_once(WIND_SCRIPT,
            'for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} audit_wind_material_${z} true`);',
            'for(let z=0;z<3;z++)cmd(`tickingarea add 0 0 ${z*128} 127 100 ${z*128+127} barrier_audit_${z} true`);')
        # Both controls keep 108 frames, including the quiet post-impact tail.
        script = clock_script(cases, replace_once(script, 'if(t===56)', 'if(t===108)'))
    else:
        cases = [c for c in barrier_cases() if not c['initial'] and not c['powered'] and c['feed'] != 'split']
        script = clock_script(cases)
    result = dict(reference=__doc__, complete=False,
        kind='wind' if wind else 'redstone', binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), scenarios=[], transcript=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
        script=script, local_client=True)
    client = None
    try:
        reference.until(lambda s: 'ADAMANT_TIMING ' in s and '"ready":true' in s, timeout=45)
        client = LocalClient(reference, args.port,
            executable=Path(__file__).resolve().parents[1]/'.tmp/redstone-client-clock.exe')
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
                    raise RuntimeError('incomplete native barrier clock scenes')
                result['complete'] = True
                break
        # BDS has emitted the final Script record, but its outgoing text queue
        # may not flush until the following network pass. Observe the actual
        # final marker before stopping the client rather than losing that tail.
        deadline = time.monotonic()+5
        final_tick = result['scenarios'][0]['setup_tick']+108
        while time.monotonic() < deadline:
            found = False
            for line in reversed(client.log):
                try:
                    marker = tick_marker(json.loads(line))
                except (ValueError, TypeError):
                    continue
                if marker and marker['tick'] == final_tick and marker['stage'] == 'after_inputs':
                    found = True
                    break
            if found:
                break
            time.sleep(.02)
        else:
            raise RuntimeError('native final clock marker was not received')
    finally:
        if client:
            client.stop()
            for line in client.log:
                try:
                    p = json.loads(line)
                except (ValueError, TypeError):
                    continue
                if not any(k in p for k in ('block_update', 'subchunk_update', 'sound', 'level_event', 'text')):
                    continue
                if 'text' in p:
                    marker = tick_marker(p)
                    if marker is None:
                        continue
                    p['marker'] = marker
                result['transcript'].append(p)
        reference.stop()
        markers = [p['marker'] for p in result['transcript'] if 'marker' in p]
        if len(markers) != 216 or len(result['scenarios']) != 32:
            result['complete'] = False
        else:
            start = result['scenarios'][0]['setup_tick']
            expected = [dict(tick=start+t, stage=stage) for t in range(1, 109) for stage in ('before_inputs', 'after_inputs')]
            if [dict(tick=m['tick'], stage=m['stage']) for m in markers] != expected:
                result['complete'] = False
            sequences = [p['packet_sequence'] for p in result['transcript']]
            if any(b <= a for a, b in zip(sequences, sequences[1:])):
                result['complete'] = False
        result['log_directory'] = str(reference.root)
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], scenes=len(result['scenarios']), markers=len(markers), logs=str(reference.root))), flush=True)
    if not result['complete']:
        raise RuntimeError('incomplete native barrier clock capture')


if __name__ == '__main__':
    capture_clock(arguments())
