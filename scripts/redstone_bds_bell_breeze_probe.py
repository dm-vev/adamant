#!/usr/bin/env python3
"""Redirect actual unmodified BDS breeze-fired charges into controlled bells.

The native breeze projectile cannot be summoned. Real breezes shoot at real
iron golems; their original spawn bodies and owner IDs are retained. Scripted
teleport/shoot inputs then control the bell impact without changing definitions.
This measures the projectile/bell path, not the breeze's targeting algorithm.
"""
import json
from pathlib import Path

from redstone_bds_bell_projectile_probe import readback
from redstone_bds_bell_wind_probe import bell_wind_cases, bell_wind_script
from redstone_bds_dispenser_switch_pulse_probe import replace_once
from redstone_bds_timing_probe import arguments, capture


HARVEST = r'''
let pool=[],factoryReady=false,factoryStart=0,birthSerial=0,harvested=0;
const required=cases.reduce((n,c)=>n+(c.repeat?2:1),0);
world.afterEvents.entitySpawn.subscribe(e=>{try{
 if(e.entity.typeId!=="minecraft:breeze_wind_charge_projectile")return;
 if(harvested>=required){e.entity.remove();return;}
 const actor=e.entity,owner=actor.getComponent("minecraft:projectile")?.owner;
 if(!owner||owner.typeId!=="minecraft:breeze")throw new Error("native breeze owner control missing");
 const birth={tick:system.currentTick,id:actor.typeId,owner_id:owner.id,owner_type:owner.typeId,
  position:actor.location,velocity:actor.getVelocity(),aabb:actor.getAABB()};
 const slot=birthSerial++,hold={x:4+slot%16*2,y:160,z:260+Math.floor(slot/16)*2};
 actor.clearVelocity();actor.teleport(hold);pool.push({entity:actor,birth,hold});harvested++;
}catch(e){emit({error:String(e),stack:e.stack});}});
function factory(){
 for(let i=0;i<48;i++){const x=8+i%8*16,z=264+Math.floor(i/8)*16;
  cmd(`fill ${x-3} 63 ${z-3} ${x+11} 72 ${z+3} air`);
  cmd(`fill ${x-3} 64 ${z-3} ${x+11} 64 ${z+3} stone`);
  d.spawnEntity("minecraft:breeze",{x:x+.5,y:65,z:z+.5});
  d.spawnEntity("minecraft:iron_golem",{x:x+8.5,y:65,z:z+.5});
 }
}
'''


def bell_breeze_script(cases):
    script = bell_wind_script(cases)
    script = replace_once(script, 'audit_bell_wind_b true', 'audit_bell_breeze_b true')
    script = replace_once(script, '0 0 128 127 100 255', '-16 0 256 143 100 383')
    script = replace_once(script, '!get([127,65,255])', '!get([143,65,383])')
    script = replace_once(script, 'world.afterEvents.projectileHitBlock.subscribe', HARVEST+'\nworld.afterEvents.projectileHitBlock.subscribe')
    script = replace_once(script, 'const h=system.runInterval(()=>{try{',
        'const h=system.runInterval(()=>{try{'
        'for(const p of pool){if(!p.entity.isValid)throw new Error("held native breeze charge expired");p.entity.clearVelocity();p.entity.teleport(p.hold);}')
    script = replace_once(script, '  if(system.currentTick%20!==2)return;',
        '  if(!factoryReady){cmd("difficulty normal");factory();factoryReady=true;factoryStart=system.currentTick;return;}'
        'if(pool.length<required){if(system.currentTick-factoryStart>600)throw new Error("native breeze harvest incomplete: "+pool.length);'
        'if((system.currentTick-factoryStart)%100===0)emit({harvested:pool.length,required});return;}'
        'if(system.currentTick%20!==2)return;')
    script = replace_once(script, 'const arrow=d.spawnEntity(c.scenario.projectile,position),projectile=arrow.getComponent("minecraft:projectile");',
        'const entry=pool.shift();if(!entry?.entity.isValid)throw new Error("native breeze shot unavailable");'
        'const arrow=entry.entity;arrow.clearVelocity();arrow.teleport(position);const projectile=arrow.getComponent("minecraft:projectile");')
    script = replace_once(script, 'const birth={id:arrow.typeId,requested_position:position,',
        'const birth={id:arrow.typeId,native_birth:entry.birth,requested_position:position,')
    return script


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(c, projectile='minecraft:breeze_wind_charge_projectile') for c in bell_wind_cases()]
    capture(args, bell_breeze_script(cases), len(cases))
    result = json.loads(args.output.read_text(encoding='utf-8'))
    result['complete'] = False
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    result = readback(result, Path(__file__).resolve().parents[1]/'.tmp/redstone-structure-readback.exe')
    result['complete'] = True
    args.output.write_text(json.dumps(result, separators=(',', ':'))+'\n', encoding='utf-8')
    print(json.dumps(dict(complete=True, scenes=len(cases), snapshots=result['actor_readback']['snapshots'])), flush=True)
