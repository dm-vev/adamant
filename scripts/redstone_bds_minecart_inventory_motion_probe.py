#!/usr/bin/env python3
"""Extend the isolated native flat tracks to container loads and other carts."""
import json

from redstone_bds_minecart_motion_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity, mode=mode, axis=axis, speed=speed, phase=0, load=load)
             for entity in ('minecraft:chest_minecart', 'minecraft:hopper_minecart')
             for load in ('single_diamond', 'full_diamond', 'full_pearl')
             for mode in ('rail', 'golden_off', 'golden_on', 'detector')
             for axis in ('x', 'z') for speed in (.1, -.4)]
    cases += [dict(entity=entity, mode=mode, axis=axis, speed=speed, phase=0, load='empty')
              for entity in ('minecraft:tnt_minecart', 'minecraft:command_block_minecart')
              for mode in ('rail', 'golden_off', 'golden_on', 'detector')
              for axis in ('x', 'z') for speed in (0, .1, -.1, .4)]
    script = SCRIPT.replace('{world,system}', '{world,system,ItemStack}')
    old = 'c.spawn={tick:system.currentTick,position,body:body(c.entity),inventory_size:inv?inv.size:0};'
    new = '''
 const load=c.scenario.load,inventory=[];
 if(load!=="empty"){
  if(!inv)throw new Error("missing native cart container");
  const id=load==="full_pearl"?"minecraft:ender_pearl":"minecraft:diamond",count=load==="single_diamond"?1:load==="full_pearl"?16:64;
  for(let n=0;n<(load==="single_diamond"?1:inv.size);n++)inv.setItem(n,new ItemStack(id,count));
 }
 if(inv)for(let n=0;n<inv.size;n++){const item=inv.getItem(n);if(item)inventory.push({slot:n,id:item.typeId,count:item.amount});}
 c.spawn={tick:system.currentTick,position,body:body(c.entity),inventory_size:inv?inv.size:0,inventory};
'''
    if old not in script:
        raise ValueError('native motion spawn contract changed')
    script = script.replace(old, new)
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
