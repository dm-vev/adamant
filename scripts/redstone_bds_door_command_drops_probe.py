#!/usr/bin/env python3
"""Record native /setblock and /fill destroy resource counts for both door halves."""
from redstone_bds_door_support_probe import SCRIPT, capture
from redstone_bds_timing_probe import arguments

CASES = [dict(block=b, action='command_destroy', command=cmd, top=top, drops=drops, phase=phase)
         for b in ('wooden_door', 'iron_door', 'copper_door')
         for cmd in ('setblock', 'fill') for top in (False, True)
         for drops in (False, True) for phase in (0, 1)]

SOURCE = SCRIPT.replace('  current.after=positions.map(read);', r'''
  if(c.action==="command_destroy"){
   const p=(c.top?upper:lower).join(" ");
   current.command_input=c.command==="fill"?`fill ${p} ${p} air destroy`:`setblock ${p} air destroy`;
   current.success_count=cmd(current.command_input).successCount;
   if(current.success_count!==0&&current.success_count!==1)throw new Error("unexpected native command result");
  }
  current.after=positions.map(read);''')

if __name__ == '__main__':
    capture(arguments(), CASES, source_script=SOURCE, description=__doc__)
