#!/usr/bin/env python3
"""Capture real player switch use accepted by an unmodified local BDS."""

import hashlib
import json

from redstone_bds_client import LocalClient, network_state_hash
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


BUTTONS = ["wooden_button", "spruce_button", "birch_button", "jungle_button", "acacia_button", "dark_oak_button",
           "crimson_button", "warped_button", "mangrove_button", "cherry_button", "pale_oak_button", "bamboo_button",
           "poplar_button", "stone_button", "polished_blackstone_button"]
CASES = [{"block": block, "pattern": pattern, "facing": 1}
         for block in BUTTONS for pattern in ("single", "repeat")]
CASES += [{"block": block, "pattern": pattern, "facing": facing}
          for block in ("wooden_button", "stone_button") for facing in (0, 2, 3, 4, 5) for pattern in ("single", "repeat")]
CASES += [{"block": "lever", "pattern": pattern, "facing": facing, "direction": direction}
          for direction, facing in (("down_east_west", 0), ("down_north_south", 0), ("up_east_west", 1),
                                    ("up_north_south", 1), ("north", 2), ("south", 3), ("west", 4), ("east", 5))
          for pattern in ("single", "repeat")]
for i, case in enumerate(CASES):
    case["request_phase"] = i & 1

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],hits=[[.5,.9375,.5],[.5,.0625,.5],[.5,.5,.9375],[.5,.5,.0625],[.9375,.5,.5],[.0625,.5,.5]];
let d,load=0,running=false,index=0,current=null,setupTick=0,lastSample=0,worldReady=false;
const cmd=x=>d.runCommand(x),read=x=>{const b=d.getBlock({x,y:65,z:0});return {id:b.typeId,states:b.permutation.getAllStates()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
    if(!current||e.player.name!=="RedstoneAudit"||e.block.location.x!==0||e.block.location.y!==65||e.block.location.z!==0)return;
    current.changes.push({kind:"click",tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,before:read(0)});
});
world.afterEvents.playerInteractWithBlock.subscribe(e=>{
    if(!current||e.player.name!=="RedstoneAudit"||e.block.location.x!==0||e.block.location.y!==65||e.block.location.z!==0)return;
    current.results.push({tick:system.currentTick,source:read(0)});
});
function request(){emit({action:{action:"click",block:[0,65,0],face:current.scenario.facing,hit:hits[current.scenario.facing],feet:current.feet},block_state:read(0)});current.requested++;current.requestTick=system.currentTick;}
function setup(){
    const c=cases[index];cmd("fill -3 63 -3 3 63 3 stone");cmd("fill -3 64 -3 3 67 3 air");
    const v=delta[c.facing],wx=c.facing===4?-1:1,feet=c.facing<2?[.5,64,2.5]:[.5+v[0]*2,64,.5+v[2]*2];
    cmd(`setblock ${-v[0]} ${65-v[1]} ${-v[2]} stone`);cmd(`setblock ${wx} 64 0 stone`);
    cmd(`setblock 0 65 0 ${c.block} ${c.block==="lever"?`["lever_direction"="${c.direction}"]`:`["facing_direction"=${c.facing}]`}`);
    cmd(`setblock ${wx} 65 0 redstone_wire`);
    const p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("audit player missing");
    p.teleport({x:feet[0],y:feet[1],z:feet[2]});
    setupTick=system.currentTick;current={scenario:c,feet,wx,samples:[],changes:[],results:[],requested:0,requestTick:0,setup_tick:setupTick};
}
const h=system.runInterval(()=>{try{
    if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 16 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
    if(!running){if(d.getBlock({x:0,y:65,z:0})){
        if(!worldReady){cmd("fill -3 63 -3 3 63 3 stone");cmd("setworldspawn 0 64 2");worldReady=true;emit({ready:true});}
    }else if(system.currentTick-load>200)throw new Error("load timeout");return;}
    if(!current){if(system.currentTick&1)return;setup();return;}
    current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),source:read(0),wire:read(current.wx)});lastSample=system.currentTick;
    if(!current.requested&&system.currentTick-setupTick>=8&&(system.currentTick&1)===current.scenario.request_phase)request();
    if(current.changes.length<current.requested&&system.currentTick-current.requestTick>60)throw new Error("native player did not accept click "+JSON.stringify(current.scenario));
    const first=current.changes[0]?.tick??0;
    if(first&&current.scenario.pattern==="repeat"&&current.requested===1&&system.currentTick-first>=5)request();
    if(first&&system.currentTick-first>=60){
        const expected=current.scenario.pattern==="repeat"?2:1;
        if(current.changes.length!==expected)throw new Error("missing/duplicate native interactions");
        if(!current.samples.some(s=>s.source.states.button_pressed_bit||s.source.states.open_bit))throw new Error("native click positive control failed");
        emit(current);index++;current=null;
        if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
    }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture_player_use(args, cases, require_parities=True):
    output = {"complete": False, "reference": "unmodified Windows BDS; native player interaction events and per-tick state readback",
              "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(), "scenarios": []}
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=SCRIPT.replace("CASES", json.dumps(cases)), local_client=True)
    client = None
    try:
        reference.until(lambda s: "ADAMANT_USE " in s and '"ready":true' in s, timeout=30)
        client = LocalClient(reference, args.port)
        reference.command("op RedstoneAudit")
        reference.command("scriptevent audit:start now")
        while True:
            lines = reference.until(lambda s: "ADAMANT_USE " in s, timeout=20)
            record = json.loads(lines[-1].split("ADAMANT_USE ", 1)[1].strip())
            if record.get("error"):
                raise RuntimeError(record)
            if "action" in record:
                record["action"]["runtime_id"] = network_state_hash(record["block_state"])
                client.send(record["action"])
            if "scenario" in record:
                output["scenarios"].append(record)
                print(json.dumps({"count": len(output["scenarios"]), "scenario": record["scenario"]}), flush=True)
            if record.get("complete"):
                if record["count"] != len(cases) or len(output["scenarios"]) != len(cases):
                    raise RuntimeError("incomplete native player-use capture")
                if require_parities and {c["changes"][0]["tick"] & 1 for c in output["scenarios"]} != {0, 1}:
                    raise RuntimeError("native player capture did not exercise both actual input parities")
                output["complete"] = True
                break
    finally:
        if client:
            client.stop()
        reference.stop()
        output["sounds"] = []
        if client:
            for line in client.log:
                try:
                    value = json.loads(line)
                except (ValueError, TypeError):
                    continue
                if "sound" in value and value["sound"]["SoundType"].startswith("button.click_"):
                    output["sounds"].append(value["sound"])
        output["log_directory"] = str(reference.root)
        output["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(output, separators=(",", ":"))+"\n", encoding="utf-8")
        print(json.dumps({"output": str(args.output), "complete": output["complete"], "logs": str(reference.root)}), flush=True)


if __name__ == "__main__":
    capture_player_use(arguments(), CASES)
