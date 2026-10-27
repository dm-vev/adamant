#!/usr/bin/env python3
"""Capture filtering, obstruction and button vibrations via real player use."""

import argparse
import itertools
import json
from pathlib import Path

import redstone_bds_switch_use_probe as driver


COLOURS = ("white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
           "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black")


def cases(suite):
    if suite == "adjacent":
        result = [{"sensor": "calibrated_sculk_sensor", "facing": facing,
                   "source": source, "sensor_pos": [1,65,0]}
                  for facing, source in itertools.product(("north", "south", "west", "east"),
                                                          ("", "wooden_button", "stone_button"))]
        result += [{"sensor": "sculk_sensor", "facing": "north", "source": source,
                    "sensor_pos": [1,65,0]} for source in ("", "wooden_button", "stone_button")]
    elif suite == "buttons":
        result = [{"sensor": sensor, "facing": "north", "source": source}
                  for sensor, source, _ in itertools.product(("sculk_sensor", "calibrated_sculk_sensor"),
                                                            ("wooden_button", "stone_button"), (0, 1))]
    elif suite == "filters":
        result = [{"sensor": "calibrated_sculk_sensor", "facing": facing, "filter": power, "filter_front": False}
                  for facing, power in itertools.product(("north", "south", "west", "east"), (0, 9, 10, 15))]
        result += [{"sensor": "calibrated_sculk_sensor", "facing": facing, "filter": power, "filter_front": True}
                   for facing, power in itertools.product(("north", "south", "west", "east"), (9, 15))]
    else:
        materials = ("air", "stone", "glass", "white_carpet", "red_carpet", "moss_carpet", "snow_layer", "oak_leaves")
        materials += tuple(colour + "_wool" for colour in COLOURS)
        result = [{"sensor": sensor, "facing": "north", "barrier": material}
                  for sensor, material in itertools.product(("sculk_sensor", "calibrated_sculk_sensor"), materials)]
    for i, c in enumerate(result):
        c["request_phase"] = i & 1
    return result


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null,lastSample=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates(),power:b.getRedstonePower()};};
const add=(a,b)=>a.map((v,i)=>v+b[i]),neg=a=>a.map(v=>-v);
const directions={north:[0,0,-1],south:[0,0,1],west:[-1,0,0],east:[1,0,0]},source=[0,65,0];
let sensor=[4,65,0];
const request=()=>{emit({action:{action:"click",block:source,face:1,hit:[.5,.05,.5],feet:[.5,65,2.5]},block_state:read(source)});current.request_tick=system.currentTick;current.requested++;};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(current&&e.player.name==="RedstoneAudit"&&e.block.location.x===0&&e.block.location.y===65&&e.block.location.z===0)
  current.changes.push({tick:system.currentTick,kind:"click",before_sample:lastSample!==system.currentTick});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
 if(current&&e.player.name==="RedstoneAudit"&&e.block.location.x===0&&e.block.location.y===65&&e.block.location.z===0)
  current.results.push({tick:system.currentTick,source:read(source)});
});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -32 0 -32 48 100 32 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("time set 6001");cmd("weather clear 100000");return;}
 if(!running){if(get([-32,65,-32])&&get([48,65,32])){
  if(!ready){cmd("fill -4 64 -4 10 64 4 stone");cmd("setworldspawn 0 65 2");ready=true;emit({ready:true});}
 }else if(system.currentTick-load>240)throw new Error("load timeout");return;}
 if(!current){
  const c=cases[index],p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("missing audit player");p.teleport({x:.5,y:65,z:2.5});sensor=c.sensor_pos??[4,65,0];
  cmd("fill -4 65 -4 10 69 4 air");cmd("fill -4 64 -4 10 64 4 stone");
  if(c.source)cmd(`setblock 0 65 0 ${c.source} ["facing_direction"=1]`);
  else cmd('setblock 0 65 0 lever ["lever_direction"="up_north_south"]');
  const input=c.filter===undefined?null:add(sensor,c.filter_front?directions[c.facing]:neg(directions[c.facing]));
  const wires=Object.values(directions).map(v=>add(sensor,v)).filter(p=>(!input||p.some((v,i)=>v!==input[i]))&&p.some((v,i)=>v!==source[i]));
  for(const p of wires)cmd(`setblock ${p.join(" ")} redstone_wire`);
  if(input)cmd(`setblock ${input.join(" ")} daylight_detector ["redstone_signal"=${c.filter}]`);
  if(c.barrier&&c.barrier!=="air")cmd(`setblock 2 65 0 ${c.barrier}`);
  cmd(`setblock ${sensor.join(" ")} ${c.sensor} ${c.sensor==="calibrated_sculk_sensor"?'["minecraft:cardinal_direction"="'+c.facing+'"]':""}`);
  current={scenario:c,input,wires,setup_tick:system.currentTick,changes:[],results:[],samples:[],request_tick:0,requested:0};return;
 }
 const c=current.scenario;
 current.samples.push({tick:system.currentTick,source:read(source),sensor:read(sensor),input:current.input?read(current.input):null,barrier:read([2,65,0]),wires:current.wires.map(pos=>({pos,block:read(pos)}))});lastSample=system.currentTick;
 if(!current.request_tick&&system.currentTick-current.setup_tick>=80&&(system.currentTick&1)===c.request_phase){
  if(get(sensor).permutation.getState("sculk_sensor_phase")!==0)throw new Error("sensor not idle before controlled input");
  if(current.input&&read(current.input).power!==c.filter)throw new Error("native filter input strength not stable");
  request();
 }
 if(current.changes.length===1&&current.requested===1&&system.currentTick-current.changes[0].tick>=60)request();
 if(current.requested>current.changes.length&&system.currentTick-current.request_tick>60)throw new Error("native player interaction missing");
 if(current.changes.length===2&&system.currentTick-current.changes[1].tick>=90){
  if(current.results.length!==2)throw new Error("missing accepted inputs");
  if(c.source){if(!current.results.every(r=>r.source.states.button_pressed_bit))throw new Error("button input positive control failed");}
  else if(!current.results[0].source.states.open_bit||current.results[1].source.states.open_bit)throw new Error("lever input positive control failed");
  emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--"+name, type=Path, required=True)
    parser.add_argument("--suite", choices=("filters", "occlusion", "buttons", "adjacent"), required=True)
    parser.add_argument("--port", type=int, default=30867)
    args = parser.parse_args()
    driver.SCRIPT = SCRIPT
    driver.capture_player_use(args, cases(args.suite))
    output = json.loads(args.output.read_text(encoding="utf-8"))
    output["reference"] = "unmodified Windows BDS; real accepted player uses; per-tick source/sensor/wire readback"
    args.output.write_text(json.dumps(output, separators=(",", ":"))+"\n", encoding="utf-8")
