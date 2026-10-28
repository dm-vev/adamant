#!/usr/bin/env python3
"""Capture actual carts traversing isolated eight-block native rail slopes.

All authored non-air cells and rail permutations are checked after settling.
The tracks stay in a fully force-loaded 512-chunk area, 128 blocks apart.
"""
import json
from copy import copy

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,offset=0,load=0,cells=[],ready=false;
const cmd=x=>d.runCommand(x);
const read=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded slope cell "+p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const body=e=>({id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<8&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[64+i%2*128,65,64+Math.floor(i/2)*128],dx=scenario.axis==="x"?1:0,dz=scenario.axis==="z"?1:0;
  const lo=[base[0]-40*dx-2*dz,64,base[2]-40*dz-2*dx],hi=[base[0]+40*dx+2*dz,85,base[2]+40*dz+2*dx];
  cmd(`fill ${lo.join(" ")} ${hi.join(" ")} air`);
  const authored=new Map(),rail_positions=[],height=u=>65+(scenario.slope===1?Math.max(0,Math.min(8,u)):8-Math.max(0,Math.min(8,u)));
  const set=(p,id,states={})=>{cmd(`setblock ${p.join(" ")} ${id}`+(Object.keys(states).length?' ['+Object.entries(states).map(([k,v])=>`"${k}"=${v}`).join(',')+']':''));authored.set(p.join(','),{position:p,id:"minecraft:"+id,states});};
  for(let u=-40;u<=40;u++){
   const p=[base[0]+u*dx,height(u),base[2]+u*dz];rail_positions.push(p);
   cmd(`fill ${p[0]-2*dz} 64 ${p[2]-2*dx} ${p[0]+2*dz} ${p[1]-1} ${p[2]+2*dx} stone`);
   for(let n=-2;n<=2;n++)for(let y=64;y<p[1];y++){
    const position=[p[0]+n*dz,y,p[2]+n*dx];authored.set(position.join(','),{position,id:"minecraft:stone",states:{}});
   }
  }
  if(scenario.mode==="golden_on")for(const p of rail_positions)set([p[0],p[1]-1,p[2]],"redstone_block");
  const id=scenario.mode==="rail"?"rail":scenario.mode==="detector"?"detector_rail":"golden_rail";
  for(let n=0;n<rail_positions.length;n++){
   const u=n-40,onSlope=scenario.slope===1?u>=0&&u<8:u>0&&u<=8;
   const direction=onSlope?(scenario.slope===1?(dx?2:5):(dx?3:4)):(dx?1:0);
   set(rail_positions[n],id,{rail_direction:direction});
  }
  cells.push({scenario,base,rail_positions,environment:[...authored.values()],setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],entity:undefined});
 }start=system.currentTick;
}
function spawn(c){
 for(const b of c.environment){
  const actual=read(b.position);if(actual.id!==b.id)throw new Error("slope terrain mismatch "+b.position);
  for(const [k,v] of Object.entries(b.states))if(actual.states[k]!==v)throw new Error("slope permutation mismatch "+b.position);
  if(b.id.endsWith("rail")&&b.id!=="minecraft:rail"&&actual.states.rail_data_bit!==(c.scenario.mode==="golden_on"))throw new Error("slope power not settled "+JSON.stringify({scenario:c.scenario,position:b.position,actual}));
  b.states=actual.states;
 }
 const u=c.scenario.speed>0?-2:10,p=c.rail_positions[u+40],position={x:p[0]+.5,y:p[1]+.0625,z:p[2]+.5};
 c.entity=d.spawnEntity(c.scenario.entity,position);c.entity.clearVelocity();
 c.entity.applyImpulse({x:c.scenario.axis==="x"?c.scenario.speed:0,y:0,z:c.scenario.axis==="z"?c.scenario.speed:0});
 c.spawn={tick:system.currentTick,position,body:body(c.entity)};
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  for(let z=0;z<512;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${Math.min(z+95,511)} audit_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){
  for(let x=0;x<256;x+=16)for(let z=0;z<512;z+=16)if(!d.getBlock({x,y:65,z})){
   if(system.currentTick-load>480)throw new Error("slope simulation load timeout");return;
  }
  if(system.currentTick&1)return;ready=true;setup();return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  let b=null;
  if(c.entity){b=body(c.entity);if(Math.abs(b.position.x-c.base[0]-.5)>36||Math.abs(b.position.z-c.base[2]-.5)>36||b.position.y<63||b.position.y>80)throw new Error("cart escaped isolated slope");}
  c.samples.push({tick:system.currentTick,body:b});if(t===40)spawn(c);
 }
 if(t===120){
  for(const c of cells){const {entity,...trace}=c;emit(trace);}
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else setup();
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == '__main__':
    args = arguments()
    args.offline_reference = True
    cases = [dict(entity=entity, mode=mode, axis=axis, slope=slope, speed=speed, phase=0)
             for entity in ('minecraft:minecart', 'minecraft:chest_minecart')
             for mode in ('rail', 'golden_off', 'golden_on', 'detector')
             for axis in ('x', 'z') for slope in (1, -1) for speed in (.4, -.4)]
    # Separate native processes keep actor removal and replacement of tall
    # tracks from one cart family out of another family's initial conditions.
    parts = []
    for entity in ('minecraft:minecart', 'minecraft:chest_minecart'):
        subset = [c for c in cases if c['entity'] == entity]
        part = copy(args)
        part.output = args.output.with_name(args.output.stem + '-' + entity.split(':')[1] + args.output.suffix)
        capture(part, SCRIPT.replace('CASES', json.dumps(subset)), len(subset))
        parts.append(json.loads(part.output.read_text()))
    result = dict(parts[0])
    result['scenarios'] = [c for part in parts for c in part['scenarios']]
    result['log_directories'] = [part['log_directory'] for part in parts]
    result.pop('log_directory', None)
    if len(result['scenarios']) != 64 or not all(part['complete'] for part in parts):
        raise ValueError('incomplete native slope captures')
    args.output.write_text(json.dumps(result), encoding='utf-8')
    print(json.dumps(dict(output=str(args.output), complete=True, count=64)), flush=True)
