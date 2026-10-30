#!/usr/bin/env python3
"""Read piston animation fields from an owned, unmodified original BDS.

The pack holds its own Script API callback for a bounded checkpoint while
ReadProcessMemory observes actor fields. No process write, injection, debugger
or thread suspension is used. Tick-component vtables are located independently
in the BDS image from a unique original-client instruction prefix.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct
import time

from redstone_bds_circuit_order_probe import Memory, occurrences, rva_at
from redstone_bds_probe import Reference
from redstone_native_probe import ClientPE


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_PISTON_ACTOR "+JSON.stringify(x)),payloadCount=PAYLOAD_COUNT;
let d,load=0,ready=false,start=0,running=false,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start"){start=system.currentTick;running=true;}});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 63 100 31 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!ready){
  if(!get([0,65,0])||!get([63,65,31])){if(system.currentTick-load>240)throw new Error("load timeout");return;}
  for(const sticky of [false,true])for(const phase of [0,1])for(const blocked of [false,true]){
   const i=cells.length,base=[4+(i%4)*16,65,4+Math.floor(i/4)*16],source=[base[0]-1,65,base[2]],front=Array.from({length:payloadCount+2},(_,j)=>[base[0]+j+1,65,base[2]]);
   cmd(`fill ${base[0]-3} 62 ${base[2]-3} ${base[0]+4} 68 ${base[2]+3} air`);
   cmd(`setblock ${base.join(" ")} ${sticky?"sticky_piston":"piston"} ["facing_direction"=4]`);
   for(let j=0;j<(blocked?1:payloadCount);j++)cmd(`setblock ${front[j].join(" ")} ${blocked?"obsidian":"stone"}`);
   cells.push({scenario:{sticky,phase,blocked,payload_count:payloadCount},base,source,front,samples:[],changes:[]});
  }
  ready=true;emit({ready:true,bases:cells.map(c=>c.base)});return;
 }
 if(!running)return;
 const t=system.currentTick-start;
 for(const c of cells){
  const p=get(c.base).getComponent("minecraft:piston");
  c.samples.push({tick:system.currentTick,state:p.state,moving:p.isMoving,base:read(c.base),source:read(c.source),front:c.front.map(read)});
  const on=40+((c.scenario.phase-((start+40)&1)+2)%2);
  if(t===on||t===on+16){const powered=t===on;cmd(`setblock ${c.source.join(" ")} ${powered?"redstone_block":"air"}`);c.changes.push({tick:system.currentTick,powered});}
 }
 if(t>=36&&t<=68){
  const deadline_ms=Date.now()+400;
  emit({checkpoint:true,tick:system.currentTick,deadline_ms});while(Date.now()<deadline_ms){}
 }
 if(t===90){for(const c of cells)emit(c);emit({complete:true,count:cells.length});system.clearRun(h);running=false;}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


def locate_vtable(bds, client):
    prefix = client.read(0x4AE13D0, 0x60)
    matches = list(occurrences(bds.data, prefix))
    if len(matches) != 1:
        raise RuntimeError("no unique BDS piston tick instruction prefix")
    tick = rva_at(bds, matches[0])
    header = struct.unpack_from("<I", bds.data, 0x3C)[0]
    image_base = struct.unpack_from("<Q", bds.data, header + 48)[0]
    pointers = list(occurrences(bds.data, struct.pack("<Q", image_base + tick)))
    if len(pointers) != 1:
        raise RuntimeError("no unique BDS piston tick-component vtable")
    # IVanillaTickBlockActorComponent has four accessors before tick.
    vtable = rva_at(bds, pointers[0])-0x20
    position = struct.unpack("<Q",bds.read(vtable+0x10,8))[0]-image_base
    if bds.read(position,5)!=bytes.fromhex("488d41c8c3"):
        raise RuntimeError("unexpected native tick-component position accessor")
    return vtable, {"tick_rva":hex(tick),"vtable_rva":hex(vtable),
                    "tick_slot":0x20,"client_prefix_rva":"0x4ae13d0","prefix_size":len(prefix),
                    "prefix_sha256":hashlib.sha256(prefix).hexdigest()}


def locate_consumer(bds,client):
    prefix=client.read(0x66D8900,0x40)
    matches=list(occurrences(bds.data,prefix))
    if len(matches)!=1: raise RuntimeError("no unique consumer evaluator prefix")
    evaluate=rva_at(bds,matches[0])
    header=struct.unpack_from("<I",bds.data,0x3C)[0]
    image_base=struct.unpack_from("<Q",bds.data,header+48)[0]
    tables=[]
    for p in occurrences(bds.data,struct.pack("<Q",image_base+evaluate)):
        table=rva_at(bds,p)-0x80
        get_type=struct.unpack("<Q",bds.read(table+0xD0,8))[0]-image_base
        if bds.read(get_type,8)==client.read(0x14D79C0,8): tables.append(table)
    if len(tables)!=1: raise RuntimeError("no unique piston consumer vtable")
    return tables[0],{"vtable_rva":hex(tables[0]),"evaluate_rva":hex(evaluate),
                      "client_prefix_rva":"0x66d8900","prefix_size":len(prefix),
                      "prefix_sha256":hashlib.sha256(prefix).hexdigest()}


def locate_moving(bds,client):
    prefix=client.read(0x4ADE460,0x40)
    matches=list(occurrences(bds.data,prefix))
    if len(matches)!=1: raise RuntimeError("no unique native moving-block tick prefix")
    tick=rva_at(bds,matches[0])
    header=struct.unpack_from("<I",bds.data,0x3C)[0]
    image_base=struct.unpack_from("<Q",bds.data,header+48)[0]
    pointers=list(occurrences(bds.data,struct.pack("<Q",image_base+tick)))
    if len(pointers)!=1: raise RuntimeError("no unique native moving-block tick vtable")
    table=rva_at(bds,pointers[0])-0x20
    position=struct.unpack("<Q",bds.read(table+0x10,8))[0]-image_base
    if bds.read(position,5)!=bytes.fromhex("488d41c8c3"): raise RuntimeError("unexpected moving-block position accessor")
    return table,{"tick_rva":hex(tick),"vtable_rva":hex(table),"tick_slot":0x20,
                  "client_prefix_rva":"0x4ade460","prefix_size":len(prefix),
                  "prefix_sha256":hashlib.sha256(prefix).hexdigest()}


def moving_actors(memory,vtable,bases):
    memory.segments.clear()
    memory.starts.clear()
    memory.snapshot()
    result=[]
    for pointer in memory.find(struct.pack("<Q",memory.base+vtable)):
        try:
            raw=memory.read(pointer-0x38,0x138)
            pos=struct.unpack_from("<iii",raw)
            parent=struct.unpack_from("<iii",raw,0x108)
            if parent not in bases or raw[0xC]!=0x13: continue
            live=memory.read(pointer-0x38,0x138)
            if raw!=live: raise RuntimeError("moving actor changed during checkpoint")
            main_vtable=struct.unpack_from("<Q",raw,0x28)[0]
            dirty_callback=struct.unpack("<Q",memory.read(main_vtable+0xC0,8))[0]
            result.append({"pos":pos,"piston_pos":parent,"tick_count":struct.unpack_from("<I",raw,0x40)[0],
                           "flags":raw[0x44],"expanding":bool(raw[0x114]),
                           "main_vtable_rva":hex(main_vtable-memory.base),"dirty_callback_rva":hex(dirty_callback-memory.base)})
        except (OSError,ValueError): continue
    return result


def actor(memory, pointer, pos, vtable):
    raw = memory.read(pointer, 0xE0)
    if struct.unpack_from("<Q", raw)[0] != memory.base + vtable:
        raise RuntimeError("piston tick-component identity changed")
    if struct.unpack("<iii",memory.read(pointer-0x38,12)) != tuple(pos):
        raise RuntimeError("piston actor position changed")
    begin,end = struct.unpack_from("<QQ",raw,0xC8)
    if end < begin or (end-begin)%12 or end-begin > 12*16:
        raise RuntimeError("invalid native attached-block vector")
    attached = list(struct.iter_unpack("<iii",memory.read(begin,end-begin))) if end>begin else []
    if raw != memory.read(pointer,0xE0):
        raise RuntimeError("piston actor changed during checkpoint read")
    return {"sticky":bool(raw[0xB0]),"progress":struct.unpack_from("<f",raw,0xB4)[0],
            "last_progress":struct.unpack_from("<f",raw,0xB8)[0],"old_state":raw[0xBE],
            "state":raw[0xBF],"new_state":raw[0xC0],"attached":attached}


if __name__ == "__main__":
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ("binary","assets","client","output"):
        parser.add_argument("--"+name,type=Path,required=True)
    parser.add_argument("--port",type=int,default=30876)
    parser.add_argument("--moving-actors",action="store_true",help="Include bounded heap scans of moving-block actors")
    parser.add_argument("--payload-count",type=int,choices=range(1,4),default=1)
    args=parser.parse_args()
    bds,client=ClientPE(args.binary),ClientPE(args.client)
    bds_hash,client_hash=hashlib.sha256(bds.data).hexdigest(),hashlib.sha256(client.data).hexdigest()
    if bds_hash!="76d547f82e02c18d0986c30b47132c9cc4171d0f2df1c00649e50ff35788b321" or client_hash!="8bbe24d2cf7a3d7acdb3fc7f3f821a4f24518bdbe6a83ce3afd424ecfce988a5":
        raise RuntimeError("reference image differs from inspected binaries")
    vtable,locator=locate_vtable(bds,client)
    consumer_vtable,consumer_locator=locate_consumer(bds,client)
    moving_vtable,moving_locator=locate_moving(bds,client) if args.moving_actors else (None,None)
    output={"complete":False,"reference":__doc__,"binary_sha256":bds_hash,"client_sha256":client_hash,
            "locator":locator,"consumer_locator":consumer_locator,"offline_reference":True,"sample_phase":"actor checkpoint after scripted changes; before the callback returns",
            "scenarios":[],"checkpoints":[]}
    script=SCRIPT.replace("PAYLOAD_COUNT",str(args.payload_count))
    if args.moving_actors:
        output["moving_locator"]=moving_locator
        script=script.replace('const deadline_ms=Date.now()+400;', 'const scan_moving=[43,44,45,46,47,48,59,60,61,62,63,64].includes(t);const deadline_ms=Date.now()+(scan_moving?2500:400);')
        script=script.replace('checkpoint:true,tick:system.currentTick,deadline_ms','checkpoint:true,tick:system.currentTick,deadline_ms,scan_moving')
    reference=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=script,local_client=True)
    memory=None
    try:
        line=reference.until(lambda s:"ADAMANT_PISTON_ACTOR " in s,timeout=45)[-1]
        record=json.loads(line.split("ADAMANT_PISTON_ACTOR ",1)[1])
        if not record.get("ready") or len(record["bases"])!=8:
            raise RuntimeError("missing native actor setup")
        memory=Memory(reference.process)
        memory.snapshot()
        candidates={tuple(p):[] for p in record["bases"]}
        for pointer in memory.find(struct.pack("<Q",memory.base+vtable)):
            try:
                pos=struct.unpack("<iii",memory.cached(pointer-0x38,12))
                if pos in candidates:
                    state=actor(memory,pointer,pos,vtable)
                    if state["state"]==state["new_state"]==0 and state["progress"]==state["last_progress"]==0:
                        candidates[pos].append(pointer)
            except (OSError,ValueError,RuntimeError):
                continue
        if any(len(p)!=1 for p in candidates.values()):
            raise RuntimeError("native piston actor lookup is not unique: "+str(candidates))
        actors={p:pointers[0] for p,pointers in candidates.items()}
        consumers={p:[] for p in actors}
        for pointer in memory.find(struct.pack("<Q",memory.base+consumer_vtable)):
            try:
                raw=memory.cached(pointer,0x98)
                pos=struct.unpack_from("<iii",raw,0x34)
                if pos in consumers and raw[0x55]==0 and struct.unpack_from("<Q",raw,0x90)[0]==0x20001:
                    consumers[pos].append(pointer)
            except (OSError,ValueError): continue
        if any(len(p)!=1 for p in consumers.values()): raise RuntimeError("piston consumer lookup is not unique")
        reference.command("scriptevent audit:start now")
        while True:
            line=reference.until(lambda s:"ADAMANT_PISTON_ACTOR " in s,timeout=45)[-1]
            record=json.loads(line.split("ADAMANT_PISTON_ACTOR ",1)[1])
            if record.get("error"):
                raise RuntimeError(record)
            if record.get("checkpoint"):
                record["actors"]=[{"pos":pos,**actor(memory,pointer,pos,vtable)} for pos,pointer in actors.items()]
                for entry in record["actors"]:
                    pointer=consumers[tuple(entry["pos"])][0]
                    raw=memory.read(pointer,0x98)
                    if struct.unpack_from("<Q",raw)[0]!=memory.base+consumer_vtable or raw[0x55]!=0:
                        raise RuntimeError("native consumer identity changed")
                    entry["consumer_strength"]=struct.unpack_from("<h",raw,0x4E)[0]
                    entry["consumer_old_strength"]=struct.unpack_from("<h",raw,0x50)[0]
                if record.get("scan_moving"):
                    record["moving_actors"]=moving_actors(memory,moving_vtable,set(actors))
                if time.time()*1000 >= record["deadline_ms"]:
                    raise RuntimeError("native piston checkpoint deadline missed")
                output["checkpoints"].append(record)
            if "samples" in record:
                output["scenarios"].append(record)
            if record.get("complete"):
                if record["count"]!=8 or len(output["scenarios"])!=8 or len(output["checkpoints"])!=33:
                    raise RuntimeError("incomplete native piston actor capture")
                for case in output["scenarios"]:
                    if len(case["samples"])!=90 or len(case["changes"])!=2 or case["changes"][0]["tick"]%2!=case["scenario"]["phase"]:
                        raise RuntimeError("invalid native actor input parity or trace length")
                    moved=any(s["moving"] for s in case["samples"])
                    if moved==case["scenario"]["blocked"]:
                        raise RuntimeError("native blocked/moving control failed")
                    powers=[a["consumer_strength"] for k in output["checkpoints"] for a in k["actors"] if tuple(a["pos"])==tuple(case["base"])]
                    if 15 not in powers or 0 not in powers:
                        raise RuntimeError("native consumer input control failed")
                output["complete"]=True
                break
    finally:
        if memory: memory.close()
        reference.stop()
        output["log_directory"]=str(reference.root)
        output["build_log"]=[s for s in reference.log if "Version:" in s or "Build ID:" in s]
        args.output.write_text(json.dumps(output,separators=(",",":"))+"\n",encoding="utf-8")
        print(json.dumps({"complete":output["complete"],"checkpoints":len(output["checkpoints"]),"logs":str(reference.root)}))
