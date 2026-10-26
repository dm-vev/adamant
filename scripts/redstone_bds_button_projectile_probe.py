#!/usr/bin/env python3
"""Observe original BDS button activation and holding by real arrow physics.

The inspected native query also accepts tridents. They are not summonable by
the stable API; their world-level capture remains a separate audit item.
"""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import {world,system} from "@minecraft/server";
const emit=x=>console.warn("ADAMANT_TIMING "+JSON.stringify(x));
const delta=[[0,-1,0],[0,1,0],[0,0,-1],[0,0,1],[-1,0,0],[1,0,0]],cases=[];
for(const block of ["wooden_button","spruce_button","birch_button","jungle_button","acacia_button","dark_oak_button","crimson_button","warped_button","mangrove_button","cherry_button","pale_oak_button","bamboo_button","poplar_button","stone_button","polished_blackstone_button"])
for(const projectile of ["minecraft:arrow"])for(let facing=0;facing<6;facing++)for(const phase of [0,1])cases.push({block,projectile,facing,phase});
let d,load=0,start=0,cells=[];
const cmd=x=>d.runCommand(x),block=(x,y,z)=>d.getBlock({x,y,z});
const read=(x,z)=>{const b=block(x,65,z);if(!b)throw new Error("unloaded sample");return {id:b.typeId,states:b.permutation.getAllStates()};};
const h=system.runInterval(()=>{try{
if(!d){d=world.getDimension("overworld");cmd("tickingarea add -16 0 -16 175 100 79 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");load=system.currentTick;return;}
if(!start){
    if(!block(0,65,0)||!block(174,65,78)){if(system.currentTick-load>200)throw new Error("load timeout");return;}if(system.currentTick&1)return;
    emit({ready:true});for(let y=63;y<=67;y++)cmd(`fill 0 ${y} 0 175 ${y} 79 air`);
    for(let i=0;i<cases.length;i++){
        const c=cases[i],x=i%22*8+3,z=Math.floor(i/22)*8+3,v=delta[c.facing],wx=x+(c.facing===4?-1:1);
        cmd(`setblock ${x-v[0]} ${65-v[1]} ${z-v[2]} stone`);cmd(`setblock ${wx} 64 ${z} stone`);
        cmd(`setblock ${x} 65 ${z} ${c.block} ["facing_direction"=${c.facing}]`);cmd(`setblock ${wx} 65 ${z} redstone_wire`);
        cells.push({scenario:c,x,z,wx,entity:null,samples:[],changes:[],spawn:null});
    }
    start=system.currentTick;return;
}
const t=system.currentTick-start;
for(const c of cells){
    const source=read(c.x,c.z);if(source.id!=="minecraft:"+c.scenario.block)throw new Error("button lost support");
    const entity=c.entity&&c.entity.isValid?c.entity:null;
    c.samples.push({tick:system.currentTick,source,wire:read(c.wx,c.z),projectile:entity?{id:entity.typeId,position:entity.location,velocity:entity.getVelocity(),aabb:typeof entity.getAABB==="function"?entity.getAABB():null}:null});
    const edge=12+c.scenario.phase;
    if(t===edge){
        const v=delta[c.scenario.facing],position={x:c.x+0.5+v[0]*0.8,y:65.5+v[1]*0.8,z:c.z+0.5+v[2]*0.8};
        const e=d.spawnEntity(c.scenario.projectile,position),projectile=e.getComponent("minecraft:projectile");
        if(!projectile)throw new Error("native projectile lacks physics component");
        projectile.shoot({x:-v[0]*0.75,y:-v[1]*0.75,z:-v[2]*0.75});c.entity=e;
        c.spawn={id:e.typeId,position,velocity:e.getVelocity(),components:e.getComponents().map(c=>c.typeId)};
        c.changes.push({tick:system.currentTick,kind:"spawn"});
    }
    if(t===edge+45){if(!c.entity?.isValid)throw new Error("native projectile disappeared before removal");c.entity.remove();c.entity=null;c.changes.push({tick:system.currentTick,kind:"remove"});}
}
if(t>=100){
    for(const c of cells){delete c.entity;emit(c);}
    emit({complete:true,count:cells.length});system.clearRun(h);
}
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    capture(arguments(), SCRIPT, 180)
