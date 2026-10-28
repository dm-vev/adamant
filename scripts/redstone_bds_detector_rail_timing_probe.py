#!/usr/bin/env python3
"""Capture actual-cart detector-rail activation, removal and output timing."""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,index=0,start=0,on=0,entity,samples=[],changes=[],ready=false,load=0;
const cmd=x=>d.runCommand(x);
const read=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded detector cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();
 cmd("fill -3 63 -3 4 69 3 air");cmd("fill -3 64 -3 4 64 3 stone");
 cmd('setblock 0 65 0 detector_rail ["rail_direction"=0]');
 cmd("setblock 1 65 0 redstone_wire");cmd("setblock 2 65 0 redstone_lamp");
 samples=[];changes=[];start=system.currentTick;
 on=start+24+((cases[index].phase-((start+24)&1)+2)%2);
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 16 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!d.getBlock({x:-3,y:65,z:-3})||!d.getBlock({x:4,y:65,z:3})){if(system.currentTick-load>240)throw new Error("native rail area load timeout");return;}ready=true;setup();return;}
 const c=cases[index];samples.push({tick:system.currentTick,source:read([0,65,0]),wire:read([1,65,0]),lamp:read([2,65,0])});
 if(system.currentTick===on){entity=d.spawnEntity(c.entity,{x:.5,y:65.0625,z:.5});entity.clearVelocity();changes.push({tick:system.currentTick,kind:"spawn"});}
 if(system.currentTick===on+c.dwell){entity.remove();entity=undefined;changes.push({tick:system.currentTick,kind:"remove"});}
 if(system.currentTick-start>=100){emit({scenario:c,setup_tick:start,changes,samples});index++;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}else setup();}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity, phase=phase, dwell=dwell)
             for entity in ('minecraft:minecart', 'minecraft:chest_minecart')
             for phase in (0, 1) for dwell in (1, 2, 3, 8, 19, 20, 21, 30)]
    capture(args, SCRIPT.replace('CASES', json.dumps(cases)), len(cases))
