#!/usr/bin/env python3
"""Capture initial producers in all dimensions after proving each chunk ticks."""

import json
from redstone_bds_daylight_probe import SCRIPT
from redstone_bds_timing_probe import arguments, capture

CASES = [{"time": 6000, "advance": False, "phase": 1}]

# getBlock proves terrain availability, not that a newly forced native chunk
# has entered block-actor ticking. Use nonmatching sentinel signals to require
# an actual detector update in every tested chunk before the initial-state case.
SCRIPT = SCRIPT.replace("let ds=[]", "let primed=false,tickReady=0;let ds=[]", 1)
needle = "  const c=cases[index];if((system.currentTick&1)!==c.phase)return;"
if SCRIPT.count(needle) != 1:
    raise RuntimeError("daylight setup template changed")
SCRIPT = SCRIPT.replace(needle, r'''
  if(!primed){
   cmd(ds[0],"gamerule dodaylightcycle false");cmd(ds[0],"time set 6000");
   for(const d of ds)for(const x of [0,16])for(const z of [0,16])cmd(d,`setblock ${x} 65 ${z} daylight_detector ["redstone_signal"=1]`);
   primed=true;return;
  }
  if(!tickReady){
   if(!ds.every((d,i)=>[0,16].every(x=>[0,16].every(z=>get(d,x,65,z).permutation.getState("redstone_signal")===(i===0?15:0)))))return;
   tickReady=system.currentTick;
  }
''' + needle, 1)
SCRIPT = SCRIPT.replace("current={scenario:c,", "current={warmup_tick:tickReady,scenario:c,", 1)

if __name__ == "__main__":
    capture(arguments(), SCRIPT.replace("CASES", json.dumps(CASES)), len(CASES))
