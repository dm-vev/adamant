#!/usr/bin/env python3
"""Exercise native rod selection through the real skeleton-horse trap goal.

The inspected trap goal and natural chunk weather call the same selector. A
regular local player activates the trap; the pack never spawns lightning.
"""

import json
from redstone_bds_switch_use_probe import capture_player_use
from redstone_bds_timing_probe import arguments

CASES = [{"name": "clear weather", "weather": "clear", "rods": [{"x": 8}]},
         {"name": "no rods", "rods": []},
         {"name": "one exposed", "rods": [{"x": 8}]},
         {"name": "covered nearest", "rods": [{"x": 4, "cover": "stone"}, {"x": 8}]},
         {"name": "boundary 64", "rods": [{"x": 64}]},
         {"name": "outside 65", "rods": [{"x": 65}]}]
CASES += [{"name": "cover " + material, "rods": [{"x": 8, "cover": material}]}
          for material in ("glass", "leaves", "daylight_detector", "daylight_detector_inverted", "carpet", "oak_slab",
                           "oak_fence", "spruce_fence", "birch_fence", "jungle_fence", "acacia_fence", "dark_oak_fence",
                           "mangrove_fence", "cherry_fence", "bamboo_fence", "pale_oak_fence", "poplar_fence",
                           "crimson_fence", "warped_fence", "nether_brick_fence")]

SCRIPT = r'''
import {world,system} from "@minecraft/server";
const cases=CASES,emit=x=>console.warn("ADAMANT_USE "+JSON.stringify(x));
let d,load=0,ready=false,running=false,index=0,current=null,horse=null;
const cmd=x=>d.runCommand(x),player=()=>world.getAllPlayers().find(p=>p.name==="RedstoneAudit");
system.afterEvents.scriptEventReceive.subscribe(e=>{if(e.id==="audit:start")running=true;});
world.afterEvents.entitySpawn.subscribe(e=>{if(current&&e.entity.typeId==="minecraft:lightning_bolt")current.bolts.push({tick:system.currentTick,location:e.entity.location});});
const h=system.runInterval(()=>{try{
 if(!d){d=world.getDimension("overworld");load=system.currentTick;cmd("tickingarea add -16 0 -16 80 100 16 audit true");cmd("gamerule randomtickspeed 0");cmd("gamerule domobspawning false");cmd("gamerule dofiretick false");cmd("difficulty normal");return;}
 if(!running){if(d.getBlock({x:70,y:65,z:0})){
  if(!ready){cmd("fill -4 64 -4 70 64 4 stone");cmd("setworldspawn 0 65 4");ready=true;emit({ready:true});}
 }else if(system.currentTick-load>240)throw new Error("load timeout");return;}
 if(!current){
  const p=player();if(!p)throw new Error("missing audit player");p.teleport({x:.5,y:65,z:4.5});
  for(const e of d.getEntities())if(e.typeId!=="minecraft:player")e.remove();
  cmd("fill -4 65 -4 70 70 4 air");cmd("fill -4 64 -4 70 64 4 stone");
  // Rain enables the selector while keeping random thunder bolts out of this
  // controlled trap-triggered capture.
  const c=cases[index];cmd("weather "+(c.weather??"rain")+" 100000");
  for(const r of c.rods){cmd(`setblock ${r.x} 65 0 lightning_rod ["facing_direction"=1]`);if(r.cover)cmd(`setblock ${r.x} 68 0 ${r.cover} ${r.cover==="leaves"?'["persistent_bit"=true]':""}`);}
  current={scenario:c,setup_tick:system.currentTick,warmup:c.weather==="clear"?160:24,changes:[],bolts:[],samples:[]};horse=null;return;
 }
 if(!horse&&system.currentTick-current.setup_tick===current.warmup){
  horse=d.spawnEntity("minecraft:skeleton_horse",{x:.5,y:65,z:.5});horse.triggerEvent("minecraft:set_trap");
  current.changes.push({tick:system.currentTick,horse:horse.location});
  current.blocks=current.scenario.rods.flatMap(r=>[65,...(r.cover?[68]:[])].map(y=>{const b=d.getBlock({x:r.x,y,z:0});return {position:[r.x,y,0],id:b.typeId,states:b.permutation.getAllStates()};}));
 }
 if(horse){current.samples.push({tick:system.currentTick,horse:horse.isValid?horse.location:null,rods:current.scenario.rods.map(r=>({x:r.x,states:d.getBlock({x:r.x,y:65,z:0}).permutation.getAllStates()}))});}
 if(system.currentTick-current.setup_tick===current.warmup+30){
  if(current.bolts.length!==1)throw new Error("expected one trap lightning bolt: "+JSON.stringify(current.bolts));
  emit(current);index++;current=null;
  if(index===cases.length){emit({complete:true,count:index});running=false;system.clearRun(h);}
 }
}catch(e){emit({error:String(e),stack:e.stack});system.clearRun(h);}},1);
'''

if __name__ == "__main__":
    import redstone_bds_switch_use_probe as driver
    driver.SCRIPT = SCRIPT
    args = arguments()
    capture_player_use(args, CASES, require_parities=False)
    output = json.loads(args.output.read_text(encoding="utf-8"))
    output["reference"] = "unmodified Windows BDS; actual skeleton-horse trap invokes the shared rod selector; regular loopback player; pack never spawns bolts"
    args.output.write_text(json.dumps(output,separators=(",", ":"))+"\n",encoding="utf-8")
