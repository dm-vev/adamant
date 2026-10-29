#!/usr/bin/env python3
"""Observe native crafter pulse states and barrel output, without inventory API assumptions."""
import json

from redstone_bds_dispenser_pulse_probe import SCRIPT
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_hopper_crafter_slots_probe import crafter_structure
from redstone_bds_timing_probe import arguments, capture


def crafter_pulse_script():
    script=replace_once(SCRIPT, "set(base,scenario.kind+' [\"facing_direction\"=2]');",
        'set(base,\'crafter ["orientation"="north_up"]\');'
        'set([base[0],base[1],base[2]-1],"barrel");')
    script=replace_once(script,
        'const inv=block(c.base).getComponent("minecraft:inventory")?.container;if(!inv)throw new Error("missing dispenser/dropper inventory");',
        'const inv=block([c.base[0],c.base[1],c.base[2]-1]).getComponent("minecraft:inventory")?.container;'
        'if(!inv)throw new Error("missing output barrel");')
    script=replace_once(script,'items:inv.getItem(0)?.amount??0',
        'items:Array.from({length:inv.size},(_,i)=>{const s=inv.getItem(i);return s?{id:s.typeId,count:s.amount}:null;})')
    script=replace_once(script,'inv.setItem(0,new ItemStack("minecraft:diamond",64));c.load_tick=system.currentTick;',
        'if(!cmd(`structure load audit:crafter_${c.scenario.recipe} ${c.base.join(" ")}`).successCount)throw new Error("crafter input load failed");'
        'c.load_tick=system.currentTick;')
    return script


if __name__=='__main__':
    args=arguments()
    args.offline_reference=True
    edges=([0,1],[0,2],[0,3],[0,4],[0,6],[0,8],[0,12],[0,4,6,10],[0,4,16,20])
    cases=[dict(kind='crafter',face=4,conducted=False,phase=phase,edges=e,recipe=recipe)
           for recipe in ('iron_block','invalid') for phase in (0,1) for e in edges]
    structures={
        'audit/crafter_iron_block':crafter_structure([dict(id='minecraft:iron_ingot',count=8)]*9),
        'audit/crafter_invalid':crafter_structure([dict(id='minecraft:dirt',count=8)]*9),
    }
    capture(args,crafter_pulse_script().replace('CASES',json.dumps(cases)),len(cases),structures=structures)
