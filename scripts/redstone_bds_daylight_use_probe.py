#!/usr/bin/env python3
"""Capture accepted player inversion while the native detector clock is frozen."""

import itertools
import redstone_bds_switch_use_probe as player_probe
from redstone_bds_timing_probe import arguments

CASES = [{"inverted": inverted, "initial": initial, "request_phase": phase}
         for inverted, initial, phase in itertools.product((False, True), (0, 7, 15), (0, 1))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,running=false,index=0,current=null,ready=false,lastSample=0;
const cmd=x=>d.runCommand(x),read=()=>{const b=d.getBlock({x:0,y:65,z:0});return {id:b.typeId,states:b.permutation.getAllStates()};};
const readWire=()=>{const b=d.getBlock({x:1,y:65,z:0});return {id:b.typeId,states:b.permutation.getAllStates()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||e.block.location.x!==0||e.block.location.y!==65||e.block.location.z!==0)return;
 current.changes.push({kind:"click",tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,before:read()});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||e.block.location.x!==0||e.block.location.y!==65||e.block.location.z!==0)return;
 current.results.push({tick:system.currentTick,source:read()});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 audit true");load=system.currentTick;return;}
 if(!ready){
  if(!d.getBlock({x:0,y:65,z:0})){if(system.currentTick-load>200)throw new Error("load timeout");return;}
  cmd("fill -3 63 -3 3 63 3 stone");cmd("setworldspawn 0 64 2");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("time set 6001");ready=true;emit({ready:true});return;
 }
 if(!running)return;
 if(!current){
  const c=cases[index];cmd("fill -3 64 -3 3 68 3 air");cmd("fill 0 64 0 1 64 0 stone");
  cmd(`setblock 0 65 0 ${c.inverted?"daylight_detector_inverted":"daylight_detector"} ["redstone_signal"=${c.initial}]`);
  cmd("setblock 1 65 0 redstone_wire");
  const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("audit player missing");
  const feet=[.5,64,2.5];p.teleport({x:feet[0],y:feet[1],z:feet[2]});
  current={scenario:c,setup_tick:system.currentTick,feet,samples:[],changes:[],results:[],requested:false};return;
 }
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),source:read(),wire:readWire()});lastSample=system.currentTick;
 if(!current.requested&&system.currentTick-current.setup_tick>=8&&(system.currentTick&1)===current.scenario.request_phase){
  emit({action:{action:"click",block:[0,65,0],face:1,hit:[.5,.375,.5],feet:current.feet},block_state:read()});current.requested=true;current.request_tick=system.currentTick;
 }
 if(current.requested&&!current.changes.length&&system.currentTick-current.request_tick>60)throw new Error("native inversion not accepted");
 if(current.changes.length&&system.currentTick-current.changes[0].tick>=24){
  if(current.changes.length!==1||current.results.length!==1)throw new Error("missing/duplicate inversion event");
  const v=current.results[0].source,c=current.scenario;
  if(v.id.endsWith("_inverted")===c.inverted)throw new Error("inversion did not change variant");
  emit(current);current=null;index++;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    player_probe.SCRIPT = SCRIPT
    player_probe.capture_player_use(arguments(), CASES)
