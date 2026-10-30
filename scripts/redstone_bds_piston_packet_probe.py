#!/usr/bin/env python3
"""Observe original-BDS piston sound and block-actor packets on a loopback client.

Script API drives eight isolated nearby pistons after the client has spawned.
Packet positions identify their scenarios. The client input clock is not used
as a native simulation timestamp.
"""

import argparse
import hashlib
import json
from pathlib import Path

from redstone_bds_client import LocalClient
from redstone_bds_probe import Reference


SCRIPT=r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_PISTON_PACKET "+JSON.stringify(x)),facings=FACINGS,payloadCount=PAYLOAD_COUNT;
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],nativeFace=[0,1,3,2,5,4];
let d,load=0,ready=false,running=false,start=0,cells=[],facingIndex=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 31 100 31 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){if(!get([-4,65,-4])||!get([20,65,20])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  cmd("fill -4 63 -4 20 63 20 stone");cmd("fill -4 64 -4 20 70 20 air");cmd("setworldspawn 8 64 8");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!start){
  if(system.currentTick&1)return;
  cmd("fill -4 63 -4 30 63 20 stone");cmd("fill -4 64 -4 30 70 20 air");
  const facing=facings[facingIndex];
  for(const sticky of [false,true])for(const phase of [0,1])for(const blocked of [false,true]){
   const i=cells.length,spacing=3+payloadCount,base=[(i%4)*spacing,65,Math.floor(i/4)*spacing],source=base.map((v,k)=>v-delta[facing][k]),front=Array.from({length:payloadCount+2},(_,j)=>base.map((v,k)=>v+delta[facing][k]*(j+1)));
   cmd(`fill ${base[0]-1} 61 ${base[2]-1} ${base[0]+1} 68 ${base[2]+1} air`);
   cmd(`setblock ${base.join(" ")} ${sticky?"sticky_piston":"piston"} ["facing_direction"=${nativeFace[facing]}]`);
   for(let j=0;j<(blocked?1:payloadCount);j++)cmd(`setblock ${front[j].join(" ")} ${blocked?"obsidian":"stone"}`);
   cells.push({scenario:{sticky,phase,blocked,facing,payload_count:payloadCount},base,source,front,setup_tick:system.currentTick,setup_time_ms:Date.now(),samples:[],changes:[]});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  const p=get(c.base).getComponent("minecraft:piston");
  c.samples.push({tick:system.currentTick,observed_at_ms:Date.now(),actor:{state:p.state,moving:p.isMoving},base:read(c.base),source:read(c.source),front:c.front.map(read)});
  const edge=40+c.scenario.phase;
  if(t===edge||t===edge+16){const powered=t===edge;cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t===100){for(const c of cells){
   if(c.changes.length!==2||c.changes[0].tick%2!==c.scenario.phase||c.samples.some(s=>s.actor.moving)===c.scenario.blocked)throw new Error("native packet movement control failed");
   c.end_time_ms=Date.now();emit(c);
  }
  facingIndex++;if(facingIndex===facings.length){emit({complete:true,count:8*facings.length});system.clearRun(h);running=false;}
  else {cells=[];start=0;}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__=="__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("binary","assets","output"): parser.add_argument("--"+name,type=Path,required=True)
    parser.add_argument("--port",type=int,default=30876)
    parser.add_argument("--all-directions",action="store_true")
    parser.add_argument("--payload-count",type=int,choices=range(1,4),default=1)
    args=parser.parse_args()
    facings=list(range(6)) if args.all_directions else [5]
    output={"complete":False,"reference":__doc__,"binary_sha256":hashlib.sha256(args.binary.read_bytes()).hexdigest(),
            "offline_reference":True,"scenarios":[],"sounds":[],"block_actors":[],"packet_observations":[]}
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT.replace("FACINGS",json.dumps(facings)).replace("PAYLOAD_COUNT",str(args.payload_count)),local_client=True)
    client=None
    try:
        reference.until(lambda s:"ADAMANT_PISTON_PACKET " in s and '"ready":true' in s,timeout=45)
        client=LocalClient(reference,args.port)
        print(json.dumps({"client_ready":True}),flush=True)
        reference.command("op RedstoneAudit")
        reference.command("scriptevent audit:start now")
        while True:
            line=reference.until(lambda s:"ADAMANT_PISTON_PACKET " in s,timeout=30)[-1]
            record=json.loads(line.split("ADAMANT_PISTON_PACKET ",1)[1])
            if record.get("error"): raise RuntimeError(record)
            if "scenario" in record: output["scenarios"].append(record)
            if record.get("complete"):
                if record["count"]!=8*len(facings) or len(output["scenarios"])!=8*len(facings): raise RuntimeError("incomplete native piston packet scenarios")
                output["complete"]=True
                break
    finally:
        if client:
            client.stop()
            for line in client.log:
                try: record=json.loads(line)
                except (ValueError,TypeError): continue
                if "sound" in record and record["sound"]["SoundType"].startswith("piston."):
                    output["sounds"].append(record["sound"])
                    output["packet_observations"].append(record)
                if "block_actor" in record:
                    output["block_actors"].append(record["block_actor"])
                    output["packet_observations"].append(record)
        reference.stop()
        if output["complete"]:
            for c in output["scenarios"]:
                observed=[r for r in output["packet_observations"] if c["setup_time_ms"]<=r["received_at_ms"]<=c["end_time_ms"]]
                sounds=[r["sound"] for r in observed if "sound" in r and r["sound"]["Position"]==c["base"]]
                actors=[r["block_actor"] for r in observed if "block_actor" in r and r["block_actor"]["Position"]==c["base"]]
                if len(sounds)!=(0 if c["scenario"]["blocked"] else 2) or not actors:
                    output["complete"]=False
                    output["error"]="missing native piston sound/actor packets"
        output["log_directory"]=str(reference.root)
        output["build_log"]=[s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(output,separators=(",",":"))+"\n",encoding="utf-8")
        print(json.dumps({"complete":output["complete"],"sounds":len(output["sounds"]),"block_actors":len(output["block_actors"]),"logs":str(reference.root)}),flush=True)
        if not output["complete"]: raise RuntimeError("native piston packet capture incomplete")
