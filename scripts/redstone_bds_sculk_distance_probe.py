#!/usr/bin/env python3
"""Capture diagonal sensor signals after real player lever uses on untouched BDS."""

import itertools
import json

import redstone_bds_switch_use_probe as driver
from redstone_bds_timing_probe import arguments


POSITIONS = [(1, 65, 1), (2, 65, 2), (3, 65, 1), (3, 65, 3),
             (5, 65, 2), (5, 65, 5), (7, 65, 3), (8, 65, 1),
             (11, 65, 11), (15, 65, 5), (16, 65, 1), (-3, 65, -3)]
CASES = [{"sensor": sensor, "pos": pos, "request_phase": i & 1}
         for i, (sensor, pos) in enumerate(itertools.product(
             ("sculk_sensor", "calibrated_sculk_sensor"), POSITIONS))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null,lastSample=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates(),power:b.getRedstonePower()};};
const source=[0,65,0];
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(current&&e.player.name==="RedstoneAudit"&&e.block.location.x===0&&e.block.location.y===65&&e.block.location.z===0)
  current.changes.push({tick:system.currentTick,before_sample:lastSample!==system.currentTick});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
 if(current&&e.player.name==="RedstoneAudit"&&e.block.location.x===0&&e.block.location.y===65&&e.block.location.z===0)
  current.results.push({tick:system.currentTick,source:read(source)});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -32 0 -32 48 100 48 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("weather clear 100000");return;}
 if(!running){if(get([-32,65,-32])&&get([48,65,48])){
  if(!ready){cmd("fill -4 64 -4 20 64 20 stone");cmd("setworldspawn 0 65 2");ready=true;emit({ready:true});}
 }else if(system.currentTick-load>240)throw new Error("load timeout");return;}
 if(!current){
  const c=cases[index],p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("missing player");p.teleport({x:.5,y:65,z:2.5});
  cmd("fill -4 65 -4 20 68 20 air");cmd("fill -4 64 -4 20 64 20 stone");
  cmd('setblock 0 65 0 lever ["lever_direction"="up_north_south"]');
  const wire=[c.pos[0]+1,c.pos[1],c.pos[2]];cmd(`setblock ${wire.join(" ")} redstone_wire`);
  cmd(`setblock ${c.pos.join(" ")} ${c.sensor} ${c.sensor==="calibrated_sculk_sensor"?'["minecraft:cardinal_direction"="north"]':""}`);
  current={scenario:c,wire,setup_tick:system.currentTick,changes:[],results:[],samples:[],request_tick:0};return;
 }
 const c=current.scenario;
 current.samples.push({tick:system.currentTick,source:read(source),sensor:read(c.pos),wire:read(current.wire)});lastSample=system.currentTick;
 if(!current.request_tick&&system.currentTick-current.setup_tick>=60&&(system.currentTick&1)===c.request_phase){
  if(read(c.pos).states.sculk_sensor_phase!==0)throw new Error("sensor not idle before input");
  emit({action:{action:"click",block:source,face:1,hit:[.5,.05,.5],feet:[.5,65,2.5]},block_state:read(source)});current.request_tick=system.currentTick;
 }
 if(current.request_tick&&!current.changes.length&&system.currentTick-current.request_tick>60)throw new Error("native click missing");
 if(current.changes.length&&system.currentTick-current.changes[0].tick>=70){
  if(current.changes.length!==1||current.results.length!==1||!current.results[0].source.states.open_bit)throw new Error("native input positive control failed");
  emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    driver.SCRIPT = SCRIPT
    args = arguments()
    driver.capture_player_use(args, CASES)
    output = json.loads(args.output.read_text(encoding="utf-8"))
    output["reference"] = __doc__
    args.output.write_text(json.dumps(output, separators=(",", ":"))+"\n", encoding="utf-8")
