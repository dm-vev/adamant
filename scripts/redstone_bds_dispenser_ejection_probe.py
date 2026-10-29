#!/usr/bin/env python3
"""Capture ordinary dispenser/dropper item ejection without special item actions."""
import json

from redstone_audit_structure import byte, string, listing, one_block
from redstone_bds_crafter_ejection_probe import SCRIPT as CRAFTER_SCRIPT
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


def ejection_cases():
    return [dict(kind=kind, face=face, phase=phase)
            for kind in ('dropper', 'dispenser') for face in range(6) for phase in (0, 1)]


def ejection_structures():
    stacks = [{'Slot': byte(4), 'Name': string('minecraft:iron_ingot'),
               'Count': byte(3), 'Damage': (2, 0)}]
    return {f'audit/{kind}_eject_{face}': one_block(f'minecraft:{kind}',
        {'facing_direction': (3, face), 'triggered_bit': byte(0)},
        {'id': string(kind.title()), 'Items': listing(10, stacks)})
        for kind in ('dropper', 'dispenser') for face in range(6)}


def ejection_script():
    script = replace_once(CRAFTER_SCRIPT, 'audit:crafter_eject_${scenario.face}',
                          'audit:${scenario.kind}_eject_${scenario.face}')
    script = replace_once(script, 'z<192;z+=96', 'z<384;z+=96')
    script = replace_once(script, 'z<192;z+=16', 'z<384;z+=16')
    script = replace_once(script, 'block:{id:b.typeId,states:b.permutation.getAllStates()},',
        'block:{id:b.typeId,states:b.permutation.getAllStates()},'
        'slots:Array.from({length:9},(_,i)=>{const s=b.getComponent("minecraft:inventory").container.getItem(i);'
        'return s?{id:s.typeId,count:s.amount}:null;}),')
    return replace_once(script, 'first.drops[0].id!=="minecraft:iron_block"',
                         'first.drops[0].id!=="minecraft:iron_ingot"')


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = ejection_cases()
    capture(args, ejection_script().replace('CASES', json.dumps(cases)), len(cases),
            structures=ejection_structures())
