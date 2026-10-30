#!/usr/bin/env python3
"""Compare native hopper-cart collection of whole dropped stacks."""
import json

from redstone_bds_hopper_cart_transfer_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(mode=mode, source=False, destination=False, initial=initial,
                  world_item_count=count, phase=0)
             for mode in ('rail', 'activator_off', 'activator_on')
             for initial in (0, 32, 319, 320) for count in (3, 64)]
    script = SCRIPT.replace('function snapshot(c){', '''
function worldItems(c){let n=0;for(const e of d.getEntities({type:"minecraft:item",location:{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5},maxDistance:4}))n+=e.getComponent("minecraft:item")?.itemStack.amount??0;return n;}
function snapshot(c){''')
    script = script.replace('tick:system.currentTick,cart:', 'tick:system.currentTick,world_items:worldItems(c),cart:')
    script = script.replace('if(c.scenario.initial) c.entity.getComponent("minecraft:inventory").container.setItem(0,new ItemStack("minecraft:diamond",c.scenario.initial));', '''
 if(c.scenario.initial){const inv=c.entity.getComponent("minecraft:inventory").container;let left=c.scenario.initial;for(let slot=0;left>0;slot++){const n=Math.min(left,64);inv.setItem(slot,new ItemStack("minecraft:diamond",n));left-=n;}}
''')
    script = script.replace(' c.spawn=snapshot(c);', '''
 const dropped=d.spawnItem(new ItemStack("minecraft:diamond",c.scenario.world_item_count),{x:c.base[0]+.5,y:65.5,z:c.base[2]+.5});dropped.clearVelocity();
 c.spawn=snapshot(c);''')
    if 'd.spawnItem' not in script or 'world_items:worldItems' not in script:
        raise ValueError('native transfer producer contract changed')
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
