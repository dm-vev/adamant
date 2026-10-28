#!/usr/bin/env python3
"""Capture original-BDS detector-rail power and minecart inventory output."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,index=0,start=0,entity,samples=[],ready=false,load=0;
const read=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded detector-rail cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const cmd=x=>d.runCommand(x);
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();
 cmd("fill -4 63 -3 4 69 3 air");cmd("fill -4 64 -3 4 64 3 stone");
 cmd('setblock -1 65 0 detector_rail ["rail_direction"=0]');
 cmd('setblock 0 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]');
 cmd("setblock 1 65 0 redstone_wire");
 const c=cases[index];entity=d.spawnEntity(c.entity,{x:-.5,y:65.0625,z:.5});entity.clearVelocity();
 const inv=entity.getComponent("minecraft:inventory")?.container;
 if(c.stacks.length&&!inv)throw new Error("native cart has no inventory "+c.entity);
 if(inv)for(let slot=0;slot<inv.size;slot++)inv.setItem(slot,undefined);
 for(const [slot,name,count] of c.stacks)inv.setItem(slot,new ItemStack(name,count));
 samples=[];start=system.currentTick;
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 16 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!d.getBlock({x:-4,y:65,z:-3})||!d.getBlock({x:4,y:65,z:3})){if(system.currentTick-load>240)throw new Error("native rail area load timeout");return;}if(system.currentTick&1)return;ready=true;setup();return;}
 const inv=entity.getComponent("minecraft:inventory")?.container;
 samples.push({tick:system.currentTick,source:read([-1,65,0]),comparator:read([0,65,0]),wire:read([1,65,0]),
  body:{id:entity.typeId,position:entity.location,aabb:entity.getAABB()},inventory:inv?{size:inv.size,stacks:Array.from({length:inv.size},(_,slot)=>{const i=inv.getItem(slot);return i?{slot,id:i.typeId,count:i.amount,max:i.maxAmount}:null;}).filter(x=>x)}:null});
 if(system.currentTick-start===32){emit({scenario:cases[index],samples,changes:[{tick:start,kind:"setup"}]});index++;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}else setup();}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity, stacks=[], phase=0) for entity in
             ('minecraft:minecart', 'minecraft:tnt_minecart', 'minecraft:armor_stand')]
    for entity, size in (('minecraft:chest_minecart', 27), ('minecraft:hopper_minecart', 5)):
        for stacks in ([], [[0, 'minecraft:diamond', 1]], [[0, 'minecraft:diamond', 64]],
                       [[slot, 'minecraft:diamond', 64] for slot in range(size)],
                       [[size - 1, 'minecraft:ender_pearl', 16]], [[0, 'minecraft:shield', 1]]):
            cases.append(dict(entity=entity, stacks=stacks, phase=0))
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
