#!/usr/bin/env python3
"""Capture dispenser/dropper pulses from recreated buttons, including conduction.

Changing only button_pressed_bit does not update an existing native producer.
The source is removed/recreated at each edge; this probes source registration,
not the player-use entry point.
"""
import json

from redstone_bds_dispenser_pulse_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


def replace_once(script, before, after):
    if script.count(before) != 1:
        raise ValueError('base pulse script no longer has the expected injection point')
    return script.replace(before, after)


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    script = replace_once(SCRIPT, 'if(scenario.conducted)set(support,"stone");',
                          'if(scenario.conducted)set(support,"stone");'
                          'set(source,\'stone_button ["facing_direction"=\'+scenario.face+\',"button_pressed_bit"=false]\');')
    script = replace_once(script, 'set(c.source,powered?"redstone_block":"air")',
                          'set(c.source,"air");set(c.source,\'stone_button ["facing_direction"=\'+c.scenario.face+\',"button_pressed_bit"=\'+powered+\']\')')
    script = replace_once(script, 'block:read(c.base),items:', 'block:read(c.base),source:read(c.source),items:')
    script = replace_once(script, 'const inv=block(c.base)',
                          'if(read(c.source).id!=="minecraft:stone_button")throw new Error("button lost support");const inv=block(c.base)')
    script = replace_once(script, 'if(c.changes.length!==c.scenario.edges.length)',
                          'if(c.samples[c.samples.length-1].items===64)throw new Error("button pulse positive control failed "+JSON.stringify(c.scenario));if(c.changes.length!==c.scenario.edges.length)')
    cases = [dict(kind=kind, face=face, conducted=conducted, phase=phase, edges=edges, source_kind='stone_button')
             for kind in ('dispenser', 'dropper') for face in range(6)
             for conducted in (False, True) for phase in (0, 1)
             for edges in ([0, 8], [0, 12], [0, 12, 24, 36])]
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
