#!/usr/bin/env python3
"""Capture real player hook placement, line attachment and native sound packets."""

import itertools

import redstone_bds_switch_use_probe as player_capture
from redstone_bds_timing_probe import arguments


CASES = [{"direction": direction, "gap": gap, "game_mode": mode, "request_phase": i & 1}
         for i, (direction, gap, mode) in enumerate(itertools.product(
             range(4), (False, True), ("creative", "survival")))]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
const delta=[[0,-1],[1,0],[0,1],[-1,0]],nativeDirection=[2,3,0,1],faces=[3,4,2,5];
let d,load=0,ready=false,running=false,index=0,current=null,direction=1,lastSample=0;
const cmd=x=>d.runCommand(x),pos=x=>({x:x*delta[direction][0],y:65,z:x*delta[direction][1]});
const read=x=>{const b=d.getBlock(pos(x));if(!b)throw new Error("unloaded hook");return {id:b.typeId,states:b.permutation.getAllStates()};};
const set=(x,id)=>{const p=pos(x);cmd(`setblock ${p.x} ${p.y} ${p.z} ${id}`);};
const player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
const at=(a,b)=>a.x===b.x&&a.y===b.y&&a.z===b.z;
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerInteractWithBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||!at(e.block.location,pos(4)))return;
 current.changes.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,held:e.itemStack?.typeId,before:read(3)});
});
world.afterEvents.playerPlaceBlock.subscribe(e=>{
 if(!current||e.player.name!=="RedstoneAudit"||!at(e.block.location,pos(3)))return;
 current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),source:read(3)});
});
function setup(){
 for(const e of d.getEntities({excludeTypes:["minecraft:player"]}))e.remove();
 const c=cases[index];direction=c.direction;const v=delta[direction];
 cmd("fill -8 63 -8 8 63 8 stone");for(let y=64;y<=68;y++)cmd(`fill -8 ${y} -8 8 ${y} 8 air`);
 set(-2,"stone");set(4,"end_stone");set(-1,`tripwire_hook ["direction"=${nativeDirection[direction]}]`);
 for(let x=0;x<3;x++)if(!c.gap||x!==1)set(x,"trip_wire");
 const p=player();if(!p)throw new Error("audit player missing");cmd(`gamemode ${c.game_mode} "RedstoneAudit"`);
 cmd('replaceitem entity "RedstoneAudit" slot.hotbar 0 tripwire_hook 2');p.selectedSlotIndex=0;
 const feet=[4*v[0]+.5-v[0]*2-v[1]*1.25,64,4*v[1]+.5-v[1]*2+v[0]*1.25];p.teleport({x:feet[0],y:feet[1],z:feet[2]});
 current={scenario:c,feet,setup_tick:system.currentTick,changes:[],results:[],samples:[],requested:0};
}
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 16 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dotiledrops false");return;}
 if(!running){if(d.getBlock({x:0,y:65,z:0})){if(!ready){cmd("fill -8 63 -8 8 63 8 stone");cmd("setworldspawn 0 64 2");ready=true;emit({ready:true});}}else if(system.currentTick-load>240)throw new Error("load timeout");return;}
 if(!current){if(system.currentTick&1)return;setup();return;}
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),left:read(-1),right:read(3),strings:[read(0),read(1),read(2)]});lastSample=system.currentTick;
 if(!current.requested&&system.currentTick-current.setup_tick>=10&&(system.currentTick&1)===current.scenario.request_phase){
  if(player().getComponent("minecraft:inventory").container.getItem(0)?.typeId!=="minecraft:tripwire_hook")throw new Error("held hook control failed");
  const b=pos(4),face=faces[direction];
  const hit=[.5,.5,.5];hit[face<4?2:0]=(face&1)?1:0;
  emit({action:{action:"click",block:[b.x,b.y,b.z],face,hit,feet:current.feet,held_count:2,held_network_id:131},block_state:read(4)});current.requested=system.currentTick;
 }
 if(current.requested&&!current.results.length&&system.currentTick-current.requested>60)throw new Error("native hook placement not accepted "+JSON.stringify({scenario:current.scenario,changes:current.changes,right:read(3)}));
 if(current.results.length&&system.currentTick-current.results[0].tick>=30){
  if(current.changes.length!==1||current.results.length!==1||current.changes[0].held!=="minecraft:tripwire_hook"||current.results[0].source.id!=="minecraft:tripwire_hook"||read(3).states.attached_bit===current.scenario.gap)throw new Error("native hook placement positive control failed");
  emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''


if __name__ == "__main__":
    player_capture.SCRIPT = SCRIPT
    player_capture.capture_player_use(arguments(), CASES,
                                      sound_prefixes=("attach", "detach", "power.on", "power.off"))
