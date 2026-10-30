#!/usr/bin/env python3
"""Capture native bottom extraction from crafter slots into hopper holes."""
from redstone_bds_hopper_crafter_slots_probe import capture_crafter_slots, crafter_structure
from redstone_bds_timing_probe import arguments


if __name__=='__main__':
    args=arguments();args.offline_reference=True
    stack=lambda name,n:dict(id='minecraft:'+name,count=n)
    diamond=lambda n:stack('diamond',n)
    stone=stack('stone',64)
    profiles=(
        ('first_slot',[diamond(4)]+[None]*8),
        ('rejected_first',[stack('dirt',4),diamond(4)]+[None]*7),
        ('later_slot',[None]*8+[diamond(4)]),
    )
    destinations=(
        ('empty',[None]*5),('earlier_hole',[None,diamond(60),stone,stone,stone]),
        ('matching_first',[diamond(60),None,stone,stone,stone]),('only_matching',[stone]*4+[diamond(60)]),
    )
    cases,structures=[],{}
    for profile,upper in profiles:
        name=f'audit/crafter_pull_{profile}'
        structures[name]=crafter_structure(upper)
        for pattern,receiver in destinations:
            cases.append(dict(kind='hopper',face=5,target='crafter',fill=profile+'_'+pattern,phase=0,pull=True,
                              target_structure=name,target_orientation='north_up',initial_source=receiver,initial_target=upper))
    capture_crafter_slots(args,cases,structures,pull=True)
