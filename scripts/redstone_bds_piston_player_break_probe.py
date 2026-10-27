#!/usr/bin/env python3
"""Capture actual original-BDS player breaks of piston bases and heads."""

import argparse
import hashlib
import itertools
import json
from pathlib import Path

from redstone_bds_client import LocalClient, network_state_hash
from redstone_bds_probe import Reference


SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_PISTON_BREAK "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
let d,load=0,ready=false,running=false,index=0,current;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const side=(p,f)=>p.map((v,i)=>v+delta[f][i]);
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded break cell");return {id:b.typeId,states:b.permutation.getAllStates()};};
const actor=p=>{const a=get(p).getComponent("minecraft:piston");return a?{state:a.state,moving:a.isMoving,attached:a.getAttachedBlocksLocations()}:null;};
const snapshot=()=>({base:read(current.base),head:read(current.head),source:read(current.source),actor:actor(current.base)});
const player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
const held=p=>p.getComponent("minecraft:inventory").container.getItem(0)?.typeId??"minecraft:air";
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerBreakBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit")return;
 if(!current.requested)throw new Error("unsolicited native piston break");
 current.changes.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:e.block.location,held:held(e.player),game_mode:e.player.getGameMode(),before:snapshot()});
});
world.afterEvents.playerBreakBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit")return;
 current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:e.block.location,after:snapshot()});
});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();
 cmd("fill -5 63 -5 7 63 7 stone");cmd("fill -5 64 -5 7 70 7 air");
 const scenario=cases[index],base=[0,65,0],head=side(base,scenario.facing),source=side(base,scenario.facing^1);
 cmd(`setblock ${base.join(" ")} ${scenario.sticky?"sticky_piston":"piston"} ["facing_direction"=${nativeFace[scenario.facing]}]`);
 cmd(`setblock ${source.join(" ")} redstone_block`);
 const p=player();cmd(`gamemode ${scenario.game_mode} "${p.name}"`);
 p.selectedSlotIndex=0;p.getComponent("minecraft:inventory").container.setItem(0,scenario.game_mode==="creative"?undefined:new ItemStack("minecraft:diamond_pickaxe",1));
 const perpendicular=[0,1,2,3,4,5].find(f=>f>=2&&(f>>1)!==(scenario.facing>>1)),v=delta[perpendicular];
 const feet=[.5+v[0]*2.5,64,.5+v[2]*2.5];p.teleport({x:feet[0],y:feet[1],z:feet[2]});
 current={scenario,base,head,source,feet,setup_tick:system.currentTick,setup_game_tick:world.getAbsoluteTime(),changes:[],results:[],samples:[],requested:0};
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 16 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!running){if(get([-5,65,-5])&&get([7,65,7])){if(!ready){cmd("fill -5 63 -5 7 63 7 stone");cmd("setworldspawn 3 64 3");ready=true;emit({ready:true});}}else if(system.currentTick-load>200)throw new Error("load timeout");return;}
 if(!current){if(system.currentTick&1)return;setup();return;}
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),...snapshot()});
 if(!current.requested&&system.currentTick-current.setup_tick>=40&&(system.currentTick&1)===current.scenario.request_phase){
  const a=actor(current.base);if(a.state.toLowerCase()!=="expanded"||a.moving||!read(current.head).id.endsWith("piston_arm_collision"))throw new Error("native extended-piston control failed");
  const position=current.scenario.target==="head"?current.head:current.base;
  emit({action:{action:"break",block:position,face:1,hit:[.5,.5,.5],feet:current.feet,break_ticks:current.scenario.game_mode==="survival"?20:0},block_state:read(position)});current.requested=system.currentTick;
 }
 if(current.requested&&!current.results.length&&system.currentTick-current.requested>80)throw new Error("native piston break was not accepted "+JSON.stringify(current.scenario));
 if(current.results.length&&system.currentTick-current.results[0].tick>=20){
  const position=current.scenario.target==="head"?current.head:current.base;
  const before=current.changes[0],after=current.results[0];
  if(current.changes.length!==1||current.results.length!==1||before.tick<current.requested||JSON.stringify(position)!==JSON.stringify([before.position.x,before.position.y,before.position.z])||before.game_mode.toLowerCase()!==current.scenario.game_mode)throw new Error("native piston break control failed");
  emit(current);current=null;index++;if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("binary", "assets", "output"):
        parser.add_argument("--" + name, type=Path, required=True)
    parser.add_argument("--port", type=int, default=30876)
    parser.add_argument("--mode", choices=("creative", "survival"), default="creative")
    args = parser.parse_args()
    cases = [dict(game_mode=args.mode, sticky=s, facing=f, target=t, request_phase=i & 1)
             for i, (s, f, t) in enumerate(itertools.product((False, True), range(6), ("base", "head")))]
    output = dict(complete=False, reference=__doc__, binary_sha256=hashlib.sha256(args.binary.read_bytes()).hexdigest(),
                  offline_reference=True, scenarios=[], sounds=[], block_actors=[], level_events=[])
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=SCRIPT.replace("CASES", json.dumps(cases)), local_client=True)
    client = None
    try:
        reference.until(lambda s: "ADAMANT_PISTON_BREAK " in s and '"ready":true' in s, timeout=45)
        client = LocalClient(reference, args.port)
        reference.command("op RedstoneAudit")
        reference.command("scriptevent audit:start now")
        while True:
            line = reference.until(lambda s: "ADAMANT_PISTON_BREAK " in s, timeout=30)[-1]
            record = json.loads(line.split("ADAMANT_PISTON_BREAK ", 1)[1])
            if record.get("error"):
                raise RuntimeError(record)
            if "action" in record:
                record["action"]["runtime_id"] = network_state_hash(record["block_state"])
                client.send(record["action"])
            if "scenario" in record:
                output["scenarios"].append(record)
                print(json.dumps(dict(count=len(output["scenarios"]), scenario=record["scenario"])), flush=True)
            if record.get("complete"):
                if record["count"] != len(cases) or len(output["scenarios"]) != len(cases):
                    raise RuntimeError("incomplete native player-break capture")
                output["complete"] = True
                break
    finally:
        if client:
            client.stop()
            action_index = -1
            for line in client.log:
                try:
                    entry = json.loads(line)
                except (ValueError, TypeError):
                    continue
                if "sound" in entry:
                    output["sounds"].append(entry["sound"])
                if "block_actor" in entry:
                    output["block_actors"].append(entry["block_actor"])
                if entry.get("sent") == "interaction":
                    action_index += 1
                    if action_index < len(output["scenarios"]):
                        case = output["scenarios"][action_index]
                        expected = case[case["scenario"]["target"]]
                        if entry["block"] != expected:
                            output["complete"] = False
                            output["error"] = "native client action order differs from scenarios"
                        case["level_events"] = []
                if "level_event" in entry:
                    event = entry["level_event"]
                    output["level_events"].append(event)
                    if 0 <= action_index < len(output["scenarios"]):
                        output["scenarios"][action_index]["level_events"].append(event)
            if output["complete"] and action_index + 1 != len(output["scenarios"]):
                output["complete"] = False
                output["error"] = "native client action count differs from scenarios"
        reference.stop()
        output["log_directory"] = str(reference.root)
        output["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(output, separators=(",", ":")) + "\n", encoding="utf-8")
        print(json.dumps(dict(complete=output["complete"], scenarios=len(output["scenarios"]), logs=str(reference.root))), flush=True)
