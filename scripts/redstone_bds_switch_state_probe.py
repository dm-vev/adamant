#!/usr/bin/env python3
"""Read native switch permutation, producer faces and wire output after placement.

Pressed/on command states are input states, not simulated player clicks.
Normal use, release/retrigger and projectile timing require interaction traces.
"""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const faces=["Down","Up","North","South","West","East"],delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]];
const buttons=["wooden_button","spruce_button","birch_button","jungle_button","acacia_button","dark_oak_button","crimson_button","warped_button","mangrove_button","cherry_button","pale_oak_button","bamboo_button","poplar_button","stone_button","polished_blackstone_button"];
const cases=[];
for(const block of buttons)for(let facing=0;facing<6;facing++)for(const powered of [false,true])cases.push({block,facing,powered,phase:0});
for(const [direction,facing] of [["down_east_west",0],["down_north_south",0],["up_east_west",1],["up_north_south",1],["north",2],["south",3],["west",4],["east",5]])
for(const powered of [false,true])cases.push({block:"lever",direction,facing,powered,phase:0});
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x),block=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=block(x,65,z);if(!b)throw new Error("unloaded sample");return {id:b.typeId,states:b.permutation.getAllStates()};};
function producer(x,z){const p=block(x,65,z).getComponent("minecraft:redstone_producer");if(!p)throw new Error("no native switch producer");return {power:p.power,strong:p.getStronglyPoweredFace(),connected:p.getConnectedFaces()};}
const h=system.runInterval(()=>{try{
if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 175 100 79 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
if(!start){
    if(!block(0,65,0)||!block(174,65,78)){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
    emit({ready:true});cmd("fill 0 64 0 175 64 79 stone");cmd("fill 0 65 0 175 66 79 air");
    for(let i=0;i<cases.length;i++){
        const c=cases[i],x=i%22*8+3,z=Math.floor(i/22)*8+2,v=delta[c.facing],wx=x+(c.facing===4?-1:1);
        cmd(`setblock ${x-v[0]} ${65-v[1]} ${z-v[2]} stone`);
        const props=c.block==="lever"?`"lever_direction"="${c.direction}","open_bit"=${c.powered}`:`"facing_direction"=${c.facing},"button_pressed_bit"=${c.powered}`;
        cmd(`setblock ${x} 65 ${z} ${c.block} [${props}]`);cmd(`setblock ${wx} 65 ${z} redstone_wire`);
        const source=read(x,z);
        if(source.id!=="minecraft:"+c.block)throw new Error("native switch placement failed");
        cells.push({scenario:c,x,z,wx,initial_producer:producer(x,z),samples:[],changes:[{tick:system.currentTick,kind:"setup"}]});
    }
    start=system.currentTick;return;
}
for(const c of cells){
    const source=read(c.x,c.z);if(source.id!=="minecraft:"+c.scenario.block)throw new Error("switch lost support");
    c.samples.push({tick:system.currentTick,source,wire:read(c.wx,c.z)});
}
if(system.currentTick-start>=80){
    for(const c of cells){c.final_producer=producer(c.x,c.z);emit(c);}
    emit({complete:true,count:cells.length});system.clearRun(h);
}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 196)
