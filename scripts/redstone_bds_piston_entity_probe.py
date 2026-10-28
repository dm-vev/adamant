#!/usr/bin/env python3
"""Isolate native piston displacement using passive, gravity-free audit bodies.

The added behavior-pack entities have no AI. BDS still executes its original
piston, pushable, actor movement and collision paths. Definitions are retained
with the fixture; this probe does not replace evidence for vanilla mob AI.
"""

import argparse
import itertools
import json
from pathlib import Path

from redstone_bds_timing_probe import capture


def entity_definition(collision):
    return {"format_version": "1.26.0", "minecraft:entity": {
        "description": {"identifier": "adamant_audit:body_" + str(int(collision)),
                        "is_spawnable": False, "is_summonable": True},
        "components": {
            "minecraft:collision_box": {"width": 0.5, "height": 0.5},
            "minecraft:health": {"value": 20, "max": 20},
            "minecraft:physics": {"has_gravity": False, "has_collision": collision},
            "minecraft:pushable": {"is_pushable": True, "is_pushable_by_piston": True},
            "minecraft:movement": {"value": 0},
            "minecraft:persistent": {},
        }}}


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),cases=CASES;
const grid=cases[0].ride?{width:4,batch:16,spacing:32,bias:16}:{width:8,batch:64,spacing:16,bias:cases[0].entity?8:4};
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
let d,load=0,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const side=(p,f,n=1)=>p.map((v,i)=>v+delta[f][i]*n);
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded entity cell "+JSON.stringify(p));return {id:b.typeId,states:b.permutation.getAllStates()};};
const actor=p=>{const a=get(p).getComponent("minecraft:piston");return a?{state:a.state,moving:a.isMoving,attached:a.getAttachedBlocksLocations()}:null;};
const body=e=>{if(!e.isValid)throw new Error("native piston body disappeared");return {id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;
  if(cases[0].ride){cmd("tickingarea add -32 0 -32 95 100 159 audit_west true");cmd("tickingarea add 96 0 -32 159 100 159 audit_east true");}
  else cmd("tickingarea add 0 0 0 127 100 127 audit true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){
  if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  const bias=cases[0].entity?8:4;
  for(let x=0;x<8;x++)for(let z=0;z<8;z++)if(!get([bias+x*16,65,bias+z*16])){if(system.currentTick-load>240)throw new Error("interior entity cell load timeout");return;}
  if(cases[0].ride)for(let x=-32;x<160;x+=16)for(let z=-32;z<160;z+=16)if(!get([x,65,z])){if(system.currentTick-load>240)throw new Error("rider simulation margin load timeout");return;}
  if(system.currentTick&1)return;
  cells=[];
  for(let i=0;i<grid.batch&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],radius=scenario.ride?14:scenario.entity?6:4,base=[grid.bias+(i%grid.width)*grid.spacing,65,grid.bias+Math.floor(i/grid.width)*grid.spacing],source=side(base,scenario.facing^1),front=(scenario.obstruction?[1,2,3]:[1,2]).map(n=>side(base,scenario.facing,n));
   cmd(`fill ${base[0]-radius} 60 ${base[2]-radius} ${base[0]+radius} 70 ${base[2]+radius} air`);
   if(scenario.entity)cmd(`fill ${base[0]-4} 64 ${base[2]-4} ${base[0]+4} 64 ${base[2]+4} stone`);
   set(base,(scenario.sticky?"sticky_piston":"piston")+` ["facing_direction"=${nativeFace[scenario.facing]}]`);
   if(scenario.payload!=="air")set(front[0],scenario.payload);
   if(scenario.obstruction)set(front[scenario.payload==="air"?1:2],"stone");
   let setup_control=null;
   if(scenario.entity){
    const expected=new Map(),key=p=>p.join(",");
    for(let dx=-4;dx<=4;dx++)for(let dz=-4;dz<=4;dz++)expected.set(key([base[0]+dx,64,base[2]+dz]),"minecraft:stone");
    expected.set(key(base),scenario.sticky?"minecraft:sticky_piston":"minecraft:piston");
    if(scenario.payload!=="air")expected.set(key(front[0]),"minecraft:"+scenario.payload);
    if(scenario.obstruction)expected.set(key(front[scenario.payload==="air"?1:2]),"minecraft:stone");
    let checked=0,non_air=0;
    for(let dx=-radius;dx<=radius;dx++)for(let dz=-radius;dz<=radius;dz++)for(const y of [64,65]){
     const p=[base[0]+dx,y,base[2]+dz],id=read(p).id,want=expected.get(key(p))??"minecraft:air";
     if(id!==want)throw new Error("contaminated native setup "+JSON.stringify({scenario,p,id,want}));
     checked++;if(id!=="minecraft:air")non_air++;
    }
    setup_control={checked_cells:checked,non_air,expected_non_air:expected.size,clear_radius:radius};
   }
   cells.push({scenario,base,source,front,setup_tick:system.currentTick,setup_control,changes:[],samples:[],spawn:null,entity:null});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.samples.push({tick:system.currentTick,base:read(c.base),actor:actor(c.base),source:read(c.source),front:c.front.map(read),body:c.entity?body(c.entity):null});
  const edge=40+c.scenario.phase;
  if(t===edge||t===edge+16){const powered=t===edge;set(c.source,powered?"redstone_block":"air");c.changes.push({tick:system.currentTick,powered});}
  const spawnAt=c.scenario.motion==="push"?edge-5:edge+10;
  if(t===spawnAt){
   const distance=(c.scenario.payload==="air"?1:2)+c.scenario.distance;
   const centre=side(c.base,c.scenario.facing,distance).map(v=>v+.5);centre[1]-=.25;
   if(c.scenario.entity)centre[1]=c.scenario.entity==="minecraft:item"?65.25:65;
   if(c.scenario.ride){centre[1]=66;centre[c.scenario.facing<4?0:2]+=c.scenario.lateral;}
   const position={x:centre[0],y:centre[1],z:centre[2]};
   c.entity=c.scenario.entity==="minecraft:item"?d.spawnItem(new ItemStack("minecraft:diamond",1),position):d.spawnEntity(c.scenario.entity??("adamant_audit:body_"+Number(c.scenario.collision)),position);c.entity.clearVelocity();
   c.spawn={tick:system.currentTick,position,body:body(c.entity)};
  }
 }
 if(t===100){
  for(const c of cells){
   if(!c.spawn||c.changes.length!==2||!c.samples.some(s=>s.actor?.moving))throw new Error("missing entity control "+JSON.stringify(c.scenario));
   c.entity.remove();delete c.entity;emit(c);
  }
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30878)
    parser.add_argument("--suite", choices=("smoke", "displacement", "vanilla", "vanilla_obstruction", "vanilla_riders", "all"), default="smoke")
    args = parser.parse_args()
    args.offline_reference = True
    cases = [dict(sticky=s, facing=f, phase=p, payload=b, distance=distance, collision=collision,
                  motion=motion, ride=False)
             for s, f, p, b, distance, collision, motion in itertools.product(
                 (False,) if args.suite == "smoke" else (False, True),
                 (1, 5) if args.suite == "smoke" else range(6),
                 (0,) if args.suite == "smoke" else (0, 1),
                 ("air", "stone", "honey_block") if args.suite == "displacement" else ("air", "stone", "honey_block", "slime"),
                 (0, 0.5), (False, True),
                 ("push",) if args.suite == "smoke" else ("push", "pull"))]
    definitions = {"body_" + str(int(c)): entity_definition(c) for c in (False, True)}
    if args.suite in ("vanilla", "vanilla_obstruction"):
        definitions = {}
        cases = [dict(sticky=s, facing=f, phase=p, payload=b, distance=distance, collision=True,
                      motion=motion, ride=False, entity=entity)
                 for s, f, p, b, distance, motion, entity in itertools.product(
                     (False, True), range(2, 6), (0, 1),
                     ("air", "stone") if args.suite == "vanilla_obstruction" else ("air", "stone", "honey_block", "slime"),
                     (0, 0.5), ("push", "pull"), ("minecraft:armor_stand", "minecraft:item"))]
        if args.suite == "vanilla_obstruction":
            for case in cases:
                case["obstruction"] = True
    if args.suite == "vanilla_riders":
        definitions = {}
        cases = [dict(sticky=s, facing=f, phase=p, payload=b, distance=-1 if motion == "push" else 0,
                      collision=True, motion=motion, ride=True, lateral=lateral, entity=entity)
                 for s, f, p, b, motion, lateral, entity in itertools.product(
                     (False, True), range(2, 6), (0, 1), ("stone", "honey_block", "slime"),
                     ("push", "pull"), (0, .5, .75, 1), ("minecraft:armor_stand", "minecraft:item"))]
    # Definitions are also explicit data in the generated source, retained with
    # the fixture by the caller below without relying on temporary pack files.
    capture(args, SCRIPT.replace("CASES", json.dumps(cases)), len(cases), entity_definitions=definitions)
    output = json.loads(args.output.read_text(encoding="utf-8"))
    output["entity_definitions"] = definitions
    if args.suite == "vanilla_riders":
        output["scene_spacing"] = 32
        output["simulation_bounds"] = [-32, -32, 159, 159]
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
