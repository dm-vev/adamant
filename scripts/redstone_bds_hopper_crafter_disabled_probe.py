#!/usr/bin/env python3
"""Capture native disabled crafter slots during all-sided hopper insertion."""
from redstone_bds_hopper_crafter_slots_probe import capture_crafter_slots, crafter_structure
from redstone_bds_timing_probe import arguments


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    stack=lambda name,n:dict(id='minecraft:'+name,count=n)
    diamond=lambda n:stack('diamond',n)
    stone=stack('stone',64)
    patterns=(
        ('all_disabled',511,[None]*9),
        ('only_centre',511^(1<<4),[None]*9),
        ('disabled_before_hole',(1<<0)|(1<<2),[None,diamond(1),None,diamond(60)]+[stone]*5),
        ('disabled_later_empty',1<<1,[diamond(60),None]+[stone]*7),
        ('disabled_least_filled',1<<4,[diamond(4),diamond(8),stone,stone,None]+[stone]*4),
        ('disabled_multigap',(1<<0)|(1<<2)|(1<<6)|(1<<8),[None,diamond(3),None,diamond(1),diamond(5),stone,None,stone,None]),
    )
    cases,structures=[],{}
    for pattern,mask,target in patterns:
        name=f'audit/crafter_disabled_{pattern}'
        empty=f'audit/crafter_disabled_empty_{mask}'
        structures[name]=crafter_structure(target,mask)
        structures[empty]=crafter_structure([None]*9,mask)
        for face in (0,2,3,4,5):
            cases.append(dict(kind='hopper',face=face,target='crafter',fill=pattern,phase=0,disabled_mask=mask,
                              target_structure=name,target_empty_structure=empty,target_orientation='north_up',
                              initial_source=[diamond(4),stack('dirt',4),None,None,None],initial_target=target))
    capture_crafter_slots(args,cases,structures)
