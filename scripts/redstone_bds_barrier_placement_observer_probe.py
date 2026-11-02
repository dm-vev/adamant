#!/usr/bin/env python3
"""Compare placer-only and observer-visible native barrier placement packets."""
import copy
import hashlib
import json
import time
from pathlib import Path

from redstone_bds_barrier_placement_probe import CASES, SCRIPT
from redstone_bds_barrier_use_probe import scope_packets
from redstone_bds_client import LocalClient, network_state_hash
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


def capture(args, source_script=SCRIPT, kind='actual_player_placement_observer', drain=None):
    cases = [c for c in CASES if c['direction'] == 0]
    script = source_script.replace('CASES', json.dumps(cases))
    script = replace_once(script,
        'p.teleport({x:feet[0],y:feet[1],z:feet[2]});',
        'p.teleport({x:feet[0],y:feet[1],z:feet[2]});'
        'const observer=world.getAllPlayers().find(p=>p.name==="RedstoneObserver");'
        'if(!observer)throw new Error("native observer missing");'
        'observer.teleport({x:feet[0]+v[1]*3,y:feet[1],z:feet[2]-v[0]*3});')
    script = replace_once(script,
        'if(elapsed===12)player().sendMessage(',
        'if(elapsed===12)for(const witness of world.getAllPlayers())witness.sendMessage(')
    result = dict(complete=False, reference=__doc__, kind=kind,
        binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(), scenarios=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port, script=script, local_client=True)
    actor = observer = None
    executable = Path(__file__).resolve().parents[1]/'.tmp/redstone-client-placement.exe'
    try:
        reference.until(lambda s: 'ADAMANT_USE ' in s and '"ready":true' in s, timeout=45)
        actor = LocalClient(reference, args.port, executable=executable)
        observer = LocalClient(reference, args.port, executable=executable,
            name='RedstoneObserver', log_filename='observer.log')
        reference.command('op RedstoneAudit')
        reference.command('scriptevent audit:start now')
        while True:
            line = reference.until(lambda s: 'ADAMANT_USE ' in s, timeout=25)[-1]
            record = json.loads(line.split('ADAMANT_USE ', 1)[1])
            if record.get('error'):
                raise RuntimeError(record)
            if 'action' in record:
                record['action']['runtime_id'] = network_state_hash(record['block_state'])
                actor.send(record['action'])
            if 'scenario' in record:
                result['scenarios'].append(record)
                print(json.dumps(dict(count=len(result['scenarios']), scenario=record['scenario'])), flush=True)
            if record.get('complete'):
                if record['count'] != len(cases) or len(result['scenarios']) != len(cases):
                    raise RuntimeError('incomplete actual placement/observer corpus')
                if {c['changes'][0]['game_tick'] & 1 for c in result['scenarios']} != {0, 1}:
                    raise RuntimeError('native actual placement input parity control failed')
                break
        if drain:
            deadline = time.monotonic()+5
            while not all(drain(client.log, result['scenarios']) for client in (actor, observer)):
                if time.monotonic() >= deadline:
                    raise RuntimeError('native final clock marker did not drain on both clients')
                if any(client.process.poll() is not None for client in (actor, observer)):
                    raise RuntimeError('native client exited before its final clock marker')
                time.sleep(.01)
        result['complete'] = True
    finally:
        if actor:
            actor.stop()
        if observer:
            observer.stop()
        reference.stop()
        result['log_directory'] = str(reference.root)
        result['build_log'] = [s for s in reference.log if 'Version:' in s or 'Build ID:' in s]
        args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
        print(json.dumps(dict(complete=result['complete'], logs=str(reference.root))), flush=True)
    if not result['complete']:
        raise RuntimeError('incomplete actual placement/observer capture')
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = scope_packets(result)
    observed = scope_packets(copy.deepcopy(result), log_filename='observer.log')
    result['observer_packet_observations'] = observed['packet_observations']
    for c, o in zip(result['scenarios'], observed['scenarios']):
        c['observer_packets'] = o['packets']
        c['observer_checkpoint'] = o['checkpoint']
        c['observer_effect_packets'] = o['effect_packets']
        if len(c['changes']) != 1 or len(c['results']) != 1:
            raise RuntimeError('native actual placement event count differs')
        for role in ('effect_packets', 'observer_effect_packets'):
            if not any('block_update' in p for p in c[role]) or not any('sound' in p for p in c[role]):
                raise RuntimeError('native observer/placer interaction positive control failed')
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenes=len(cases),
        placer_effects=sum(len(c['effect_packets']) for c in result['scenarios']),
        observer_effects=sum(len(c['observer_effect_packets']) for c in result['scenarios']))), flush=True)


if __name__ == '__main__':
    capture(arguments())
