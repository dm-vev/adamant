#!/usr/bin/env python3
"""Observe native leaf update bits around ordinary changes and decay-source removal."""
import json

from redstone_bds_timing_probe import arguments
from redstone_bds_door_support_probe import capture


CASES = [dict(action=action, persistent=persistent)
         for action in ('stone_remove', 'door_remove', 'door_toggle', 'log_remove',
                        'log_rotate', 'log_replace_wood', 'leaf_remove', 'leaf_change_state')
         for persistent in (False, True)]
OFFSETS = ((1, 0, 0), (1, 1, 1), (2, 0, 0), (4, 0, 0), (4, 4, 4),
           (5, 0, 0), (-4, -4, -4), (0, 4, -4), (0, 5, 0))

SCRIPT = r'''
import {world,system,BlockPermutation} from "@minecraft/server";
const cases=CASES,offsets=OFFSETS,emit=x=>console.warn("ADAMANT_SUPPORT "+JSON.stringify(x));
let d,load=0,ready=false,index=0,current=null;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(p,id,states={})=>get(p).setPermutation(BlockPermutation.resolve("minecraft:"+id,states));
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  cmd("tickingarea add -16 0 -16 95 110 95 audit_leaf_neighbours true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([0,70,0])||!get([90,70,90])){if(system.currentTick-load>480)throw new Error("leaf chunks not ready");return;}ready=true;return;}
 if(!current){const c=cases[index],base=[16+index%4*20,70,16+Math.floor(index/4)*20],positions=offsets.map(o=>base.map((v,i)=>v+o[i])),source_positions=[base,[base[0],71,base[2]]];
  if(c.action.startsWith("door")){set([base[0],69,base[2]],"stone");cmd(`setblock ${base.join(" ")} wooden_door ["minecraft:cardinal_direction"="north"]`);}
  else if(c.action.startsWith("log"))set(base,"oak_log",{pillar_axis:"y"});
  else if(c.action.startsWith("leaf"))set(base,"oak_leaves",{persistent_bit:c.persistent,update_bit:false});
  else set(base,"stone");
  for(const p of positions)set(p,"oak_leaves",{persistent_bit:c.persistent,update_bit:false});
  current={scenario:c,base,positions,source_positions,setup_tick:system.currentTick,initial_source:source_positions.map(read),initial_leaves:positions.map(read),samples:[]};
  let id="air",states={};
  if(c.action==="door_toggle"){id="wooden_door";states=read(base).states;delete states.direction;states.open_bit=true;}
  if(c.action==="log_rotate"){id="oak_log";states={pillar_axis:"x"};}
  if(c.action==="log_replace_wood"){id="spruce_log";states={pillar_axis:"y"};}
  if(c.action==="leaf_change_state"){id="oak_leaves";states={persistent_bit:c.persistent,update_bit:true};}
  const input=BlockPermutation.resolve("minecraft:"+id,states);current.requested_source={id:"minecraft:"+id,states:input.getAllStates()};
  get(base).setPermutation(input);current.after_source=source_positions.map(read);current.after_leaves=positions.map(read);return;
 }
 current.samples.push({tick:system.currentTick,source:current.source_positions.map(read),leaves:current.positions.map(read)});
 if(system.currentTick-current.setup_tick>=6){emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    capture(args, CASES, source_script=SCRIPT.replace('OFFSETS', json.dumps(OFFSETS)), description=__doc__)
