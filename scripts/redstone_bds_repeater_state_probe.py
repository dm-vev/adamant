#!/usr/bin/env python3
"""Native world traces of lock/unlock, repeated pulses and delay changes."""

from redstone_bds_timing_probe import arguments, capture


SCRIPT = r'''
import { world, system } from "@minecraft/server";
const commandDelay = __COMMAND_DELAY__, delayOnly = __DELAY_ONLY__;
const emit = value => console.warn("ADAMANT_TIMING " + JSON.stringify(value));
const cases = [];
for (let delay = 0; delay < 4; delay++) {
    const patterns = {
        lock_off: [[-12,"side",1],[0,"rear",1],[30,"rear",0],[40,"side",0]],
        lock_on: [[-12,"rear",1],[0,"side",1],[12,"rear",0],[36,"side",0]],
        lock_rise: [[0,"rear",1],[0,"side",1],[20,"rear",0],[36,"side",0]],
        lock_fall: [[-12,"rear",1],[0,"rear",0],[0,"side",1],[36,"side",0]],
        unlock_high: [[-12,"side",1],[0,"rear",1],[20,"side",0],[52,"rear",0]],
        delay_rise: [[0,"rear",1],[3,"delay",(delay+1)%4],[28,"rear",0]],
        delay_fall: [[-12,"rear",1],[0,"rear",0],[1,"delay",(delay+1)%4]],
    };
    for (const width of [1,2,3]) {
        const sequence = [];
        for (let offset=0; offset<30; offset+=width) sequence.push([offset,"rear",((offset/width)&1)^1]);
        sequence.push([30,"rear",0]);
        patterns["train_"+width] = sequence;
    }
    for (const [name, events] of Object.entries(patterns)) {
        if (!delayOnly || name.startsWith("delay_")) cases.push({name, delay, phase:0, events});
    }
}
let dimension, index=-1, start=0, on=0, loadStarted=0, samples=[], changes=[];
const cmd = command => dimension.runCommand(command);
function read(x,z=0) {
    const b = dimension.getBlock({x,y:65,z});
    if (!b) throw new Error("sampled chunk not loaded");
    return {id:b.typeId};
}
function setup() {
    const c = cases[index];
    cmd("fill -3 64 -2 6 68 3 air"); cmd("fill -3 64 -2 6 64 3 stone");
    cmd(`setblock 0 65 0 unpowered_repeater ["minecraft:cardinal_direction"="west","repeater_delay"=${c.delay}]`);
    cmd('setblock 1 65 0 unpowered_repeater ["minecraft:cardinal_direction"="west"]');
    cmd('setblock 0 65 1 unpowered_repeater ["minecraft:cardinal_direction"="south"]');
    start=system.currentTick; on=24+((c.phase-((start+24)&1)+2)%2); samples=[]; changes=[];
}
const handle=system.runInterval(()=>{
    try {
        if (!dimension) {
            dimension=world.getDimension("overworld");
            cmd("tickingarea add -16 0 -16 16 100 16 redstone_audit true");
            cmd("gamerule randomtickspeed 0"); loadStarted=system.currentTick; return;
        }
        if (index<0) {
            if (!dimension.getBlock({x:-3,y:65,z:-2}) || !dimension.getBlock({x:6,y:65,z:3})) {
                if (system.currentTick-loadStarted>200) throw new Error("ticking area failed to load"); return;
            }
            emit({ready:true}); index=0; setup(); return;
        }
        const t=system.currentTick-start, c=cases[index];
        samples.push({t,tick:system.currentTick,first:read(0),second:read(1),side:read(0,1)});
        for (const [offset,kind,value] of c.events) {
            if (t!==on+offset) continue;
            if (kind==="delay") {
                const b=dimension.getBlock({x:0,y:65,z:0});
                if (commandDelay) cmd(`setblock 0 65 0 ${b.typeId} ["minecraft:cardinal_direction"="west","repeater_delay"=${value}]`);
                else b.setPermutation(b.permutation.withState("repeater_delay",value));
            } else {
                const pos=kind==="side"?"0 65 2":"-1 65 0";
                cmd("setblock "+pos+" "+(value?"redstone_block":"air"));
            }
            changes.push({tick:system.currentTick,kind,value,powered:!!value});
        }
        if (t>=on+100) {
            emit({scenario:c,samples,changes}); index++;
            if (index===cases.length) { emit({complete:true,count:cases.length}); system.clearRun(handle); }
            else setup();
        }
    } catch(error) { emit({error:String(error),stack:error.stack}); system.clearRun(handle); }
},1);
'''


def script(command_delay=False, delay_only=False):
    return SCRIPT.replace("__COMMAND_DELAY__", str(command_delay).lower()).replace("__DELAY_ONLY__", str(delay_only).lower())


if __name__ == "__main__":
    capture(arguments(), script(), 40)
