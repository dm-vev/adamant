#!/usr/bin/env python3
"""Capture native tripwire length limits and command removal at both parities."""

import json
from redstone_bds_timing_probe import arguments, capture


CASES = [{"direction": direction, "length": length, "disarmed": disarmed, "phase": phase}
         for direction in range(4) for length in (1, 2, 39, 40, 41)
         for disarmed in (False, True) for phase in (0, 1)]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1],[1,0],[0,1],[-1,0]],nativeDirection=[2,3,0,1];
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:65,z:p[1]}),shift=(p,v,n)=>[p[0]+v[0]*n,p[1]+v[1]*n];
const set=(p,name,states="")=>cmd(`setblock ${p[0]} 65 ${p[1]} ${name} ${states}`);
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded line");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");cmd("tickingarea add 0 0 0 159 100 159 audit true");cmd("gamerule randomtickspeed 0");load=system.currentTick;return;}
 if(!start){
  if(!get([0,0])||!get([159,159])){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
  emit({ready:true});cmd("fill 0 64 0 159 64 159 stone");for(let y=65;y<=66;y++)cmd(`fill 0 ${y} 0 159 ${y} 159 air`);
  for(let i=0;i<cases.length;i++){
   const c=cases[i],v=delta[c.direction],j=i%20,bx=(c.direction===1||c.direction===3)?80:0,bz=c.direction>=2?80:0;
   const left=v[0]?[bx+(v[0]>0?3:76),bz+3+j*3]:[bx+3+j*3,bz+(v[1]>0?3:76)];
   const right=shift(left,v,c.length+1),wires=[];
   set(shift(left,v,-1),"stone");set(shift(right,v,1),"stone");
   set(left,"tripwire_hook",`["direction"=${nativeDirection[c.direction]}]`);set(right,"tripwire_hook",`["direction"=${nativeDirection[(c.direction+2)%4]}]`);
   for(let n=1;n<=c.length;n++){const p=shift(left,v,n);wires.push(p);set(p,"trip_wire");}
   const output=shift(left,v,-2);set(output,"redstone_wire");
   cells.push({scenario:c,left,right,wires,output,cut:wires[Math.floor(c.length/2)],samples:[],changes:[]});
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.samples.push({tick:system.currentTick,left:read(c.left),right:read(c.right),wires:c.wires.map(read),output:read(c.output)});
  const edge=16+c.scenario.phase;
  if(t===edge-4&&c.scenario.disarmed){const b=get(c.cut);b.setPermutation(b.permutation.withState("disarmed_bit",true));c.configuration={tick:system.currentTick};}
  if(t===edge){set(c.cut,"air");c.changes.push({tick:system.currentTick,kind:"remove"});}
 }
 if(t>=64){
  for(const c of cells){
   const before=c.samples.filter(s=>s.tick<c.changes[0].tick);
   if(c.scenario.length===40&&!before.some(s=>s.left.states.attached_bit&&s.right.states.attached_bit))throw new Error("forty-string positive control failed");
   emit(c);
  }emit({complete:true,count:cells.length});system.clearRun(h);
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
