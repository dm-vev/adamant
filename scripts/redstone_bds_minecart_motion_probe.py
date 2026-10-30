#!/usr/bin/env python3
"""Capture original cart motion on isolated flat rail tracks.

Each track has 97 rails and five floor cells across. Eight scenes run in a
128-block-spaced batch. All 512 simulation chunks are force loaded; body bounds
and initial terrain are checked so another track or unloaded chunk cannot
become a motion oracle. No native physics is replaced by script movement.
"""
import json

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
let d,start=0,offset=0,load=0,cells=[],ready=false;
const cmd=x=>d.runCommand(x),set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const block=p=>{const b=d.getBlock({x:p[0],y:p[1],z:p[2]});if(!b)throw new Error("unloaded minecart cell "+p);return b;};
const read=p=>{const b=block(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const body=e=>({id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();cells=[];
 for(let i=0;i<8&&offset+i<cases.length;i++){
  const scenario=cases[offset+i],base=[64+i%2*128,65,64+Math.floor(i/2)*128],dx=scenario.axis==="x"?1:0,dz=scenario.axis==="z"?1:0;
  const low=[base[0]-48*dx-2*dz,64,base[2]-48*dz-2*dx],high=[base[0]+48*dx+2*dz,69,base[2]+48*dz+2*dx];
  cmd(`fill ${low.join(" ")} ${high.join(" ")} air`);cmd(`fill ${low.join(" ")} ${high[0]} 64 ${high[2]} stone`);
  const rail_positions=Array.from({length:97},(_,n)=>[base[0]+(n-48)*dx,65,base[2]+(n-48)*dz]);
  const rail_id=scenario.mode==="rail"?"rail":scenario.mode==="detector"?"detector_rail":"golden_rail";
  if(scenario.mode==="golden_on")for(const p of rail_positions)set([p[0],64,p[2]],"redstone_block");
  for(const p of rail_positions)set(p,rail_id+` ["rail_direction"=${dx?1:0}]`);
  cells.push({scenario,base,rail_positions,rail_id,setup_tick:system.currentTick,changes:[{tick:system.currentTick,kind:"setup"}],samples:[],entity:undefined});
 }start=system.currentTick;
}
function spawn(c){
 const dx=c.scenario.axis==="x"?1:0,dz=c.scenario.axis==="z"?1:0;
 let checked_floor=0;
 for(const p of c.rail_positions){
  const b=read(p);if(b.id!=="minecraft:"+c.rail_id||b.states.rail_direction!==(dx?1:0))throw new Error("native track setup changed");
  if(c.scenario.mode!=="rail"&&b.states.rail_data_bit!==(c.scenario.mode==="golden_on"))throw new Error("native rail power not settled");
  for(let n=-2;n<=2;n++){
   const q=[p[0]+n*dz,64,p[2]+n*dx],expected=c.scenario.mode==="golden_on"&&n===0?"minecraft:redstone_block":"minecraft:stone";
   if(read(q).id!==expected)throw new Error("native minecart floor mismatch");checked_floor++;
  }
 }
 c.setup_control={checked_rails:c.rail_positions.length,checked_floor};
 const position={x:c.base[0]+.5,y:65.0625,z:c.base[2]+.5};
 c.entity=d.spawnEntity(c.scenario.entity,position);c.entity.clearVelocity();
 c.entity.applyImpulse({x:dx*c.scenario.speed,y:0,z:dz*c.scenario.speed});
 const inv=c.entity.getComponent("minecraft:inventory")?.container;
 if(inv)for(let n=0;n<inv.size;n++)if(inv.getItem(n))throw new Error("native cart inventory is not empty");
 c.spawn={tick:system.currentTick,position,body:body(c.entity),inventory_size:inv?inv.size:0};
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  for(let z=0;z<512;z+=96)cmd(`tickingarea add 0 0 ${z} 255 100 ${Math.min(z+95,511)} audit_${z} true`);
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;
 }
 if(!ready){
  for(let x=0;x<256;x+=16)for(let z=0;z<512;z+=16)if(!d.getBlock({x,y:65,z})){
   if(system.currentTick-load>480)throw new Error("native motion simulation load timeout");
   if(system.currentTick%20===0)emit({loading:true,tick:system.currentTick});return;
  }
  if(system.currentTick&1)return;ready=true;setup();return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  let b=null,rail=null;
  if(c.entity){
   b=body(c.entity);
   if(Math.abs(b.position.x-c.base[0]-.5)>46||Math.abs(b.position.z-c.base[2]-.5)>46)throw new Error("cart escaped its isolated track");
   const p=[Math.floor(b.position.x),65,Math.floor(b.position.z)];rail={position:p,block:read(p)};
  }
  c.samples.push({tick:system.currentTick,body:b,rail});
  if(t===40)spawn(c);
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
    cases = [dict(entity=entity,mode=mode,axis=axis,speed=speed,phase=0)
             for entity in ('minecraft:minecart','minecraft:chest_minecart','minecraft:hopper_minecart')
             for mode in ('rail','golden_off','golden_on','detector') for axis in ('x','z')
             for speed in (0,.1,-.1,.4)]
    capture(args,SCRIPT.replace('CASES',json.dumps(cases)),len(cases))
