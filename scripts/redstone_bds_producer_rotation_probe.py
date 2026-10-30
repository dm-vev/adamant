#!/usr/bin/env python3
"""Observe setup direction versus same-identifier rotation for native producers.

Powered command states isolate producer configuration from player/bolt timing.
Lamps behind the old and new support blocks independently observe conduction.
An air replacement is a separate control, followed by an unconditional reset.
"""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],cases=[];
for(const block of ["stone_button","wooden_button","lightning_rod"])
 for(let from=0;from<6;from++)for(const to of [from,from^1,(from+2)%6])
  for(const replace of [false,true])for(const edge_phase of [0,1])cases.push({block,from,to,replace,edge_phase,phase:0});
let d,load=0,start=0,offset=0,cells=[];
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);if(!b)throw new Error("unloaded producer");return {id:b.typeId,states:b.permutation.getAllStates()};};
const side=(p,face,n)=>p.map((x,i)=>x+delta[face][i]*n);
const set=(p,id)=>cmd(`setblock ${p.join(" ")} ${id}`);
const place=(c,facing)=>set(c.source,c.scenario.block+` ["facing_direction"=${facing},"${c.scenario.block==="lightning_rod"?"powered_bit":"button_pressed_bit"}"=true]`);
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add 0 0 0 143 100 143 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");cmd("weather clear 100000");return;}
 if(!start){
  if(!get([0,65,0])||!get([143,65,143])){if(system.currentTick-load>240)throw new Error("load timeout");return;}if(system.currentTick&1)return;
  emit({ready:true});cells=[];
  for(let i=0;i<72&&offset+i<cases.length;i++){
   const scenario=cases[offset+i],source=[3+(i%9)*16,65,3+Math.floor(i/9)*16];
   for(let y=62;y<=68;y++)cmd(`fill ${source[0]-3} ${y} ${source[2]-3} ${source[0]+3} ${y} ${source[2]+3} air`);
   const old_support=side(source,scenario.from^1,1),new_support=side(source,scenario.to^1,1);
   const old_output=side(source,scenario.from^1,2),new_output=side(source,scenario.to^1,2);
   set(old_support,"stone");set(new_support,"stone");set(old_output,"redstone_lamp");set(new_output,"redstone_lamp");
   const c={scenario,source,old_support,new_support,old_output,new_output,changes:[{tick:system.currentTick,kind:"setup"}],samples:[]};
   place(c,scenario.from);cells.push(c);
  }start=system.currentTick;return;
 }
 const t=system.currentTick-start;
 for(const c of cells){
  c.samples.push({tick:system.currentTick,source:read(c.source),old_support:read(c.old_support),new_support:read(c.new_support),old_output:read(c.old_output),new_output:read(c.new_output)});
  if(t===30){
   const b=read(c.source),flag=c.scenario.block==="lightning_rod"?"powered_bit":"button_pressed_bit";
   if(b.id!=="minecraft:"+c.scenario.block||b.states[flag]!==true||read(c.old_output).id!=="minecraft:lit_redstone_lamp")throw new Error("powered producer positive control failed "+JSON.stringify({scenario:c.scenario,source:b,output:read(c.old_output),flagType:typeof b.states[flag]}));
  }
  if(t===40+c.scenario.edge_phase){if(c.scenario.replace)set(c.source,"air");place(c,c.scenario.to);c.changes.push({tick:system.currentTick,kind:"edit"});}
  if(t===70+c.scenario.edge_phase){set(c.source,"air");place(c,c.scenario.from);c.changes.push({tick:system.currentTick,kind:"reset"});}
 }
 if(t===90){for(const c of cells)emit(c);offset+=cells.length;if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 216)
