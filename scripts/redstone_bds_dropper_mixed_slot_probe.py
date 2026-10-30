#!/usr/bin/env python3
"""Capture dropper container routing with distinct items in multiple source slots."""
import json

from redstone_bds_hopper_slot_transfer_probe import slot_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments,capture


def mixed_cases():
    stack=lambda name,n:dict(id='minecraft:'+name,count=n)
    diamond,dirt,iron=stack('diamond',4),stack('dirt',4),stack('iron_ingot',4)
    patterns=(
        ('three_items',[diamond,dirt,iron]+[None]*6),
        ('holes',[None,diamond,None,dirt]+[None]*4+[iron]),
        ('first_depletes',[stack('diamond',1),dirt,iron]+[None]*6),
        ('later_only',[None]*6+[diamond,dirt,iron]),
        ('last_empty',[diamond,dirt]*4+[None]),
    )
    return [dict(kind='dropper',face=face,target=target,fill=name,phase=0,
                initial_source=source,initial_target=[None]*(5 if target=='hopper' else 27))
           for target in ('barrel','chest','hopper') for face in range(6) for name,source in patterns]


if __name__=='__main__':
    args=arguments()
    args.offline_reference=True
    script=replace_once(slot_script(),'if(false)','if(t===60||t===76)')
    cases=mixed_cases()
    capture(args,script.replace('CASES',json.dumps(cases)),len(cases))
