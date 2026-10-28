#!/usr/bin/env python3
"""Distinguish native cart sweeps from recovery of an existing block overlap."""
import json

from redstone_bds_minecart_motion_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture


SETUP = r'''
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<8&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[64+i%2*128,65,64+Math.floor(i/2)*128];
  cmd(`fill ${base[0]-6} 64 ${base[2]-6} ${base[0]+6} 69 ${base[2]+6} air`);
  cmd(`fill ${base[0]-6} 64 ${base[2]-6} ${base[0]+6} 64 ${base[2]+6} bedrock`);
  set(base,scenario.obstacle+(scenario.obstacle==="dispenser"?' ["facing_direction"=2]':''));
  cells.push({scenario,base,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],entity:undefined});
 }start=system.currentTick;
}
function spawn(c){
 c.obstacle=read(c.base);if(c.obstacle.id!=="minecraft:"+c.scenario.obstacle)throw new Error("native overlap obstacle mismatch");
 const position={x:c.base[0]+.5+c.scenario.shift,y:65.35,z:c.base[2]+.5};
 c.entity=d.spawnEntity(c.scenario.entity,position);c.entity.clearVelocity();c.entity.applyImpulse({x:c.scenario.speed,y:0,z:0});
 c.spawn={tick:system.currentTick,position,body:body(c.entity)};
}
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity,obstacle=obstacle,shift=shift,speed=speed,phase=0)
             for entity in ('minecraft:minecart','minecraft:chest_minecart')
             for obstacle in ('stone','dispenser','glass')
             for shift in (-1,0,1) for speed in (-.1,0,.1,.4)]
    begin, end = SCRIPT.index('function setup(){'), SCRIPT.index('const h=system.runInterval')
    script = SCRIPT[:begin] + SETUP + SCRIPT[end:]
    script = script.replace('>46', '>5')
    capture(args, script.replace('CASES', json.dumps(cases)), len(cases))
