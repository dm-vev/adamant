#!/usr/bin/env python3
"""Capture authenticated player door breaks and native item resource counts."""
import argparse
from pathlib import Path

from redstone_bds_switch_use_probe import capture_player_use

CASES = [dict(block=b, top=top, game_mode=mode, tool=tool, drops=drops)
         for mode, tool in (('creative', None), ('survival', 'diamond_pickaxe'), ('survival', 'wooden_pickaxe'))
         for b in ('wooden_door', 'iron_door', 'copper_door')
         for top in (False, True) for drops in (False, True)]

SCRIPT = r'''
import {world,system,ItemStack} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,ready=false,running=false,load=0,index=0,current=null,lastSample=0;
const cmd=x=>d.runCommand(x),get=p=>d.getBlock({x:p[0],y:p[1],z:p[2]});
const read=p=>{const b=get(p);return {id:b.typeId,states:b.permutation.getAllStates()};};
const blocks=()=>current.positions.map(read),player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
const miningTicks=c=>c.game_mode==="creative"?0:(!c.tool?550:(c.tool==="wooden_pickaxe"?260:120));
const body=e=>{const s=e.getComponent("minecraft:item").itemStack;return {actor_id:e.id,id:s.typeId,count:s.amount,position:e.location,velocity:e.getVelocity()};};
const items=()=>d.getEntities({type:"minecraft:item"}).map(body);
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.beforeEvents.playerBreakBlock.subscribe(e=>{if(!current||e.player.name!=="RedstoneAudit")return;
 current.changes.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),before_sample:lastSample!==system.currentTick,
  position:e.block.location,held:e.itemStack?.typeId,game_mode:e.player.getGameMode(),blocks:blocks()});});
world.afterEvents.playerBreakBlock.subscribe(e=>{if(!current||e.player.name!=="RedstoneAudit")return;
 current.results.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),position:e.block.location,blocks:blocks(),items:items()});});
world.afterEvents.entitySpawn.subscribe(e=>{if(current&&e.entity.typeId==="minecraft:item")
 current.spawns.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),body:body(e.entity)});});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 16 100 16 door_player_break true");
  cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");return;}
 if(!ready){if(!get([-3,65,-3])||!get([3,65,3])){if(system.currentTick-load>480)throw new Error("chunks not ready");return;}
  cmd("fill -3 64 -3 3 64 3 stone");cmd("setworldspawn 0 65 2");ready=true;emit({ready:true});return;}
 if(!running)return;
 if(!current){for(const e of d.getEntities({type:"minecraft:item"}))e.remove();
  const c=cases[index],base=[0,65,0],positions=[base,[0,66,0]],target=positions[c.top?1:0],feet=[.5,65,2.5];
  cmd("fill -3 65 -3 3 68 3 air");cmd(`gamerule dotiledrops ${c.drops}`);
  if(cmd(`setblock 0 65 0 ${c.block} ["minecraft:cardinal_direction"="north","door_hinge_bit"=false,"open_bit"=false,"upper_block_bit"=false]`).successCount!==1)throw new Error("door setup failed");
  const p=player();if(!p)throw new Error("player missing");cmd(`gamemode ${c.game_mode} "RedstoneAudit"`);
  p.selectedSlotIndex=0;p.getComponent("minecraft:inventory").container.setItem(0,c.tool?new ItemStack("minecraft:"+c.tool,1):undefined);
  p.teleport({x:feet[0],y:feet[1],z:feet[2]});
  current={scenario:c,base,positions,target,feet,setup_tick:system.currentTick,initial:positions.map(read),changes:[],results:[],spawns:[],samples:[],requested:0};return;}
 current.samples.push({tick:system.currentTick,game_tick:world.getAbsoluteTime(),blocks:blocks(),items:items()});lastSample=system.currentTick;
 const c=current.scenario;
 if((system.currentTick-current.setup_tick)%100===0)emit({heartbeat:true,index,tick:system.currentTick});
 if(!current.requested&&system.currentTick-current.setup_tick>=16){
  current.requested=system.currentTick;
  emit({action:{action:"break",block:current.target,face:3,hit:[.5,.5,.5],feet:current.feet,
   break_ticks:miningTicks(c)},block_state:read(current.target)});}
 if(current.requested&&!current.results.length&&system.currentTick-current.requested>miningTicks(c)+80)throw new Error("native player break not accepted "+JSON.stringify(current));
 if(current.results.length&&system.currentTick-current.results[0].tick>=8&&
  system.currentTick-current.requested>=miningTicks(c)+8){
  if(current.changes.length!==1||current.results.length!==1||current.changes[0].held!==(c.tool?"minecraft:"+c.tool:undefined)||current.changes[0].game_mode.toLowerCase()!==c.game_mode)throw new Error("native player input controls differ "+JSON.stringify(current));
  if(blocks().some(b=>b.id!=="minecraft:air"))throw new Error("native paired destruction failed "+JSON.stringify(current));
  emit(current);index++;current=null;if(index===cases.length){emit({complete:true,count:index});system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('binary', 'assets', 'output', 'client-executable'):
        parser.add_argument('--'+name, type=Path, required=True)
    parser.add_argument('--port', type=int, default=30951)
    parser.add_argument('--limit', type=int)
    parser.add_argument('--empty-hand', action='store_true')
    args = parser.parse_args()
    cases = [dict(block=b, top=top, game_mode='survival', tool=None, drops=True)
             for b in ('wooden_door', 'iron_door', 'copper_door') for top in (False, True)] if args.empty_hand else CASES
    limit = len(cases) if args.limit is None else args.limit
    if not 1 <= limit <= len(cases):
        parser.error('invalid case limit')
    capture_player_use(args, cases[:limit], require_parities=False, source_script=SCRIPT,
                       client_executable=args.client_executable.resolve(), sound_prefixes=())
