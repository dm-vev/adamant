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
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x)),cases=CASES;
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
let d,load=0,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const side=(p,f,n=1)=>p.map((v,i)=>v+delta[f][i]*n);
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded entity cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const actor=p=>{const a=get(p).getComponent("minecraft:piston");return a?{state:a.state,moving:a.isMoving,attached:a.getAttachedBlocksLocations()}:null;};
const body=e=>{if(!e.isValid)throw new Error("native piston body disappeared");return {id:e.typeId,position:e.location,velocity:e.getVelocity(),aabb:e.getAABB(),on_ground:e.isOnGround};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 127 100 127 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!start){
  if(!get([0,65,0])||!get([127,65,127])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  if(system.currentTick&1)return;
  cells=[];
  for(let i=0;i<64&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],base=[4+(i%8)*16,65,4+Math.floor(i/8)*16],source=side(base,scenario.facing^1),front=[1,2].map(n=>side(base,scenario.facing,n));
   cmd(`fill ${base[0]-4} 60 ${base[2]-4} ${base[0]+4} 70 ${base[2]+4} air`);
   set(base,(scenario.sticky?"sticky_piston":"piston")+` ["facing_direction"=${nativeFace[scenario.facing]}]`);
   if(scenario.payload!=="air")set(front[0],scenario.payload);
   cells.push({scenario,base,source,front,setup_tick:system.currentTick,changes:[],samples:[],spawn:null,entity:null});
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
   if(c.scenario.ride){centre[1]=66;centre[c.scenario.facing<4?0:2]+=c.scenario.lateral;}
   const position={x:centre[0],y:centre[1],z:centre[2]};
   c.entity=d.spawnEntity("adamant_audit:body_"+Number(c.scenario.collision),position);c.entity.clearVelocity();
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
    parser.add_argument("--suite", choices=("smoke", "displacement", "all"), default="smoke")
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
    # Definitions are also explicit data in the generated source, retained with
    # the fixture by the caller below without relying on temporary pack files.
    capture(args, SCRIPT.replace("CASES", json.dumps(cases)), len(cases), entity_definitions=definitions)
    output = json.loads(args.output.read_text(encoding="utf-8"))
    output["entity_definitions"] = definitions
    args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
