#!/usr/bin/env python3
"""Capture plate sound packets and adjacent calibrated-input behavior on BDS.

Native armour stands drive the plate. A far sensor with stable input 10 is a
positive control; an adjacent sensor reads the plate as its calibration input.
A native comparator records the adjacent sensor's heard frequency separately
from the source sound packets received by an actual local player client.
"""

import hashlib
import json

from redstone_bds_client import LocalClient
from redstone_bds_probe import Reference
from redstone_bds_timing_probe import arguments
from redstone_bds_pressure_plate_probe import BLOCKS


CASES=[{"block":name,"filter":10,"phase":i&1,"capture_from":60} for i,name in enumerate(BLOCKS)]

SCRIPT=r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_PLATE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates(),power:b.getRedstonePower()};};
const source=[0,65,0],sensor=[4,65,0],input=[4,65,1],output=[5,65,0],wire=[-1,65,0],near=[1,65,0],nearOutput=[2,65,0],comparator=[1,65,-1];
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -32 0 -32 32 100 32 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dodaylightcycle false");cmd("time set 6001");cmd("weather clear 100000");return;}
 if(!running){if(get([-32,65,-32])&&get([32,65,32])){
  if(!ready){cmd("fill -4 64 -4 9 64 4 stone");cmd("setworldspawn -3 65 2");ready=true;emit({ready:true});}
 }else if(system.currentTick-load>240)throw new Error("load timeout");return;}
 if(!current){
  const c=cases[index],p=world.getAllPlayers().find(p=>p.name==="RedstoneAudit");if(!p)throw new Error("missing audit player");p.teleport({x:-2.5,y:65,z:2.5});
  cmd("fill -4 65 -4 9 69 4 air");cmd("fill -4 64 -4 9 64 4 stone");
  cmd(`setblock ${source.join(" ")} ${c.block}`);cmd(`setblock ${wire.join(" ")} redstone_wire`);
  cmd(`setblock ${nearOutput.join(" ")} redstone_wire`);cmd(`setblock ${output.join(" ")} redstone_wire`);
  cmd(`setblock ${input.join(" ")} daylight_detector ["redstone_signal"=10]`);
  cmd(`setblock ${comparator.join(" ")} unpowered_comparator ["minecraft:cardinal_direction"="south"]`);
  cmd(`setblock ${sensor.join(" ")} calibrated_sculk_sensor ["minecraft:cardinal_direction"="north"]`);
  cmd(`setblock ${near.join(" ")} calibrated_sculk_sensor ["minecraft:cardinal_direction"="east"]`);
  const setup=system.currentTick,edge=80+((c.phase-((setup+80)&1)+2)%2);
  current={scenario:c,x:0,z:0,source,sensor,input,output,wire,near_sensor:near,near_output:nearOutput,comparator,setup_tick:setup,edge,entities:[],samples:[],changes:[]};return;
 }
 const c=current.scenario,t=system.currentTick-current.setup_tick;
 current.entities=current.entities.filter(e=>e.isValid);
 if(t>=c.capture_from){
  const entities=current.entities.map(e=>({id:e.typeId,entity_id:e.id,position:e.location,aabb:e.getAABB()}));
  current.samples.push({tick:system.currentTick,source:read(source),sensor:read(sensor),input:read(input),output:read(output),wire:read(wire),near_sensor:read(near),near_output:read(nearOutput),comparator:read(comparator),entities});
  if(t===c.capture_from&&(read(source).power!==0||read(sensor).states.sculk_sensor_phase!==0||read(near).states.sculk_sensor_phase!==0||read(comparator).power!==0))throw new Error("source/listeners not settled before capture");
 }
 if(t===current.edge||t===current.edge+35){
  const count=t===current.edge?1:(c.block==="heavy_weighted_pressure_plate"?10:1);
  for(let n=0;n<count;n++)current.entities.push(d.spawnEntity("minecraft:armor_stand",{x:.5,y:65.02,z:.5}));
  current.changes.push({tick:system.currentTick,kind:"spawn",count});
 }
 if(t===current.edge+70){for(const e of current.entities)e.remove();current.entities=[];current.changes.push({tick:system.currentTick,kind:"remove"});}
 for(const e of current.entities){e.teleport({x:.5,y:65.02,z:.5});e.clearVelocity();}
 if(t>=210){
  if(current.changes.length!==3||!current.samples.some(s=>s.entities.length>1)||!current.samples.some(s=>s.sensor.states.sculk_sensor_phase===1))throw new Error("plate/control sensor positive control failed");
  if(current.samples.some(s=>s.input.power!==10))throw new Error("control filter not stable");
  delete current.entities;emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__=="__main__":
    args=arguments()
    output={"complete":False,"reference":__doc__,"binary_sha256":hashlib.sha256(args.binary.read_bytes()).hexdigest(),"scenarios":[]}
    ref=Reference(args.binary.resolve(),args.assets.resolve(),args.port,script=SCRIPT.replace("CASES",json.dumps(CASES)),local_client=True)
    client=None
    try:
        ref.until(lambda s:"ADAMANT_PLATE " in s and '"ready":true' in s,timeout=30)
        client=LocalClient(ref,args.port)
        print(json.dumps({"client_ready":True}),flush=True)
        ref.command("op RedstoneAudit")
        ref.command("scriptevent audit:start now")
        while True:
            lines=ref.until(lambda s:"ADAMANT_PLATE " in s,timeout=30)
            record=json.loads(lines[-1].split("ADAMANT_PLATE ",1)[1].strip())
            if record.get("error"):raise RuntimeError(record)
            if "scenario" in record:
                output["scenarios"].append(record)
                print(json.dumps({"count":len(output["scenarios"]),"scenario":record["scenario"]}),flush=True)
            if record.get("complete"):
                if record["count"]!=len(CASES) or len(output["scenarios"])!=len(CASES):raise RuntimeError("incomplete plate context capture")
                if any(t["changes"][0]["tick"]&1!=t["scenario"]["phase"] for t in output["scenarios"]):raise RuntimeError("contact parity mismatch")
                output["complete"]=True
                break
    finally:
        if client:client.stop()
        ref.stop()
        output["sounds"]=[]
        if client:
            for line in client.log:
                try:record=json.loads(line)
                except ValueError:continue
                s=record.get("sound")
                if s and abs(s["Position"][0]-.5)<1e-5 and abs(s["Position"][1]-65.1)<1e-4 and abs(s["Position"][2]-.5)<1e-5:
                    output["sounds"].append(s)
        output["log_directory"]=str(ref.root)
        output["build_log"]=[s for s in ref.log if "Version:" in s or "Build ID:" in s]
        missing_sounds=output["complete"] and len(output["sounds"])!=2*len(CASES)
        if missing_sounds:output["complete"]=False
        args.output.write_text(json.dumps(output,separators=(",",":"))+"\n",encoding="utf-8")
        print(json.dumps({"complete":output["complete"],"sounds":len(output["sounds"]),"logs":str(ref.root)}),flush=True)
        if missing_sounds:raise RuntimeError("missing native source sound packets")
