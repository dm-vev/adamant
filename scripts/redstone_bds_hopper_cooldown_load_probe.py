#!/usr/bin/env python3
"""Compare authored hopper TransferCooldown NBT loading at both tick parities.

This exercises the native structure loader on an existing empty hopper. It is
not a claim of chunk unload/reload or world-save equivalence.
"""
import json

from redstone_audit_structure import byte, integer, string, listing, one_block
from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = replace_once(slot_script(), 'if(t===40)', 'if(t===c.scenario.load)')
    start = script.index('for(const [pos,items] of [[c.base,c.scenario.initial_source]')
    end = script.index('c.load_control=', start)
    script = script[:start] + 'if(!cmd(`structure load ${c.scenario.structure.replace("/",":")} ${c.base.join(" ")}`).successCount)throw new Error("hopper structure load failed");' + script[end:]
    cases, structures = [], {}
    for cooldown in (-3, -1, 0, 1, 2, 3, 4, 5, 8, 2147483647):
        name = f'audit/hopper_cooldown_{str(cooldown).replace("-", "m")}'
        structures[name] = one_block('minecraft:hopper', {'facing_direction': integer(2), 'toggle_bit': byte(0)}, {
            'id': string('Hopper'), 'TransferCooldown': integer(cooldown),
            'Items': listing(10, [{'Name': string('minecraft:diamond'), 'Count': byte(4), 'Damage': (2, 0), 'Slot': byte(0)}]),
        })
        for load in (40, 41):
            cases.append(dict(kind='hopper', face=2, target='barrel', fill='cooldown_load', phase=0,
                              load=load, cooldown=cooldown, structure=name,
                              initial_source=[dict(id='minecraft:diamond', count=4)] + [None] * 4,
                              initial_target=[None] * 27))
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases), structures=structures)
