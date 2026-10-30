#!/usr/bin/env python3
"""Authored note actor loads: unsigned pitch clamp and ignored private power tags."""
import json

from redstone_audit_structure import byte, compound, string, one_block
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_note_pulse_probe import SCRIPT as BASE_SCRIPT
from redstone_bds_timing_probe import arguments, capture


def note_load_cases():
    return [dict(below='air',above='air',pitch=pitch,phase=0,edges=[0,8,20,28],
                 initial_power=True,stored_powered=powered)
            for pitch in (0,12,24,25,127,255) for powered in (False,True)]


def note_load_structures():
    return {f'audit/note_load_{pitch}_{int(powered)}':one_block('minecraft:noteblock',{},
        {'id':string('Music'),'note':byte(pitch if pitch<128 else pitch-256),
         'adamant:java':compound({'powered':byte(powered)})})
        for pitch in (0,12,24,25,127,255) for powered in (False,True)}


def note_load_script():
    script=replace_once(BASE_SCRIPT,'audit:note_${scenario.pitch}',
                         'audit:note_load_${scenario.pitch}_${Number(scenario.stored_powered)}')
    return replace_once(script,'   if(!cmd(`structure load ',
        '   if(scenario.initial_power)cmd(`setblock ${source.join(" ")} redstone_block`);\n'
        '   if(!cmd(`structure load ')


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    cases=note_load_cases()
    capture(args,note_load_script().replace('CASES',json.dumps(cases)),len(cases),structures=note_load_structures())
