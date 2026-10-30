#!/usr/bin/env python3
"""Observe native piston input faces, pulse widths and per-tick movement.

The original BDS owns the simulation. Inputs change after each recorded frame;
absolute tick parity is controlled independently for every case. No floor or
entity is placed in a piston's movement path, including downward pistons.
"""

import argparse
from pathlib import Path

from redstone_bds_timing_probe import capture


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
const cases=CASES;
const large=cases[0].payload_kind!==undefined,area=large?159:127,batchSize=large?25:64;
const sensorSuite=cases[0].sensor_filter!==undefined;
let d,load=0,start=0,offset=0,cells=[],seenItems=new Set();
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const side=(p,face,n=1)=>p.map((v,i)=>v+delta[face][i]*n);
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const input=(c,powered)=>set(c.source,c.scenario.kind==="lever"?`lever ["lever_direction"="${["down_north_south","up_north_south","north","south","west","east"][c.scenario.input]}","open_bit"=${powered}]`:(powered?"redstone_block":"air"));
const read=p=>{
 const b=get(p);if(!b)throw new Error("unloaded piston cell");
 const result={id:b.typeId,states:b.permutation.getAllStates()};
 const inv=b.getComponent("minecraft:inventory")?.container;
 if(inv){result.inventory=[];for(let slot=0;slot<inv.size;slot++){const item=inv.getItem(slot);if(item)result.inventory.push({slot,id:item.typeId,count:item.amount});}}
 return result;
};
const signal=p=>({...read(p),power:get(p).getRedstonePower()});
const actor=p=>{
 const c=get(p).getComponent("minecraft:piston");
 if(!c)throw new Error("missing native piston component");
 const result={state:c.state,moving:c.isMoving};
 if(typeof result.moving!=="boolean"||typeof result.state!=="string")throw new Error("unsupported piston state "+JSON.stringify(result));
 if(typeof c.getAttachedBlocksLocations==="function")result.attached=c.getAttachedBlocksLocations();
 return result;
};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd(`tickingarea add 0 0 0 ${area} 100 ${area} audit true`);cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd(`gamerule dotiledrops ${cases[0].tile_drops?"true":"false"}`);cmd("weather clear 100000");if(sensorSuite){cmd("gamerule dodaylightcycle false");cmd("time set 6001");}return;}
 if(!start){
  if(!get([0,65,0])||!get([area,65,area])){if(system.currentTick-load>240)throw new Error("piston area load timeout");return;}
  // Loaded opposite corners do not guarantee that every interior column has
  // completed loading. Check each large cell's footprint before authoring it.
  if(large)for(let i=0;i<batchSize&&offset+i<cases.length;i++)for(const dx of [-14,0,14])for(const dz of [-14,0,14]){
   if(!get([16+(i%5)*32+dx,65,16+Math.floor(i/5)*32+dz])){if(system.currentTick-load>240)throw new Error("interior piston area load timeout");return;}
  }
  if(system.currentTick&1)return;
  emit({ready:true});cells=[];seenItems=new Set();if(cases[0].tile_drops)cmd("kill @e[type=item]");
  for(let i=0;i<batchSize&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],base=large?[16+(i%5)*32,65,16+Math.floor(i/5)*32]:[4+(i%8)*16,65,4+Math.floor(i/8)*16];
   if(large)cmd(`fill ${base[0]-14} 51 ${base[2]-14} ${base[0]+14} 79 ${base[2]+14} air`);
   else for(let y=61;y<=69;y++)cmd(`fill ${base[0]-4} ${y} ${base[2]-4} ${base[0]+(sensorSuite?8:4)} ${y} ${base[2]+(sensorSuite?8:4)} air`);
   const source=side(base,scenario.input,scenario.conducted?2:1),conductor=scenario.conducted?side(base,scenario.input):null;
   const front=Array.from({length:large?14:3},(_,i)=>side(base,scenario.facing,i+1));
   if(conductor)set(conductor,"stone");
   set(base,(scenario.sticky?"sticky_piston":"piston")+` ["facing_direction"=${nativeFace[scenario.facing]}]`);
   if(scenario.payload)set(front[0],"stone");
   if(large){
    for(let n=0;n<scenario.length;n++)set(front[n],scenario.payload_kind);
    if(scenario.payload_kind==="chest")get(front[0]).getComponent("minecraft:inventory").container.setItem(0,new ItemStack("minecraft:diamond",7));
   }
   const c={scenario,base,source,conductor,front,setup_tick:system.currentTick,changes:[],samples:[]};
   if(scenario.tile_drops){c.item_spawns=[];c.drop_counts={};}
   if(scenario.supported_popped){
    c.extras=[];c.extra_probes=[];c.injections=[];
    for(const p of front.slice(0,2)){const support=side(p,0);set(support,"stone");c.extras.push({pos:support,block:"stone"});c.extra_probes.push(support);}
    set(front[0],scenario.payload_kind+(scenario.payload_states??""));
   }
   if(scenario.target_extended!==undefined){
    const targetFace=[0,1,2,3,4,5].find(f=>(f>>1)!==(scenario.facing>>1));
    set(front[0],scenario.payload_kind+` ["facing_direction"=${nativeFace[targetFace]}]`);
    const power=side(front[0],targetFace^1),head=side(front[0],targetFace);
    set(power,scenario.target_extended?"redstone_block":"air");
    c.extras=[{pos:power,block:scenario.target_extended?"redstone_block":"air"}];
    c.extra_probes=[power,head,side(head,scenario.facing)];c.target_probes=[front[0],front[1]];
   }
   if(scenario.payload_kind==="grindstone"){
    const support=side(front[0],0);
    if(support.some((v,i)=>v!==base[i])){set(support,"stone");c.extras=[{pos:support,block:"stone"}];c.extra_probes=[support];}
   }
   if(sensorSuite){
    c.sensor=[base[0]+5,65,base[2]+5];c.sensor_input=[base[0]+5,65,base[2]+6];c.sensor_output=[base[0]+6,65,base[2]+5];
    cmd(`fill ${base[0]+4} 64 ${base[2]+4} ${base[0]+7} 64 ${base[2]+7} stone`);
    set(c.sensor_input,`daylight_detector ["redstone_signal"=${scenario.sensor_filter}]`);
    set(c.sensor_output,"redstone_wire");
    set(c.sensor,'calibrated_sculk_sensor ["minecraft:cardinal_direction"="north"]');
   }
   if(scenario.adhesion){
    c.extras=[];c.extra_probes=[];
    const perps=[0,1,2,3,4,5].filter(f=>(f>>1)!==(scenario.facing>>1));
    const add=(p,block)=>{set(p,block);c.extras.push({pos:p,block});c.extra_probes.push(p,side(p,scenario.facing));};
    if(scenario.adhesion==="cross")for(const f of perps)add(side(front[0],f),scenario.arm);
    else if(scenario.adhesion==="chain") {
     add(side(front[0],perps[0]),scenario.payload_kind);
     add(side(front[0],perps[0],2),scenario.payload_kind);
     add(side(front[0],perps[0],3),"stone");
     add(side(side(front[0],perps[0],2),perps[2]),"stone");
    } else if(scenario.adhesion==="forward_other") {
     add(front[1],scenario.payload_kind==="slime"?"honey_block":"slime");
     add(side(front[1],perps[0]),"stone");
    } else if(scenario.adhesion==="forward_push") {
     add(front[1],scenario.arm);
    } else if(scenario.adhesion==="back_branch") {
     const arm=side(front[0],perps[0]);add(arm,scenario.payload_kind);
     add(side(arm,scenario.facing^1),"stone");
    } else if(scenario.adhesion==="loop") {
     add(side(front[0],perps[0]),scenario.payload_kind);
     add(side(front[0],perps[2]),scenario.payload_kind);
     const corner=side(side(front[0],perps[0]),perps[2]);add(corner,scenario.payload_kind);
     add(side(corner,perps[0]),"stone");
    } else if(scenario.adhesion==="forward_obstruction") {
     const arm=side(front[0],perps[0]);add(arm,scenario.payload_kind);
     add(side(arm,scenario.facing),"obsidian");
    } else if(scenario.adhesion==="branch12"||scenario.adhesion==="branch13") {
     const limit=scenario.adhesion==="branch12"?12:13;
     for(let n=1;n<limit;n++)add(side(front[0],perps[0],n),n===limit-1?"stone":scenario.payload_kind);
    } else throw new Error("unknown adhesion layout");
   }
   if(scenario.kind==="lever")input(c,false);
   cells.push(c);
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 if(cases[0].tile_drops)for(const e of d.getEntities({type:"minecraft:item"})){
  if(seenItems.has(e.id))continue;
  const c=cells.find(c=>Math.abs(e.location.x-c.base[0])<8&&Math.abs(e.location.z-c.base[2])<8);if(!c)throw new Error("unowned native drop");
  seenItems.add(e.id);const it=e.getComponent("minecraft:item").itemStack;
  c.item_spawns.push({tick:system.currentTick,entity_id:e.id,id:it.typeId,count:it.amount,position:e.location});
  c.drop_counts[it.typeId]=(c.drop_counts[it.typeId]??0)+it.amount;
 }
 for(const c of cells){
  c.samples.push({tick:system.currentTick,base:read(c.base),actor:actor(c.base),source:read(c.source),conductor:c.conductor?read(c.conductor):null,front:c.front.map(read),extra:c.extra_probes?c.extra_probes.map(read):undefined});
  if(c.target_probes)c.samples.at(-1).target_actors=c.target_probes.map(p=>get(p).getComponent("minecraft:piston")?actor(p):null);
  if(c.scenario.tile_drops)c.samples.at(-1).drops=Object.entries(c.drop_counts).sort((a,b)=>a[0].localeCompare(b[0])).map(([id,count])=>({id,count}));
  if(sensorSuite)Object.assign(c.samples.at(-1),{sensor:signal(c.sensor),sensor_input:signal(c.sensor_input),sensor_output:signal(c.sensor_output)});
  if(t===30){
   const a=actor(c.base);
   if(a.moving||a.state.toLowerCase()!=="retracted")throw new Error("piston idle control failed "+JSON.stringify({scenario:c.scenario,actor:a}));
   if(c.scenario.payload&&read(c.front[0]).id!=="minecraft:stone")throw new Error("missing initial payload");
   if(large)for(let i=0;i<c.scenario.length;i++)if(read(c.front[i]).id!=="minecraft:"+c.scenario.payload_kind)throw new Error("missing large piston payload");
   if(c.extras)for(const b of c.extras)if(read(b.pos).id!=="minecraft:"+b.block)throw new Error("missing native adhesion layout");
   if(c.target_probes&&actor(c.front[0]).state.toLowerCase()!==(c.scenario.target_extended?"expanded":"retracted"))throw new Error("target piston state control failed");
  }
  const edge=40+c.scenario.phase;
  if(c.scenario.supported_popped&&t===edge+10){
   if(actor(c.base).state.toLowerCase()!=="expanded")throw new Error("popped extension control failed "+JSON.stringify({scenario:c.scenario,actor:actor(c.base),front:c.front.slice(0,3).map(read)}));
   const before=read(c.front[1]);
   set(c.front[1],c.scenario.payload_kind+(c.scenario.payload_states??""));
   c.injections.push({tick:system.currentTick,pos:c.front[1],before,block:read(c.front[1])});
  }
  const edges=[[edge,true],[edge+c.scenario.width,false]];
  if(c.scenario.gap!==undefined){const again=edge+c.scenario.width+c.scenario.gap;edges.push([again,true],[again+c.scenario.width,false]);}
  for(const [when,powered] of edges)if(t===when){
   input(c,powered);c.changes.push({tick:system.currentTick,powered});
  }
 }
 if(t===(sensorSuite?180:cases[0].gap===undefined?100:140)){
  for(const c of cells){
   if(c.changes.length!==(c.scenario.gap===undefined?2:4))throw new Error("missing piston input transitions");
   if(c.scenario.tile_drops){
    if(c.item_spawns.some(e=>e.tick<c.changes[0].tick)||Boolean(c.item_spawns.length)!==(c.scenario.payload_kind!=="candle"))throw new Error("native popped-drop control failed");
   }
   // A redstone block beyond stone is a weak-input negative control, not
   // an assumed strong-power source through that stone.
   const movableControl=!large||(c.scenario.payload_kind==="stone"&&c.scenario.length===12);
   if(movableControl&&(!c.scenario.conducted||c.scenario.kind==="lever")&&c.scenario.input===(c.scenario.facing^1)&&c.scenario.width===16&&!c.samples.some(s=>s.actor.moving))throw new Error("back input movement control failed "+JSON.stringify(c.scenario));
   if(sensorSuite){
    if(c.samples.some(s=>s.sensor_input.power!==c.scenario.sensor_filter))throw new Error("native piston sensor filter changed");
    const heard=c.samples.some(s=>s.sensor.states.sculk_sensor_phase===1);
    if(heard!==(c.scenario.payload_kind!=="obsidian"))throw new Error("native piston vibration control failed "+JSON.stringify(c.scenario));
    const heardBeforeOff=c.samples.some(s=>s.tick<c.changes[1].tick&&s.sensor.states.sculk_sensor_phase===1);
    if(heardBeforeOff!==(c.scenario.sensor_filter===10&&c.scenario.payload_kind!=="obsidian"))throw new Error("native piston event frequency control failed "+JSON.stringify(c.scenario));
   }
   emit(c);
  }
  offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def scenarios(suite):
    result = []
    for sticky in (False, True):
        for facing in range(6):
            for phase in (0, 1):
                if suite in ("faces", "all"):
                    for input_face in range(6):
                        for conducted in (False, True):
                            result.append(dict(sticky=sticky, facing=facing, phase=phase,
                                               input=input_face, conducted=conducted, width=16, payload=False))
                if suite in ("pulses", "all"):
                    for width in (1, 2, 3, 4, 8, 16):
                        result.append(dict(sticky=sticky, facing=facing, phase=phase,
                                           input=facing ^ 1, conducted=False, width=width, payload=True))
                if suite == "smoke" and facing == 5 and phase == 0:
                    result.append(dict(sticky=sticky, facing=facing, phase=phase,
                                       input=facing ^ 1, conducted=False, width=16, payload=True))
                if suite == "retrigger":
                    for width in (1, 2, 4, 16):
                        for gap in (0, 1, 2, 3, 4, 8):
                            result.append(dict(sticky=sticky, facing=facing, phase=phase,
                                               input=facing ^ 1, conducted=False, width=width, gap=gap, payload=True))
                if suite == "strong":
                    for input_face in range(6):
                        result.append(dict(sticky=sticky, facing=facing, phase=phase,
                                           input=input_face, conducted=True, width=16, payload=False, kind="lever"))
                if suite == "blocks":
                    for payload_kind, length in (("stone",12),("stone",13),("obsidian",1),("chest",1)):
                        result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                           conducted=False,width=16,payload=False,payload_kind=payload_kind,length=length))
                if suite == "materials":
                    for payload_kind in ("beacon", "calibrated_sculk_sensor", "creaking_heart", "enchanting_table",
                                         "ender_chest", "grindstone", "jukebox", "lodestone", "mob_spawner", "respawn_anchor",
                                         "sculk_catalyst", "sculk_sensor", "sculk_shrieker", "trial_spawner", "vault",
                                         "heavy_core", "black_glazed_terracotta", "red_glazed_terracotta", "stone", "chest"):
                        result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                           conducted=False,width=16,payload=False,payload_kind=payload_kind,length=1))
                if suite == "piston_payload":
                    for target_sticky in (False,True):
                        for target_extended in (False,True):
                            result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                               conducted=False,width=16,payload=False,payload_kind="sticky_piston" if target_sticky else "piston",
                                               length=1,target_extended=target_extended))
                if suite in ("popped", "popped_drops") and facing >= 2:
                    for payload_kind, states in (("redstone_wire", ""), ("unpowered_repeater", ""),
                                                 ("unpowered_comparator", ""), ("candle", ""),
                                                 ("torch", ' ["torch_facing_direction"="top"]'),
                                                 ("lever", ' ["lever_direction"="up_north_south"]'),
                                                 ("stone_button", ' ["facing_direction"=1]')):
                        result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                           conducted=False,width=16,payload=False,payload_kind=payload_kind,length=1,
                                           supported_popped=True,payload_states=states,tile_drops=suite=="popped_drops"))
                if suite == "adhesion":
                    for payload_kind in ("slime","honey_block"):
                        for arm in ("stone","slime","honey_block","obsidian"):
                            result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                               conducted=False,width=16,payload=False,payload_kind=payload_kind,length=1,
                                               adhesion="cross",arm=arm))
                        result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                           conducted=False,width=16,payload=False,payload_kind=payload_kind,length=1,
                                           adhesion="chain"))
                if suite == "push_only":
                    for payload_kind in ("slime", "honey_block"):
                        for arm in ("heavy_core", "black_glazed_terracotta", "red_glazed_terracotta"):
                            for adhesion in ("cross", "forward_push"):
                                result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                                   conducted=False,width=16,payload=False,payload_kind=payload_kind,length=1,
                                                   adhesion=adhesion,arm=arm))
                if suite == "adhesion_extended":
                    for payload_kind in ("slime","honey_block"):
                        for adhesion in ("forward_other","back_branch","loop","forward_obstruction","branch12","branch13"):
                            result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                               conducted=False,width=16,payload=False,payload_kind=payload_kind,length=1,
                                               adhesion=adhesion))
                if suite == "vibration":
                    for sensor_filter in (9,10):
                        for blocked in (False,True):
                            result.append(dict(sticky=sticky,facing=facing,phase=phase,input=facing ^ 1,
                                               conducted=False,width=60,payload=False,payload_kind="obsidian" if blocked else "stone",length=1,
                                               sensor_filter=sensor_filter))
    return result


if __name__ == "__main__":
    import json

    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--assets", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--port", type=int, default=30876)
    parser.add_argument("--offline-reference",action="store_true",help="Disable login only in the fresh loopback reference world")
    parser.add_argument("--suite", choices=("smoke", "faces", "pulses", "retrigger", "strong", "blocks", "materials", "piston_payload", "popped", "popped_drops", "push_only", "adhesion", "adhesion_extended", "vibration", "all"), default="all")
    args = parser.parse_args()
    cases = scenarios(args.suite)
    capture(args, SCRIPT.replace("CASES", json.dumps(cases)), len(cases))
