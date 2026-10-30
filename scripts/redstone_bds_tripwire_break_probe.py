#!/usr/bin/env python3
"""Capture actual native player cutting, with and without shears, over loopback."""

import hashlib
import json

from redstone_bds_client import LocalClient, network_state_hash
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments


CASES = [{"game_mode": mode, "tool": tool, "length": length, "cut": cut, "direction": 1}
         for length in (1, 3, 40, 41) for cut in sorted({0, length//2, length-1})
         for mode in ('creative', 'survival') for tool in ('air', 'shears')]
CASES += [{"game_mode": mode, "tool": tool, "length": 3, "cut": 1, "direction": direction}
          for direction in (0, 2, 3) for mode in ('creative', 'survival') for tool in ('air', 'shears')]
CASES += [{"game_mode": 'survival', "tool": tool, "length": 40, "cut": 39, "direction": direction}
          for direction in (0, 2, 3) for tool in ('air', 'shears')]
for i, case in enumerate(CASES):
    case['request_phase'] = i & 1

SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_BREAK "+JSON.stringify(x));
const delta=[[0,-1],[1,0],[0,1],[-1,0]],nativeDirection=[2,3,0,1];
let d,load=0,ready=false,running=false,index=0,current=null,direction=1;
const cmd=x=>d.runCommand(x),get=x=>d.getBlock({x:x*delta[direction][0],y:65,z:x*delta[direction][1]});
const set=(x,name,states="")=>cmd(`setblock ${x*delta[direction][0]} 65 ${x*delta[direction][1]} ${name} ${states}`);
const read=x=>{const b=get(x);if(!b)throw new Error("unloaded tripwire");return {id:b.typeId,states:b.permutation.getAllStates()};};
const player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
const held=p=>p.getComponent("minecraft:inventory").container.getItem(0)?.typeId??"minecraft:air";
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerBreakBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit")return;
 if(!current.requested)throw new Error("native break occurred before the requested input");
 current.changes.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:e.block.location,before:read(current.scenario.cut),held:held(e.player),game_mode:e.player.getGameMode()});
});
world.afterEvents.playerBreakBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit")return;
 current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:e.block.location,source:read(current.scenario.cut),held:held(e.player)});
});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();
 const c=cases[index];direction=c.direction;const v=delta[direction];
 cmd("fill -44 63 -44 44 63 44 stone");for(let y=64;y<=67;y++)cmd(`fill -44 ${y} -44 44 ${y} 44 air`);
 set(-2,"stone");set(c.length+1,"stone");
 set(-1,"tripwire_hook",`["direction"=${nativeDirection[direction]}]`);set(c.length,"tripwire_hook",`["direction"=${nativeDirection[(direction+2)%4]}]`);
 for(let x=0;x<c.length;x++)set(x,"trip_wire");
 cmd(`setblock ${-3*v[0]} 64 ${-3*v[1]} stone`);set(-3,"redstone_wire");
 const p=player();if(!p)throw new Error("audit player missing");
 cmd(`gamemode ${c.game_mode} "RedstoneAudit"`);
 p.getComponent("minecraft:inventory").container.setItem(0,c.tool==="air"?undefined:new ItemStack("minecraft:"+c.tool,1));
 p.selectedSlotIndex=0;
 const feet=[c.cut*v[0]+.5-v[1]*2,64,c.cut*v[1]+.5+v[0]*2];p.teleport({x:feet[0],y:feet[1],z:feet[2]});
 current={scenario:c,setup_tick:system.currentTick,feet,changes:[],results:[],samples:[],requested:0};
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add -48 0 -48 48 100 48 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");load=system.currentTick;return;}
 if(!running){if(get(0)){
  if(!ready){cmd("fill -4 63 -4 8 63 4 stone");cmd("setworldspawn 1 64 2");ready=true;emit({ready:true});}
 }else if(system.currentTick-load>200)throw new Error("load timeout");return;}
 if(!current){if(system.currentTick&1)return;setup();return;}
 const c=current.scenario;
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),left:read(-1),right:read(c.length),strings:Array.from({length:c.length},(_,x)=>read(x)),wire:read(-3)});
 if(!current.requested&&system.currentTick-current.setup_tick>=10&&(system.currentTick&1)===c.request_phase){
  const p=player();if(held(p)!=="minecraft:"+c.tool)throw new Error("native held-item setup failed");
  emit({action:{action:"break",block:[c.cut*delta[direction][0],65,c.cut*delta[direction][1]],face:1,hit:[.5,.1,.5],feet:current.feet},block_state:read(c.cut)});current.requested=system.currentTick;
 }
 if(current.requested&&!current.results.length&&system.currentTick-current.requested>60)throw new Error("native player break was not accepted "+JSON.stringify(c));
 if(current.results.length&&system.currentTick-current.results[0].tick>=50){
  const target={x:c.cut*delta[direction][0],y:65,z:c.cut*delta[direction][1]};
  if(!current.requested||current.changes[0]?.tick<current.requested||current.changes.length!==1||current.results.length!==1||current.results[0].source.id!=="minecraft:air"||current.changes[0].held!=="minecraft:"+c.tool||current.changes[0].game_mode.toLowerCase()!==c.game_mode||JSON.stringify(current.changes[0].position)!==JSON.stringify(target)||JSON.stringify(current.results[0].position)!==JSON.stringify(target))throw new Error("native break positive control failed");
  emit(current);current=null;index++;
  if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def capture_breaks(args, cases=CASES):
    output = {"complete": False, "reference": "unmodified BDS; actual native player-break events and tick readback",
              "binary_sha256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
              "world_rules": {"doTileDrops": False}, "scenarios": []}
    reference = Reference(args.binary.resolve(), args.assets.resolve(), args.port,
                          script=SCRIPT.replace("CASES", json.dumps(cases)), local_client=True)
    client = None
    try:
        reference.until(lambda s: "ADAMANT_BREAK " in s and '"ready":true' in s, timeout=30)
        client = LocalClient(reference, args.port)
        reference.command("op RedstoneAudit")
        reference.command("scriptevent audit:start now")
        while True:
            lines = reference.until(lambda s: "ADAMANT_BREAK " in s, timeout=20)
            record = json.loads(lines[-1].split("ADAMANT_BREAK ", 1)[1].strip())
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
                    raise RuntimeError("incomplete native cutting capture")
                if len(cases)>1 and {c["changes"][0]["game_tick"]&1 for c in output["scenarios"]}!={0,1}:
                    raise RuntimeError("native cutting did not cover both actual input parities")
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
                    record = json.loads(line)
                except ValueError:
                    continue
                if "sound" in record:
                    output["sounds"].append(record["sound"])
        output["log_directory"] = str(reference.root)
        output["build_log"] = [s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(output, separators=(",", ":"))+"\n", encoding="utf-8")
        print(json.dumps({"complete": output["complete"], "logs": str(reference.root)}), flush=True)


if __name__ == "__main__":
    capture_breaks(arguments())
