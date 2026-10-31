#!/usr/bin/env python3
"""Observe real BDS player wind-charge hits and repeated attacks on bells.

Breeze projectiles are not summonable in the unmodified native definition.
They require a separate real-mob attack capture, not a definition override.
"""
import json
from pathlib import Path

from redstone_bds_bell_projectile_probe import SCRIPT, readback
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


def bell_wind_cases():
    return [dict(attachment=a, direction=d, approach=(d+p)%4, phase=p,
                 projectile=k, repeat=r)
            for a in ('standing', 'hanging', 'side', 'multiple')
            for d in range(4)
            for k in ('minecraft:wind_charge_projectile',)
            for r in (False, True) for p in range(2)]


def bell_wind_script(cases):
    script = SCRIPT.replace('CASES', json.dumps(cases))
    script = replace_once(script,
        'cmd("tickingarea add 0 0 0 127 100 95 audit_bell_arrow_a true");cmd("tickingarea add 0 0 96 127 100 127 audit_bell_arrow_b true");',
        'cmd("tickingarea add 0 0 0 127 100 127 audit_bell_wind_a true");cmd("tickingarea add 0 0 128 127 100 255 audit_bell_wind_b true");')
    script = replace_once(script, '!get([127,65,127])', '!get([127,65,255])')
    script = replace_once(script, 'entity:null,spawn:null', 'entity:null,spawn:null,spawns:[]')
    script = replace_once(script, 'if(t===40+c.scenario.phase)',
        'if(t===40+c.scenario.phase||(c.scenario.repeat&&t===50+c.scenario.phase))')
    script = replace_once(script, 'd.spawnEntity("minecraft:arrow",position)',
        'd.spawnEntity(c.scenario.projectile,position)')
    script = replace_once(script,
        'c.spawn={id:arrow.typeId,requested_position:position,position:arrow.location,velocity:arrow.getVelocity()};c.changes.push({tick:system.currentTick,kind:"spawn"});',
        'const birth={id:arrow.typeId,requested_position:position,position:arrow.location,velocity:arrow.getVelocity()};c.spawn??=birth;c.spawns.push(birth);c.changes.push({tick:system.currentTick,kind:"spawn",spawn:birth});')
    script = replace_once(script,
        'if(!c.spawn||!c.hits.length)throw new Error("native arrow collision control missing")',
        'if(c.spawns.length!==(c.scenario.repeat?2:1)||!c.hits.length)throw new Error("native wind collision control missing: "+JSON.stringify(c.scenario))')
    return script


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = bell_wind_cases()
    capture(args, bell_wind_script(cases), len(cases))
    result = json.loads(args.output.read_text(encoding='utf-8'))
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = readback(result, Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenes=len(cases), snapshots=result['actor_readback']['snapshots'])), flush=True)
