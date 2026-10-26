#!/usr/bin/env python3
"""Capture native switch output faces and one-solid-block activation."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],cases=[];
for(const block of ["wooden_button","stone_button"])for(let facing=0;facing<6;facing++)for(let output=0;output<6;output++)
for(const powered of [false,true])for(const conducted of [false,true])cases.push({block,facing,output,powered,conducted,phase:0});
for(const [direction,facing] of [["down_east_west",0],["down_north_south",0],["up_east_west",1],["up_north_south",1],["north",2],["south",3],["west",4],["east",5]])
for(let output=0;output<6;output++)for(const powered of [false,true])for(const conducted of [false,true])cases.push({block:"lever",direction,facing,output,powered,conducted,phase:0});
let d,load=0,start=0,cells=[],offset=0;
const cmd=x=>d.runCommand(x),block=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=block(p);if(!b)throw new Error("unloaded sample");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 175 100 79 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
if(!start){
    if(!block([0,65,0])||!block([174,65,78])){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
    emit({ready:true});for(let y=62;y<=68;y++)cmd(`fill 0 ${y} 0 175 ${y} 79 air`);
    cells=[];
    for(let i=0;i<192&&offset+i<cases.length;i++){
        const c=cases[offset+i],x=i%22*8+3,z=Math.floor(i/22)*8+3,v=delta[c.facing],o=delta[c.output],n=c.conducted?2:1;
        const source=[x,65,z],output=[x+o[0]*n,65+o[1]*n,z+o[2]*n],conductor=[x+o[0],65+o[1],z+o[2]];
        cmd(`setblock ${x-v[0]} ${65-v[1]} ${z-v[2]} stone`);
        if(c.conducted)cmd(`setblock ${conductor.join(" ")} stone`);
        cmd(`setblock ${output.join(" ")} redstone_lamp`);
        const props=c.block==="lever"?`"lever_direction"="${c.direction}","open_bit"=${c.powered}`:`"facing_direction"=${c.facing},"button_pressed_bit"=${c.powered}`;
        cmd(`setblock ${source.join(" ")} ${c.block} [${props}]`);
        cells.push({scenario:c,source,output,conductor,samples:[],changes:[{tick:system.currentTick,kind:"setup"}]});
    }
    start=system.currentTick;return;
}
for(const c of cells){
    const source=read(c.source);if(source.id!=="minecraft:"+c.scenario.block)throw new Error("switch lost support "+JSON.stringify(c.scenario));
    c.samples.push({tick:system.currentTick,source,output:read(c.output),conductor:read(c.conductor)});
}
if(system.currentTick-start>=24){for(const c of cells)emit(c);offset+=cells.length;
    if(offset===cases.length){emit({complete:true,count:offset});system.clearRun(h);}else start=0;
}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 480)
