#!/usr/bin/env python3
"""Observe sensor phase/output after an actual native player lever interaction."""

import itertools
import json
import redstone_bds_switch_use_probe as driver
from redstone_bds_timing_probe import arguments

CASES = [{"sensor": sensor, "distance": distance, "request_phase": phase}
         for sensor, distance, phase in itertools.product(("sculk_sensor", "calibrated_sculk_sensor"), (1, 4, 7, 8, 9, 15, 16, 17), (0, 1))]
CASES += [{"sensor": sensor, "distance": 4, "request_phase": phase, "toggle": True}
          for sensor, phase in itertools.product(("sculk_sensor", "calibrated_sculk_sensor"), (0, 1))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null,lastSample=0;
const cmd=x=>d.runCommand(x),get=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,y,z)=>{const b=get(x,y,z);return {id:b.typeId,states:b.permutation.getAllStates(),power:typeof b.getRedstonePower==="function"?b.getRedstonePower():null};};
const player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
const request=()=>{emit({action:{action:"click",block:[0,65,0],face:1,hit:[.5,.05,.5],feet:[.5,65,2.5]},block_state:read(0,65,0)});current.request_tick=system.currentTick;current.requested++;};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(current&&e.player.name==="RedstoneAudit"&&e.block.location.x===0&&e.block.location.y===65&&e.block.location.z===0)
  current.changes.push({tick:system.currentTick,kind:"click",before_sample:lastSample!==system.currentTick});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
 if(current&&e.player.name==="RedstoneAudit"&&e.block.location.x===0&&e.block.location.y===65&&e.block.location.z===0)
  current.results.push({tick:system.currentTick,source:read(0,65,0)});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -32 0 -32 48 100 32 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("weather clear 100000");return;}
 if(!running){if(get(-32,65,-32)&&get(48,65,32)){
  if(!ready){cmd("fill -4 64 -4 22 64 4 stone");cmd("setworldspawn 0 65 2");ready=true;emit({ready:true});}
 }else if(system.currentTick-load>240)throw new Error("load timeout");return;}
 if(!current){
  const c=cases[index],x=c.distance,p=player();if(!p)throw new Error("missing audit player");p.teleport({x:.5,y:65,z:2.5});
  cmd("fill -4 65 -4 22 69 4 air");cmd("fill -4 64 -4 22 64 4 stone");
  cmd('setblock 0 65 0 lever ["lever_direction"="up_north_south"]');
  cmd(`setblock ${x+1} 65 0 unpowered_comparator ["minecraft:cardinal_direction"="west"]`);
  cmd(`setblock ${x+2} 65 0 redstone_wire`);cmd(`setblock ${x} 65 1 redstone_wire`);cmd(`setblock ${x} 65 -1 redstone_wire`);
  cmd(`setblock ${x} 65 0 ${c.sensor} ${c.sensor==="calibrated_sculk_sensor"?'["minecraft:cardinal_direction"="north"]':""}`);
  current={scenario:c,setup_tick:system.currentTick,changes:[],results:[],samples:[],request_tick:0,requested:0};return;
 }
 const c=current.scenario,x=c.distance;
 current.samples.push({tick:system.currentTick,source:read(0,65,0),sensor:read(x,65,0),north:read(x,65,-1),south:read(x,65,1),comparator:read(x+1,65,0),output:read(x+2,65,0)});lastSample=system.currentTick;
 if(!current.request_tick&&system.currentTick-current.setup_tick>=80&&(system.currentTick&1)===c.request_phase){
  if(get(x,65,0).permutation.getState("sculk_sensor_phase")!==0)throw new Error("sensor not idle before controlled input");
  request();
 }
 if(c.toggle&&current.changes.length===1&&current.requested===1&&system.currentTick-current.changes[0].tick>=60)request();
 if(current.requested>current.changes.length&&system.currentTick-current.request_tick>60)throw new Error("native player interaction missing");
 const expected=c.toggle?2:1;
 if(current.changes.length===expected&&system.currentTick-current.changes.at(-1).tick>=90){
  if(current.results.length!==expected||!current.results[0].source.states.open_bit||(c.toggle&&current.results[1].source.states.open_bit))throw new Error("lever input positive control failed");
  for(const change of current.changes)if(c.distance<=4&&!current.samples.some(s=>s.tick>=change.tick&&s.tick<change.tick+40&&s.sensor.states.sculk_sensor_phase===1))throw new Error("nearby native sensor never activated");
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
    output["reference"] = "unmodified Windows BDS; accepted native player lever use drives vibration listeners; per-tick sensor and circuit readback"
    args.output.write_text(json.dumps(output,separators=(",", ":"))+"\n",encoding="utf-8")
